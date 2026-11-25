# Security Policy

## Supported Versions

ARTEX Program is an autonomous security testing tool for Solana programs. Security fixes go to the
`main` branch and to the latest release line. The `main` branch is not a stable release. It can
change at any time.

No release is published yet. Until the first release, `main` is the only supported version.

Notes:

- Update to the latest commit or release before you report a vulnerability.
- Forks, custom builds from arbitrary commits, and modified versions are not covered by support.

## Reporting a Vulnerability

**Do not open a public issue for a security vulnerability.**

Sign in to GitHub and use the
[private vulnerability report form](https://github.com/GuiBibeau/artex-program/security/advisories/new).
You can also reach it from the **Security and quality** tab of the repository: **Advisories** →
**Report a vulnerability**. This is the only reporting channel.

Include:

- a description of the vulnerability and its expected impact;
- the ARTEX Program version or commit hash;
- the runtime environment (OS, Docker or local, PostgreSQL, LLM provider);
- the minimal steps to reproduce it (a PoC);
- logs, screenshots, or sample traffic, **with sensitive data removed**;
- a suggested remediation, if you have one.

**Do not send** real third-party data, credentials, tokens, customer data, or content with secrets.

## Scope and Authorized Testing

ARTEX Program is a security testing tool. Test only against:

- your own ARTEX Program deployment in a lab or a local environment;
- systems that you own;
- targets that are explicitly authorized in writing (a bug bounty program, a test contract).

It is prohibited to test, scan, or exploit third-party systems without explicit authorization. A
report that violates this principle can be closed without action.

## Solana Engagement Controls

These properties hold for every task:

- **Approval rules.** The built-in rules ask the operator before a command that moves funds, changes
  staking, voting, program or token state, deploys or upgrades with `anchor`, or submits a
  transaction from a command line. An agent tool cannot read or change intercept rules.
- **Limit of the command rules.** A rule matches the text of a command. A transaction that a script
  sends is not visible to a command rule. The operation constraints of the task and the fallback
  judge cover that case.
- **Wallet keys.** A worker creates test wallets in its run directory. It stores a keypair only as a
  task credential, which is sealed at rest. The prompts forbid a key in a fact, a finding, or the
  context of the model.
- **Untrusted on-chain data.** Account data, program logs and RPC responses come from the chain.
  The prompts instruct the agents to treat them as data.
- **Proxy bypass.** A worker shell sets `NO_PROXY` for loopback (`127.0.0.1`, `localhost`, `::1`)
  and for the package registries and source hosts (crates.io, GitHub, npm, PyPI, the Go module
  proxy). Traffic to these hosts does not go through the recording proxy and is not recorded. A
  local validator and a local test server must be reached directly. Cargo and other rustls clients
  do not trust the CA of the proxy.

## Trusted Inputs and Scope Enforcement

ARTEX Program trusts the scanner output file (`output.json`) that `ingest_bbot` reads. This tool is
not bound to any agent by default in ARTEX Program. An operator can bind it.

- The `scope_distance` field on an event is the verdict of the scanner. ARTEX Program ingests it as
  it is. An event that the scanner marks as in scope (through its `-s` or `--no-scope` setting) is in
  scope. ARTEX Program does not check it again against its own rules.
- The enforced trust boundary for asset insertion is `insert_assets`. It runs each candidate through
  `EvaluateAssetGate`. Block rules always apply. A task allow-list applies when one is configured.
- A rejected candidate returns an `insertion forbidden` error for that asset. It is never inserted,
  so it cannot reach task execution.
- `in_scope_not_asset` in the `ingest_bbot` result is a different count: in-scope events that are
  not asset types, such as findings.

Accepted gap: when a task has no allow-list, there is no allow rule to apply. Block rules still
run. A host on an in-scope event that is not blocked then enters the graph as an asset. An operator
who relies on the scope rules of the scanner alone takes one of two steps: configure an allow-list
per task, or review `output.json` before the file goes to ARTEX Program. A check of each ingested event against
the rules of ARTEX Program would change what an existing scanner configuration means for these
operators. It is therefore a design question (see upstream issue #317), not an accepted
vulnerability.

## Outbound Model Listing (`POST /api/llm/models`)

`POST /api/llm/models` fetches model lists from the provider endpoint and from the optional HTTP
proxy that the request supplies. By default, ARTEX Program allows only `http` and `https` URLs
whose hosts resolve to public IP addresses only. It blocks:

- loopback;
- the RFC1918 private networks;
- the IPv6 unique-local range `fc00::/7`;
- link-local addresses and cloud metadata addresses, such as `169.254.169.254`;
- the unspecified address and multicast addresses;
- carrier-grade NAT `100.64.0.0/10`;
- the benchmarking range `198.18.0.0/15`;
- `192.0.0.0/24`;
- the reserved block `240.0.0.0/4`.

A NAT64 address (`64:ff9b::/96`) or a 6to4 address (`2002::/16`) is judged by the IPv4 address that
it embeds. The same checks apply to redirects and at dial time. A non-200 answer from the endpoint
is reported by its status only. Its body is not returned. The built-in local Ollama default
(`http://127.0.0.1:11434/v1`) is permitted only when the request leaves `base_url` empty for the
`ollama` provider.

An operator who runs a self-hosted LLM gateway or Ollama on a LAN or a Docker bridge can allow
specific endpoints with `ARTEX_LLM_PRIVATE_ENDPOINTS`. It is a comma-separated list of exact
`host:port` entries, for example `127.0.0.1:11434,host.docker.internal:11434,192.168.1.10:11434`.
A URL whose `host:port` matches an entry can resolve to a loopback or private address, or to an
address in `100.64.0.0/10` or `198.18.0.0/15` (an overlay network, a fake-IP resolver). The scheme
restrictions, the link-local and metadata blocks, the other reserved ranges, and the redirect
validation still apply. Everything else stays refused. When `ARTEX_LLM_PRIVATE_ENDPOINTS` is unset
or empty, the strict public-only policy applies.

## Agent Process Isolation

This section describes the Docker deployment: a Linux server that runs as root with the privilege
drop active. On macOS, or when the server does not run as root, the drop is
off. The server logs a banner that says so. The properties below do not hold in that mode.

### The privilege drop

- The server runs as root.
- The processes that an agent starts run as an unprivileged user: Bash, the interactive shell,
  custom tools, and stdio MCP servers. The user and group come from `-exec-uid` and `-exec-gid`
  (default 10001). This user is the **executor**.
- The executor can write only the run directories and its own home (`<data>/exec-home`).

### Directory layout and permissions

- No agent run directory is an ancestor of another. The main agent and the planner work in
  `tasks/<id>/main`. A worker works in `tasks/<id>/i<N>`. A chat works in `sessions/<id>`.
- The parents (`tasks/`, `sessions/` and `tasks/<id>`) are `root:root` with mode 0755. The executor
  cannot create, rename or remove an entry there.
- Only the leaves are shared with the executor group (setgid, mode 0775).
- With the drop active, the server creates each level without following a symlink.
- A task can have entries directly under `tasks/<id>/` from the time before this layout: uploads,
  and files that the main agent wrote there. These entries are not moved. They are shared with the
  executor group again at start-up and on archive restore. `tasks/<id>` itself stays `root:root`
  0755. The new run directory `main/` of the main agent is empty on its first turn. The earlier
  artifacts stay under `tasks/<id>/` and are reachable through Bash only.

### Keys and environment

- `keys/` (`jwt.key`, `secret.key`), the config file and the MITM CA key are readable by root only.
- On Linux, the environment block of an agent process does not contain `ARTEX_PG_DSN`,
  `ARTEX_SECRET_KEY`, the LLM API keys, or the LLM proxy and base URLs.

### File tools

The file tools of an agent (Read, Write, Edit, MultiEdit, LS, Glob, Grep) run inside the server.
They reach only the run directory of the run:

- Paths are opened through `os.Root`. An absolute path elsewhere, `..`, and a symlink that leads out
  are refused.
- A run directory that is reached through a symlink is refused.
- The executor cannot replace a run directory or one of its parents.

One race remains: the executor can swap a path between the `EvalSymlinks` check and the open of a
file inside a run directory. The executor can do this only inside a leaf. Every executor process
has the same identity, so this is any leaf, never a leaf itself or its parents.

`ingest_bbot` reads its file through the same confinement.

### Custom-tool scripts

- With the drop active, a custom-tool script is written to `<data>/exec-scripts`. The directory is
  owned by root. The executor can read it and cannot change it.
- With the drop disabled, the script goes to `.tools/` under the run directory.

### Command-output spills

A spill is the full output of a tool call that is over the size cap. Norma writes it inside the
server.

- With the drop active, a spill goes to a root-owned directory per task or chat session, outside
  the run directory: `<data>/spill/tasks/<id>` or `<data>/spill/sessions/<id>`. The directory is
  root with the executor group, mode 0750, below parents that are `root:root` 0755. It is deleted
  and archived with the task, or deleted with the conversation. The executor cannot create, rename
  or remove an entry there.
- Norma writes a spill file with mode 0600. When the tool call returns, the server makes the file
  root with the executor group, mode 0640. The agent reads it with Bash. The file tools do not
  reach outside the run directory.
- With the drop disabled, a spill goes to `cmd-output/` under the run directory.

### Root reads and writes by path that stay open

These reads and writes remain open by path. A symlink that the executor places can redirect them,
so root can read or write outside the workspace:

- `ingest_scan_output` (a root read by path);
- chat uploads under `tasks/<id>/main/uploads/` or `sessions/<id>/uploads/`;
- the packaging of a task archive (`lstat`, then an open by path);
- the output of background tasks that Norma writes inside the server under
  `os.TempDir()/norma` (`<tmp>/norma/<session>/tasks/`), a path that ARTEX Program does not choose.

Rules for `<tmp>/norma` with the drop active:

- The server creates the directory `root:root` 0755 at start-up, before any agent process runs.
- The server refuses to start when the directory is a symlink or not a directory. It also refuses
  to start when a pre-existing `<tmp>/norma` is writable by the executor. Such a start can refuse
  a configuration that worked before. The remedy is to remove that directory, or to set `TMPDIR`
  to a directory that the executor cannot write. ARTEX Program never changes or deletes the
  directory.
- A directory that is already there under another owner is used as found, with a warning in the
  log.
- `TMPDIR` must be a path that root controls. The start-up check follows `TMPDIR` and inspects
  `<tmp>/norma` and its immediate parent only. A symlink, or an ancestor below a directory that
  the executor can write, is not detected. POSIX ACLs are not inspected.

### `ask_operator` image staging

The staging of an image for `ask_operator` is narrower than the paths above.

- It creates `tasks/<id>/uploads` with `Mkdir` and checks it with `Lstat`. A symlink, or anything
  that is not a directory, is refused.
- The copy gets a random name, `ask-operator-<unixnano>-<8 hex>.<ext>`. It is created with
  `O_EXCL` and mode 0640, so it never overwrites a file.
- The source is opened only when the `Lstat` of the path and the `Stat` of the opened file agree
  (`os.SameFile`).

What remains open:

- A directory component inside the run directory can be swapped for a symlink between the check
  and the open. Root can then read a file outside the run directory and stage a copy of it.
- An `uploads/` directory that existed before start-up or an archive restore is shared with the
  executor group and stays group-writable. A staged copy can be replaced after it is written, and
  the operator can see another image.
- With `fs.protected_hardlinks=0`, a hard link to a file that the executor cannot read is copied.
- A FIFO that is swapped in after the `Lstat` blocks the call.

### Container and limits

- The Docker image has no setuid programs. `docker-compose.yml` sets `no-new-privileges`.
- These properties are a containment layer, not a sandbox. An executor can still read files under
  the data directory that are readable by everyone. It can reach the loopback API and the database
  when the network allows it. It can use a credential that it received through the platform.
- The persistence tools (custom tools, MCP servers, skills) need operator approval by default.

## What to Expect

This is an open source project that is maintained on a best-effort basis. The target is to
acknowledge a report within about a week and to keep you updated. A specific response or fix time
is not guaranteed.

## If a Report Is Accepted

- A private **GitHub Security Advisory** is opened for the coordination.
- The release that contains the fix is identified when it is published.
- A **CVE** can be requested when the vulnerability qualifies.
- You or your organization are credited in the advisory, if you want that.
- The default disclosure time is **90 days** after a fix is available, or earlier by mutual
  agreement. When the vulnerability is exploited in the wild, the disclosure can be earlier.

## If a Report Is Declined

The reason is explained, for example:

- It is not a security vulnerability. It is intended behavior.
- It is outside the scope of ARTEX Program: an issue in an LLM provider, PostgreSQL, Docker, or a
  browser.
- There is not enough information to reproduce it.
- The report only states that "the tool can be misused". It does not identify a technical flaw in
  ARTEX Program.

When it is appropriate, the report becomes a public hardening issue or a feature request.

## Out of Scope

These are generally **not** security vulnerabilities of ARTEX Program:

- attacks or scans against systems that you do not own or are not authorized to test;
- self-XSS, spam, or brute force without a real impact;
- large-scale DoS, DDoS, or resource-exhaustion attacks;
- social engineering that targets maintainers or users;
- issues that exist only in third-party dependencies and do not affect ARTEX Program;
- reports that "ARTEX Program can be used to attack systems" when the user has a valid
  authorization. This is the nature of a security testing tool, not a vulnerability;
- a missing header or a hardening configuration without a concrete security impact.

## Safe Harbor

Ethical security research is welcome. When you follow this policy and test only within an
authorized scope, no legal action is taken against you for good-faith research. This does not
exempt you from the laws of your jurisdiction.

## Disclosure Policy

- Do not disclose vulnerability details in public before a fix is available, or before the
  coordinated disclosure period ends.
- Do not publish a public PoC without the consent of the maintainers.
- When you receive no response within a reasonable time, contact the channel above before you
  disclose.

Thank you for helping to make ARTEX Program safer.
