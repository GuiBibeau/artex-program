# ARTEX Program architecture (code-level companion)

The [Architecture](README.md#architecture) section of the README has the component diagram and the
general view. This document is the code-level map. It records the execution flow and the trust
boundaries that are easy to miss when you read one package alone.

## 1. The two graphs

ARTEX Program keeps two linked graphs. Both are in PostgreSQL (`db/schema.sql`).

| Graph | Answers | Main tables | Code |
| --- | --- | --- | --- |
| **Asset graph** | *What exists?* | `companies`, `company_scope`, `assets`, `task_scope` | `db/assets.go`, `db/solana.go`, `db/companies.go`, `db/company_scope.go`, `db/task_assets.go` |
| **Exploration graph** | *What was tried, with what result, and why?* | `explorations`, `exploration_nodes`, `activity`, `intent_*`, `coverage_*` | `db/exploration.go`, `db/coverage_ledger.go`, `db/finding_assets.go` |

Anchors tie a finding to the assets that justify it (`db/finding_assets.go`). Traffic snapshots are
archived separately. A finding references them (`db/finding_traffic.go`,
`db/finding_traffic_archive.go`, `traffic/archive.go`).

## 1a. Solana program assets

A program is a root asset. A root domain is the root asset for network assets. The Solana asset
types are `program`, `instruction`, `account`, `authority`, `mint` and `token_account`. They are
stored with `chain = 'solana'`. Each type but `program` carries its program id in `assets.parent`
(`program_id` in the tools). The tree is therefore one level deep.

- **Keys.** `program`, `account`, `authority`, `mint` and `token_account` are unique per
  `(type, address)`. An instruction is unique per `(parent, name)`. An address is a base58 key of 32
  bytes. `db.DecodeBase58` validates it. The type-specific attributes live in `extra` (the tools call
  them `props`). When an asset is registered again, the arrays in `props` are united with the stored
  arrays, and the scalar values are replaced.
- **Scope.** A `task_scope` row of kind `program` (value = program id) covers the program and each
  asset whose `parent` equals the program id (`db.solanaScopePredicate`, used by the three coverage
  CTEs). A row of kind `cluster` (value = `devnet`, `testnet`, `mainnet-beta`, `localnet`, or an RPC
  URL) names the endpoint that the evidence must come from.
- **Evidence.** A Solana asset has no host. `VerifyFindingProofForNode` therefore matches the bound
  traffic of the asset against the RPC host of the cluster rows of the task
  (`db.taskClusterRPCHosts`).
- **Writes.** `insert_assets` calls `db.UpsertSolanaAsset`. When the program of a child asset is not
  registered yet, a bare program row is created. This connector row does not widen the scope. The
  manual scope registration (`RegisterTaskAssetScopes`) accepts the `program` and `cluster` kinds.
- **Goal gate.** A task goal must name a concrete target. `goalGateCheck` in `server/goal_gate.go`
  accepts a Solana program id as a target.

`db.Open` executes the idempotent `schema.sql` at each startup. It then applies the pending numbered
migrations from `db/migrations/*.sql`. It records the name and the SHA-256 of each migration in
`schema_migrations`. A published migration is never edited. Fix forward with a new migration.

## 2. Package map

| Package | Responsibility | Notes |
| --- | --- | --- |
| `cmd/artex` | Process entry point | Flags (`-addr`, `-data`, `-proxy`), startup config snapshot, signal handling, graceful drain |
| `config` | Resolve config file + env | `Path`, `Load`, `PostgresDSN`, `SkillDir`, `KeyDir`, `Redact`. Env wins over file |
| `db` | All persistence | `DB`, stores that do not depend on `Manager`; AES-256-GCM secret columns in `secrets.go` |
| `agent` | LLM roles and tools | `goals`, `planner`, `worker`, `mainagent`, `retester`; `ToolSet` in `tools.go`; tool catalog in `toolcatalog.go` |
| `server` | HTTP API + orchestration | `net/http` mux (~270 routes), `Manager`, `Engine`, JWT auth, SSE broadcaster |
| `traffic` | Recording proxy | MITM proxy built on `go-mitmproxy`, replay, rewrite rules, body archive |
| `evidence` | Evidence store | Persisted request and response snapshots that findings use |
| `intercept` | Approval gate | Judge, argv grammar, target gate, pins, risk scoring (a `PreToolUse` hook) |
| `guard` | Hook wiring | Binds `intercept` decisions into the hook registry of Norma; `NewTargetOnly` for planner and mainagent |
| `egress` | Noise governor | Per-host token bucket, jitter, concurrency caps, 429 and 503 backoff; the default profile is strict passthrough |
| `llmpool` / `llmrec` | Provider pool / recorder | Rotation, health, usage, and raw wire capture |
| `mcphttp` | MCP client | HTTP and SSE transport for the configured MCP servers |
| `notify` | Outbound notifications | Email, webhook, Telegram, Feishu, DingTalk, with masking and SSRF guards |
| `report` | Deliverables | Markdown, CSV and JSON rendering of findings |
| `sidequestion` | Operator Q&A | Asks a human during a run, and suspends the intent until the answer |
| `skills/` | Skill definitions | One `SKILL.md` per subdirectory, loaded from `SkillDir()`: `solana-recon`, `anchor-audit`, `solana-bytecode`, `solana-exploit`, plus the generic `codeaudit`, `patch-diff`, `rea`, `third-party-code-review` |

The frontend is in `web/` (Next.js 16, React 19). It is embedded into the Go binary through
`server/webui_embed.go` when you build with `-tags embedui`.

## 3. End-to-end flow: from the goal to a finding with evidence

1. **Task creation.** `server.createTask` calls `Manager.CreateTaskWithOptions`. It writes the task,
   its scope and the seed goals, and creates the exploration graph (`db/tasks.go`).
2. **Loop start.** `Engine.Run` (`server/engine.go`) starts a planner loop and N worker goroutines
   under a context that belongs to the task. The loops exist without an LLM. They idle until one is
   configured.
3. **Decomposition.** The `goals` role writes goals and operation constraints with `set_goals` and
   `set_constraints` (`agent/toolcatalog.go`, `agent/tools.go`). It also registers the scope of the
   task: the programs and the clusters that the goal names.
4. **Planning.** `Planner.Plan` (`agent/planner.go`) reads the situation, queries the asset graph,
   judges the goals, and emits intents (`addOneIntent`). Each intent is an exploration node, so it is
   auditable like any other activity.
5. **Execution.** `Worker.Execute` (`agent/worker.go`) claims an intent, runs the tools with the guard
   hooks attached, and writes the observed facts back to the exploration graph.
6. **Tools and traffic.** Shell and HTTP calls go through the recording proxy and the egress governor.
   The worker shell exports the proxy variables, so the Solana CLIs and `curl` send each JSON-RPC
   call through the proxy, and each call is recorded. Loopback and the package registries bypass the
   proxy (`NO_PROXY`). A command that moves funds or changes program or token state asks the operator
   first (`db/intercept_solana_seed.go`). The HTTP clients of the platform use
   `intercept.PinnedDialer` and `intercept.RedirectCheck`, so a redirect or a direct retry is checked
   again at dial time. With a proxy in the path, the gate records the pin as name-only: the proxy
   makes the outbound connection, so no IP binding is possible.
7. **Findings.** `addFinding` (`agent/tools.go`) records the severity, the class, the confidence and
   the asset references. `db/findings.go` deduplicates and keeps the status workflow. The evidence is
   linked to the captured traffic (`traffic/evidence.go`, `db/finding_traffic.go`).
8. **Completion.** `prove_goal` marks a goal as met. The last met goal completes the task.
   `goal_met` is unbound by default, because it bypasses the proof of each goal.

## 4. Execution control

- **Context per task.** Pause, cancel and kill attach a named cause through
  `context.WithCancelCause`. The activity trace then records who initiated a stop.
- **Control per intent.** The UI can pause or cancel one intent. `steerBox` queues a course
  correction from the planner. The `PreToolUse` hook of the worker drains the queue before the next
  tool call, and plans again without a restart of the run.
- **Delete barrier.** `Engine.BeginDelete` installs the barrier and registers an operation that is in
  flight in one atomic step. After it returns, no new write of the task is admitted. The task context
  is cancelled when the existing writers are done.
- **Quiet mode.** An unchanged fingerprint of the open goals suppresses redundant planning rounds
  while work is in flight.

## 5. Trust boundaries

| Boundary | Implementation |
| --- | --- |
| Tool output → model | The untrusted-output boundary of Norma frames each tool result with nonce-tagged markers. On by default; `ARTEX_UNTRUSTED_BOUNDARY=0` disables it. See `agent/untrusted_boundary.go` |
| On-chain data → model | Account data, program logs and RPC responses are tool output, and the untrusted-output boundary frames them. The prompts tell the agents to treat them as data |
| Command → host | An allow rule matches one simple Bash command with the exact prefix. A pipeline, a redirection, a wrapper (`env`, `sudo`, `xargs`, `find -exec`, a shell `-c`), a substitution or an expansion does not inherit an allow. Deny and ask rules also inspect each simple command inside wrappers, lists and pipelines. `ArgvPrefixOf` derives the prefix that a human reads |
| Live transaction → chain | The built-in approval rules in `db/intercept_solana_seed.go` ask the operator before funds, program state, token state or a raw transaction submission change. The operation constraints of the task and the fallback judge cover a transaction that a script sends |
| URL → network | Target gate, `PinnedDialer` and `RedirectCheck`: the scope decision is made again against a fresh DNS lookup at dial time. Without a proxy, the client connects only to the addresses that it compared. With a proxy, the pin is name-only |
| Secrets → disk and log | `db/secrets.go` seals API keys, proxy credentials, the MCP environment, task credentials and captured auth headers with AES-256-GCM: a nonce per value, and AAD `table.column`. JWT keys are in `KeyDir()`, never in the browsable data directory. The boot snapshot logs only `config.Redact(dsn)` |
| Approval → audit | Each decision records the approver (`jwtSubject`), the source and the decision. The history is available through `/api/intercept/history` and `/api/intercept/history/{id}` (with `/execution` for the audit detail). `/api/tasks/{id}/intercept-rules` manages rules, not history |

## 6. Decisions

- **ADR-001: nonce-tagged untrusted output.** Tool output is framed with a nonce per run. The model
  does not have to tell instructions from data.
- **ADR-002: pin at dial time.** A decision on a URL is not sufficient. DNS can change between the
  hook and the connection. The dialer therefore resolves the name again and connects only to the
  addresses that it compared.
- **ADR-003: AES-GCM with AAD per column.** A sealed value is bound to its table and column (a
  setting is bound to its key). A ciphertext cannot move to another field.
- **ADR-004: the receipt of a Solana finding is its RPC exchange.** A Solana asset has no host. The
  recorded JSON-RPC exchange on the RPC host of the cluster scope of the task is the evidence that
  proves it.

## 7. Where to look next

- `docs/OPS.md` for startup, configuration precedence, backup and key rotation.
- `docs/vulnerability-traffic-evidence.md` for the evidence of a finding.
- `docs/traffic-rewrite-rules.md` for the rewrite engine.
- `scripts/verify.sh` to reproduce the CI checks locally.
