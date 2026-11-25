# Constrained Go tampering hooks (P4.2: backend, API and trace)

ARTEX Program has opt-in HTTP tampering hooks. Only the operator can manage them. The hooks are
written in a constrained, deterministic subset of Go. There is **no Python runtime** and **no
external interpreter**. The hooks run inside the capture proxy, after the declarative rewrite rules,
in each phase (`request` or `response`). A hook fails open on any error. It records a diff of the
changed fields in the SQLite table `exchange_rewrites`.

## Kill switch and authority

- **Kill switch: `ARTEX_TAMPER_HOOKS_ENABLED=1`, off by default.** When the variable is unset or
  false, **no hook is loaded** on the capture proxy. The proxy follows the unchanged baseline path.
- **Operator only.** Hooks are a global administrator configuration. They are managed only through
  `/api/traffic/hooks*`. They are **never an agent tool**.
- **Created disabled.** Each hook is created with `enabled: false`. Enable it with the toggle or an
  update.
- **Sealed at rest.** `source` and `matchers` on `traffic_tamper_hooks` and
  `traffic_tamper_hook_versions` (migration `0011_traffic_tamper_hooks.sql`) are encrypted at rest
  with `db/secrets.go` (AES-256-GCM). A hook can contain a signing key or a token.

## Order of execution, failure mode and limits

- **Order.** In each phase (`priority ASC, id ASC`), the **declarative rewrite rules run first**.
  The **tampering hooks** then see the message after the rules.
- **Matchers filter first.** The hooks use the matchers of `traffic/rewrite` (`methods`, `host`,
  `url_regex`, `headers`, `body_regex`, `status`, `resp_headers`, `resp_body_regex`). An exchange
  that does not match never starts the VM.
- **Fail open only (0.7).** An error, a panic, a timeout, the step limit, the memory limit, or a
  forbidden HTTP mutation has one result: **all mutations of that hook roll back in one step**, and
  the message before the hook passes unchanged. The proxy records a trace row with `outcome='error'`
  in `exchange_rewrites` with the reason. The reasons are `panic`, `timeout`, `step_limit`,
  `memory_limit`, `forbidden_mutation`, `forbidden_method`, `invalid_url`, `invalid_status`,
  `forbidden_header`, `invalid_header`, `body_too_large`, `hook_error`. The proxy also updates the
  in-memory `last_error` (`class`, `at`, `count`).
- **Messages that are skipped.**
  - Binary bodies are not modified in 0.7 (`not rewritten: binary`).
  - A body above **1 MiB** is skipped (`not rewritten: body too large`). A compressed body
    (`Content-Encoding` other than `identity`), a WebSocket upgrade, an SSE response body, and a
    response of 5 MiB or more (`StreamLargeBodies`) are also skipped.
- **Fields with sensitive data stay masked.** When a rewrite rule with `sensitive: true` wrote a
  field earlier in the phase, a later hook trace line for that field shows only keyed HMAC
  fingerprints. It never stores the plaintext `after_inline` or `after_blob`.

### Limits of resources and storage

| Limit | Default |
|---|---|
| Wall-clock timeout per hook | `25 ms` |
| Cumulative timeout of the hooks per phase | `100 ms` |
| CPU step budget per invocation | `100,000` steps |
| Memory allocation budget per invocation | `4 MiB` |
| Call depth | `32` |
| Size of a regexp pattern | `1 KiB` (RE2) |
| Size of the source | `64 KiB` |
| Size of a body | `1 MiB` |
| Stored and enabled hooks | `50` total, `20` enabled |
| Retained version history | `20` newest versions per hook |

## Specification of the Go subset

The hooks are parsed with the standard `go/parser` of Go. Each hook must declare `package hook`.
The hooks are **not** full idiomatic Go. Only the syntax, the fields and the virtual stdlib functions
below are supported.

### Entry points

- Request phase (`phase: "request"`):
  ```go
  package hook
  func OnRequest(req *Request) error
  ```
- Response phase (`phase: "response"`):
  ```go
  package hook
  func OnResponse(req *Request, resp *Response) error
  ```
  In `OnResponse`, `req` is an isolated copy of the request as it was sent to the target. An
  assignment to a field (`req.Method`, `req.Path`, `req.RawQuery`, `req.Body`, `req.Headers`) and a
  mutator method (`req.SetHeader`, `req.AddHeader`, `req.DelHeader`) fail with
  `forbidden_mutation`. A direct write to a map or slice element (`req.Headers[k] = ...`,
  `delete(req.Headers, k)`, `req.Body[0] = ...`) changes only the copy of the VM. It is discarded,
  and it does not change the exchange on the wire or the trace.

### Allowed syntax

- At package level: `import`, `const` (basic literals, `bool`, unary `-`), and helper `func`
  definitions. No receivers, no `init()`, no package-level `var`, no `type` declarations.
- Statements: `:=`, `=`, `+=`, `-=`, `++`, `--`, local `var`, `if`, `else if`, `else`, `for`
  (C-style, condition only, infinite `for {}`, and `for k, v := range ...` with map keys in
  lexicographic order), `break`, `continue`, `return`.
- Expressions:
  - literals (`int`, `float`, `string`, `rune`, `bool`, `nil`);
  - unary (`!`, `-`, `+`);
  - binary (`+`, `-`, `*`, `/`, `%`, `==`, `!=`, `<`, `<=`, `>`, `>=`, `&&`, `||`);
  - indexing (`x[i]`, `m[k]`, comma-ok `v, ok := m[k]`);
  - slicing (`x[lo:hi]`);
  - composite literals (`[]byte{...}`, `[]any{...}`, `map[string]any{...}`);
  - type conversions (`[]byte(s)`, `string(b)`, `int(x)`, `int64(x)`, `float64(x)`, `byte(x)`);
  - type assertions (`v, ok := x.(T)`) for `string`, `bool`, `int` or `int64`, `float64`,
    `[]byte`, `[]any`, `map[string]any`.
- Collections: `[]byte`, `[]any`, `map[string]any`, and `map[string][]string` for `Headers` and
  `url.ParseQuery`.
- Allowed builtins: `len`, `cap`, `append`, `make`, `delete`, `string`, `int`, `int64`, `float64`,
  `byte`, `panic`.

### Forbidden constructs (rejected with HTTP 400 at validation)

- Forbidden imports: `os`, `io`, `io/fs`, `net`, `net/http`, `os/exec`, `syscall`, `unsafe`,
  `reflect`, `time`, `math/rand`, `crypto/rand`, `fmt`, `sort`, and each package outside the closed
  allowlist below. Dot (`.`) and blank (`_`) imports are forbidden.
- Forbidden syntax: `go` (goroutines), `chan`, `select`, `defer`, `goto`, `switch`, anonymous
  function literals (`func(...)`), `&` (address of), `init()`, package-level `var`, custom `type`
  and methods.
- Forbidden builtins: `recover`, `print`, `println`, `close`, `complex`, `real`, `imag`, `new`.

### API of `Request` and `Response`

| Type | Field or method | Access | Notes |
|---|---|---|---|
| `*Request` | `Scheme`, `Host`, `Port` (`string`) | **Read-only** | An assignment fails in one step (`forbidden_mutation`) |
| `*Request` | `Method`, `Path`, `RawQuery` (`string`) | Read and write in `OnRequest`, read-only in `OnResponse` | `CONNECT` is rejected. `Path` must start with `/` |
| `*Request` | `Headers` (`map[string][]string`) | Read and write in `OnRequest`, read-only in `OnResponse` | Forbidden headers (`Host`, `Content-Length`, `Transfer-Encoding`, `Connection`, `Keep-Alive`, `TE`, `Trailer`, `Expect`, `Upgrade`, `Proxy-*`, `X-ARTEX-Replay-Nonce`) and values with CRLF or NUL are rejected in one step |
| `*Request` | `Body` (`[]byte`) | Read and write in `OnRequest`, read-only in `OnResponse` | Limit 1 MiB. A mutation removes `Content-Length`, `ETag`, `Content-MD5`, `Content-Encoding` |
| `*Request` and `*Response` | `GetHeader(name) string`, `SetHeader(name, val)`, `AddHeader(name, val)`, `DelHeader(name)` | Methods | The header keys are canonicalized |
| `*Response` | `Status` (`int`) | Read and write in `OnResponse` | Must be `200..599`, without `204` and `304` |
| `*Response` | `Headers` (`map[string][]string`), `Body` (`[]byte`) | Read and write in `OnResponse` | The same rules for headers and for a body of 1 MiB |

### Allowed virtual stdlib packages and functions

- `strings`: `Contains`, `HasPrefix`, `HasSuffix`, `Index`, `TrimPrefix`, `TrimSuffix`, `ToLower`,
  `ToUpper`, `TrimSpace`, `ReplaceAll`, `Split` (returns `[]any` of strings), `Join`, `Repeat`.
- `bytes`: `Contains`, `HasPrefix`, `HasSuffix`, `Equal`, `Index`, `TrimSpace`, `ToLower`, `ToUpper`,
  `ReplaceAll`, `Repeat`.
- `encoding/json`: `Decode(b []byte) (any, error)`, `Marshal(v any) ([]byte, error)` (sorts the object
  keys deterministically).
- `encoding/base64`: `EncodeToString`, `DecodeString`, `URLEncodeToString`, `URLDecodeString`.
- `encoding/hex`: `EncodeToString`, `DecodeString`.
- `net/url`: `QueryEscape`, `QueryUnescape`, `ParseQuery`, `EncodeQuery`.
- `regexp`: `MatchString`, `FindString`, `ReplaceAllString` (RE2, pattern of at most 1 KiB).
- `crypto/sha256`: `Sum256`, `Sum256Hex`.
- `crypto/hmac`: `SHA256(key, data)`, `SHA256Hex(key, data)`.
- `strconv`: `Atoi`, `Itoa`, `ParseBool`, `FormatBool`.
- `errors`: `New`.

## Administrator API and SQLite trace

Each route needs the administrator token.

| Route | Behavior |
|---|---|
| `GET /api/traffic/hooks` | Lists the hooks in the order of execution (`priority ASC, id ASC`), with `version`, `load_error`, `last_error` and `enabled_by_env`. |
| `POST /api/traffic/hooks` | Validates the AST and the matchers (400 on an error), and creates a disabled hook (`version: 1`). |
| `PUT /api/traffic/hooks/{id}` | Validates and updates the hook. It increments `version` and appends to `traffic_tamper_hook_versions`. |
| `DELETE /api/traffic/hooks/{id}` | Deletes the hook and its version history. |
| `POST /api/traffic/hooks/{id}/toggle` | Body: `{"enabled": true|false}`. |
| `GET /api/traffic/hooks/{id}/versions` | Lists the stored revisions, newest first. |
| `POST /api/traffic/hooks/{id}/rollback` | Body: `{"version": N}`. Restores revision `N` as a new version with a higher number. |
| `POST /api/traffic/hooks/dry-run` | Runs a stored hook (`hook_id`) or an inline hook (`phase`, `matchers`, `source`) against a synthetic `request` or `response`. The live traffic is not touched. |

In SQLite, `exchange_rewrites` adds `source_kind` (`'rule'` or `'hook'`), `hook_id` and
`hook_version`. A trace row of a hook stores compact metadata as JSON
(`{"kind":"hook","id":...,"name":...,"version":...,"sha256":...}`) in `rule_snapshot`. It never stores
the full source for each exchange. The original data of the client and of the target stays in
`exchanges` and `exchange_bodies`.
