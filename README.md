<div align="center">

<img src="web/public/logo.svg" width="84" alt="ARTEX Program logo" />

# ARTEX Program

**Autonomous Solana program security testing that keeps its receipts.**

Self-hosted, LLM-driven security testing for Solana programs. A planner explores. Workers run real
tools (`solana`, `anchor`, `surfpool`, your scripts). Every finding stays bound to the assets and
the recorded on-chain evidence that prove it.

[![License](https://img.shields.io/github/license/GuiBibeau/artex-program?color=6b34db)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/GuiBibeau/artex-program?color=6b34db)](go.mod)

[Quick start](#quick-start) · [Testing a Solana program](#testing-a-solana-program) · [How it works](#how-it-works) · [Install](#installation) · [Configuration](#configuration) · [Security](SECURITY.md)

</div>

---

ARTEX Program is a self-hosted security testing platform for Solana programs. LLM agents do the
work. A planner splits a goal into intents. Workers execute the intents with real tools. The
results go into **two linked graphs**. One graph holds what you know about the program. The other
graph holds how far the test has progressed. A finding can carry the JSON-RPC exchange that proves
it, for example a simulation or a transaction. Every autonomous action passes through scope rules,
approval gates and budgets.

ARTEX Program is a fork of [ARTEX](https://github.com/BaaRai/ARTEX), re-oriented to Solana programs.

- **It explores, it does not chat.** Goal decomposition, intent dispatch, worker execution and
  write-back form a closed loop. The loop advances until the goal is proven.
- **Two graphs, one audit trail.** The shared asset graph says *what exists*: the program and
  everything registered under it. The per-task exploration graph says *what was tried, what it
  produced, and why*. Anchors link the two.
- **Findings with evidence.** A recording proxy captures every RPC call the agents make. A
  finding is tied to the exact exchange on the RPC host of the cluster in scope.
- **Autonomy with brakes.** The operator approves every command that moves funds or changes
  program or token state. Scope and cluster rules, token and turn budgets, RPC pacing, and
  prompt-injection framing on tool output and on-chain data apply to every action.
- **Self-hosted and model-agnostic.** One Go binary with the Next.js UI embedded, PostgreSQL,
  your own Anthropic or OpenAI-compatible endpoint, or a local Ollama.

## Testing a Solana program

1. **Install the toolchain the agents call**, on the host or in the image: the Solana CLI, the
   Anchor CLI, Surfpool, a Rust toolchain, and Node 20 or later for scripts. The Docker image has
   all of them. See [Installation](#installation).
2. **Create a task.** Name the target in the goal or the description: the program id and the
   cluster. Example: *"Audit program `Eq...` on devnet. Look for missing signer and owner checks,
   PDA derivation flaws, and any way to move vault funds. Live transactions on devnet only, at
   most 0.5 SOL."* The goal decomposer registers the `program` and `cluster` scope entries,
   extracts your operation constraints, and splits the goal into verifiable results.
3. **Let the loop run.** The planner dispatches intents. Each worker maps the program: the program
   account and upgrade authority, the IDL or the bytecode, the instructions, the accounts, the
   PDAs, the authorities, the mints. The worker tests the constraints it finds. It proves
   candidates with `simulateTransaction`, a Surfpool fork or a local validator first. It registers
   instructions, accounts, authorities, mints and token accounts in the asset graph as it goes.
4. **Approve what needs approval.** Reads and simulation run without approval. Other commands ask
   the operator first:

   - a command that moves funds, or changes staking, voting, program or token state;
   - a deploy or an upgrade with `anchor`;
   - a transaction that a command line submits.

   The target gate (opt-in) also asks before the first connection to an RPC host.
5. **Fund the proof when the goal needs a live transaction.** Workers own no funds. A worker
   creates a wallet in its work directory. It seals the keypair in the task credentials. It asks
   you through `ask_operator` to run the exact funding command: an airdrop on devnet or testnet,
   or a transfer on mainnet-beta. It never prints the key.
6. **Read the evidence.** A finding binds the recorded exchanges in reproduction order: control
   call, attack, verification. The report writer turns them into a Markdown report. The report has
   a measured impact, the proof basis (executed live, executed lab, or static analysis) and a
   remediation.

Skills ship with the platform and teach the method: `solana-recon` (map a program on a cluster),
`anchor-audit` (review Anchor source), `solana-bytecode` (analyze SBF without source) and
`solana-exploit` (from candidate to executed proof), plus the generic `codeaudit`, `patch-diff`
and `third-party-code-review`. [CONTEXT.md](CONTEXT.md) defines the terms the code, the prompts
and the skills share.

### Changes from ARTEX

- **Asset model.** Six Solana asset types keyed by on-chain address: `program`, `instruction`,
  `account`, `authority`, `mint`, `token_account`. Each child carries its program id.
- **Scope.** Two scope kinds: `program` and `cluster`.
- **Proof.** A finding on a Solana asset closes a goal through the recorded JSON-RPC exchange on
  the RPC host of the task's cluster scope.
- **Prompts.** The six built-in prompts are written for Solana program testing. They include the
  money rules and the wallet funding protocol. An existing ARTEX database gets the new prompts
  once per agent.
- **Skills.** Four Solana skills replace the web-only skills.
- **Approval rules.** Four built-in rules ask the operator before funds, program state, token
  state or a raw transaction submission change.
- **Image.** Pinned Solana CLI, Anchor, Surfpool and a Rust toolchain. No web scanners.
- **Worker shells.** Loopback and the package registries bypass the recording proxy.

## Quick start

```bash
git clone https://github.com/GuiBibeau/artex-program.git
cd artex-program
cp .env.example .env            # set POSTGRES_PASSWORD (and optionally an LLM key)
docker compose up -d --build    # builds from this checkout, starts PostgreSQL
```

The image is `linux/amd64`. On an arm64 host, set the platform first:

```bash
export DOCKER_DEFAULT_PLATFORM=linux/amd64
```

Open **http://localhost:8787**. On the first visit, `/setup` asks for the one-time setup token
that the server prints at startup:

```bash
docker compose logs artex | grep "Setup token"
```

## Multi-provider workers

A task can use more than one LLM profile. Set it up in three places.

1. In **System → LLM**, configure the profiles.
2. In **System → Settings**, set the concurrent worker count to at least **2**. The count applies
   to tasks that start later.
3. In **System → Agents → Worker**, leave the model set to follow the task. A fixed Worker model
   takes precedence over rotation.

When you create a task, select the profiles in **LLM config chain**, set their order, and enable
**Enable multi-LLM worker rotation**. For an existing task, open its LLM config control, change
the switch or the order, and save.

Rotation works like this:

- It assigns successive worker intents to profile A, then B, then A, and so on. Completion order
  does not matter.
- Separate intents can run at the same time on different providers. Each intent keeps its
  provider unless that provider fails over.
- The planner and the other unbound roles use the task's current profile.
- When rotation is off, the task uses ordered failover.
- An excluded provider is skipped. An intent that is bound to an exhausted profile falls back to
  the task's current profile. It does not retry the exhausted profile.
- Rotation cycles through the whole list. It does not rebalance around a dead provider.
- When you save the chain, the failure state of the profiles resets. Calls that run now are not
  interrupted.
- The rotation cursor starts again after a server restart. Saving the chain does not reset it.
  The profile order and the switch stay saved.

The task creation API and `PUT /api/tasks/{id}/llm` accept `llm_rotate: true` with the ordered
`llm_profile_ids`. If you omit `llm_rotate` on an update, the value stays.

## Screenshots

The screenshots show the interface. The **Solana programs** tab in the asset views is new and is
not on them.

| Tasks | Findings |
| :---: | :---: |
| ![Tasks](screenshots/tasks.png) | ![Findings](screenshots/findings.png) |

| Task execution: sessions and tool calls | Exploration graph |
| :---: | :---: |
| ![Execution](screenshots/sessions.png) | ![Exploration graph](screenshots/graph.png) |

| Asset coverage graph | Traffic recording |
| :---: | :---: |
| ![Asset coverage graph](screenshots/assets_test.png) | ![Traffic](screenshots/traffic.png) |

| Human-in-the-loop chat | Interception approvals |
| :---: | :---: |
| ![Chat](screenshots/chat.png) | ![Intercept](screenshots/intercept.png) |

<details>
<summary>More screenshots (assets, agents, LLM, logs)</summary>

| Assets | Agent management |
| :---: | :---: |
| ![Assets](screenshots/assets.png) | ![Agent](screenshots/agents.png) |

| LLM configuration | Backend logs |
| :---: | :---: |
| ![LLM](screenshots/llm.png) | ![Logs](screenshots/logs.png) |

</details>

---

## How it works

### Dual-graph architecture: exploration graph + asset graph

ARTEX Program separates *what the goals are* from *how far testing has progressed*. It keeps two
graphs and links them with anchors.

- **Asset graph (shared across tasks).** One source of truth for Solana assets: `program /
  instruction / account / authority / mint / token_account`. Each asset is keyed by its on-chain
  address. Companies (a protocol or a team) own the assets. The network assets around a program
  stay in the model: `root_domain / subdomain / ip / service / app / endpoint`. The program
  computes the parent-child relationships and the deduplication keys. Agents submit raw
  observations only.
- **Exploration graph (per task).** The reasoning and the progress of the task: `goal / intent /
  fact / finding / hint` nodes, connected by `spawns / derived_from / yields / proves` edges into
  a lineage chain.
- **Anchors link them.** `exploration_anchors(node_id, asset_id)` ties intents, facts and findings
  to concrete assets. It shows which assets a direction touched, and which intents tested a given
  asset. It also powers **asset test coverage** and the coverage graph.

```mermaid
flowchart LR
  subgraph EG["Exploration graph (per-task · advance chain)"]
    direction TB
    G["goal"]
    I1["intent A"]
    F1["fact"]
    I2["intent B"]
    FD["finding (vulnerability)"]
    G -->|spawns| I1
    I1 -->|yields| F1
    F1 -->|derived_from| I2
    I2 -->|proves| FD
  end
  subgraph AG["Asset graph (globally shared · source of truth)"]
    direction TB
    PR["program"]
    IN["instruction"]
    AC["account / PDA"]
    AU["authority"]
    MI["mint / token account"]
    PR --> IN
    PR --> AC
    PR --> AU
    PR --> MI
  end
  I1 -. anchor .-> IN
  F1 -. anchor .-> AC
  I2 -. anchor .-> AU
  FD -. anchor .-> MI
```

The **planner** reads the exploration graph, judges the goals, and dispatches an intent only when
a direction is not covered yet. A **worker** claims one intent, executes it, writes assets, facts
and findings back into both graphs, then stops.

### The engine loop

The engine is event-driven. A graph change wakes the planner. Workers claim intents and execute
them. The write-back triggers the next round. The loop ends when `prove_goal` closes the task.

```mermaid
sequenceDiagram
  autonumber
  participant EV as graph-change debounce
  participant P as planner
  participant FR as frontier intent queue
  participant W as worker
  participant PX as recording proxy
  participant DB as dual graphs + activity

  EV-->>P: wake up
  P->>DB: read situation (graph_overview prefetch + coverage/scope)
  P->>FR: dispatch 0..N intents (with asset_ids)
  Note over P,FR: most wake-ups dispatch 0 (no new direction means done)
  W->>FR: claimNext claims one intent
  W->>DB: fetch the raw assets of the intent's asset_ids
  W->>PX: real tool execution (solana / anchor / surfpool / Bash / curl)
  PX-->>W: JSON-RPC response (recorded: the finding's receipt)
  W->>DB: write back fact / asset / finding + per-step activity
  DB-->>EV: graph change
  EV-->>P: wake again (closed loop)
```

### Durable attack chains, shared worker memory

A real attack chain has several steps in a fixed order. Example: find a missing signer check,
build the attack transaction, drain the vault on a fork, confirm on devnet. The planner keeps a
**todolist per task across wake-ups**. It records the chain once. It dispatches a step only when
facts show that its prerequisites are satisfied.

Workers can also **search the execution traces of other workers** (`search_all_worker_traces`,
`list_worker_traces`, `get_worker_trace`). An observation that never became a formal fact is still
reusable: a program log line, an unchecked account, a bump that worked. Each worker stays confined
to the intent it claimed.

---

## Features

### Evidence, not chat logs

- **Recording proxy** (`:8788`) with a generated CA. It traces every RPC call and HTTP request that
  the agents make through Bash. A JSON-RPC exchange (a `simulateTransaction` result, a sent
  transaction) can be bound to a finding.
- **Live-transaction approvals.** The built-in rules ask the operator before these commands:
  `solana` money, staking, voting and program commands; `spl-token` state changes; `anchor deploy`
  and `anchor upgrade`; a command line that calls `sendTransaction`. Reads and simulation run
  without approval.
- **Funded-wallet protocol.** A worker creates test wallets in its work directory. It seals the
  keypairs in the task credentials. It asks the operator for the funds. No key reaches a fact, a
  finding or the context of the model.
- **Browser extension** (`extension/`) for the web assets around a program, such as a frontend or a
  documentation portal. Log in once and capture the session. The requests and responses land in the
  same traffic store.

  <details>
  <summary>Using the traffic capture extension</summary>

  - Load the unpacked extension from `extension/` (Chrome or Edge: Developer Mode → Load unpacked).
  - In the popup, set your ARTEX Program URL and token. Then press **Start capture** on the tab that
    you browse.
  - Use the site. Text requests and responses go in batches to `POST /api/traffic/ingest`
    (authenticated). Binary bodies (images, fonts) are skipped. Bodies are truncated at 256 KiB.
    A batch holds at most 50 entries.
  - Press **Stop** when you are done. Everything stays in your local traffic store.

  </details>
- **Verified findings.** A finding has a POE level, a verification status and a note. A finding is
  `verified` only when a bound proof or response and verbatim evidence quotes support it. Gzip and
  deflate bodies are decoded before the quote check. The findings table supports bulk status
  updates. The report and the export show the proof strength of each finding next to its status.
- **Traffic replay.** Open a captured exchange and replay the recorded request (method, URL,
  headers and stored body) from the traffic detail. Replay stays disabled while only the body
  preview is loaded.
- **Vulnerability retest loop.** A `retester` agent runs a finding again, with optional fixed
  versions or conditions. It records `Still reproducible`, `Fixed` or `Cannot confirm`. It can move
  the finding to `Fixed`. The original evidence and reports are preserved.
- **Reports and exports.** Findings, evidence and task archives. Traffic snapshots are encrypted at
  rest.

### Control without slowing down

- **Interception gate.** Every tool call can require human approval. Rules match by `string`,
  `regex` or **argv prefix**. An argv rule is a JSON array of 1 to 8 tokens (Bash only, for example
  `["git","push"]`). It matches only a single simple command whose parsed prefix equals the
  pattern. Pipes, lists, redirections, prefix assignments, subshells, background jobs, wrapper
  programs and command or parameter substitutions do not match. Flags are ignored, so
  `["git","push"]` covers `git push --force`, but `git -C /repo push` matches nothing. Each approval
  records who decided: `human`, `rule`, `model` or `system`.
- **Scope and target gate.** Scope rows define the coverage denominator. Assets outside the
  declared scope are not explored. The target gate (off by default, API only) can refuse
  destinations that were not approved.
- **Budgets.** `ARTEX_TASK_BUDGET_TOKENS` and `ARTEX_TASK_BUDGET_TURNS` cap a whole task (planner
  and workers). `ARTEX_INTENT_BUDGET_TOKENS` and `ARTEX_INTENT_BUDGET_TURNS` cap one worker run.
  Near the limit the task enters a forced wrap-up. A worker does not start when the remaining budget
  cannot cover it. Spend that is already in `llm_usage` is restored after a restart. A task that is
  already inside the wrap-up reserve has only that reserve left. The revived run then wraps up; it
  does not start another worker. The cost estimate per worker starts again from the defaults
  (50,000 tokens and 10 turns) until a worker finishes in this process.
- **Noise governor.** `ARTEX_NOISE_PROFILE=quiet|balanced|aggressive` paces the requests that ARTEX
  Program sends to targets: per-host rate, burst, in-flight limit, jitter, backoff on 429 and 503,
  and a short GET cache. The agents also receive RPC pacing limits: requests per second per RPC
  host, and one `getProgramAccounts` page or simulation batch at a time.
- **Untrusted-output boundary (on by default).** Tool results are framed between nonce-tagged
  markers. The model treats them as data, not as instructions. Set `ARTEX_UNTRUSTED_BOUNDARY=0` to
  disable it.

### Assets and coverage

- **Coverage ledger and coverage graph.** In-scope assets are shown as tested or untested, with
  the linked intents, facts and findings of each node.
- **Program-scoped asset graph.** Shared across tasks and deduplicated by the program. Containment:
  `program → instruction / account / authority / mint / token_account`. A `program` scope covers the
  program and every asset registered under it. A `cluster` scope names the RPC endpoint that the
  evidence must come from.
- **On-chain search.** The asset search language reaches `address`, `program_id`, `name` and
  `chain`. Example: `parent==<program id> AND address=<account>`.
- **Task categories, templates, relations and archives** for repeatable engagements.

### Platform

- **LLM profiles with failover.** Anthropic, OpenAI, OpenAI-compatible relays and keyless Ollama.
  A task can use several profiles, with automatic failover.
- **Token accounting.** Usage per task and per session, cache reads and writes, and an LLM record
  view for every call.
- **MCP servers, skills, memory and a workspace.** Extend the agents with remote tools and reusable
  material.
- **Web search.** The search tool of the agent uses Brave, Tavily or the opt-in Keenable backend.
  Configure it in **System → Settings**. The key is encrypted at rest. Search can also egress
  through its own proxy.
- **Single binary.** The Next.js UI is embedded with `go:embed`. PostgreSQL is the only external
  dependency.

---

## Installation

Requirements: **PostgreSQL** 16 or later. Exploration requires an **LLM**: `ANTHROPIC_API_KEY` or
`OPENAI_API_KEY`, or a provider that you configure in the UI.

### Option 1: install script

```bash
git clone https://github.com/GuiBibeau/artex-program.git
cd artex-program
./install.sh
```

The script detects or installs Docker. It then offers two modes:

- **① all-Docker**: it writes `.env` and runs `docker compose up -d --build`.
- **② local build and run**: it generates `config.json`, builds the embedded binary, and starts it.

Open **http://localhost:8787**. `/setup` asks for the one-time setup token that the server prints
at startup (`docker compose logs artex | grep "Setup token"`).

The UI listens on `127.0.0.1` by default. To reach it from another machine, or from Windows when
ARTEX Program runs inside WSL, set `ARTEX_BIND_ADDR=0.0.0.0` in `.env`. The setup token still
protects the first-run claim. You can also keep loopback and use a reverse proxy or an SSH tunnel.

### Option 2: Docker

Build from a checkout with Docker Compose:

```bash
git clone https://github.com/GuiBibeau/artex-program.git
cd artex-program
cp .env.example .env            # set POSTGRES_PASSWORD, optionally ANTHROPIC_API_KEY
DOCKER_DEFAULT_PLATFORM=linux/amd64 docker compose up -d --build
# → http://localhost:8787
```

`DOCKER_DEFAULT_PLATFORM` is necessary on an arm64 host only.

The checkout build needs the Docker **buildx** plugin (BuildKit). Docker Desktop and get.docker.com
include it. Distro packages and the Homebrew docker CLI (Colima) may not include it. If
`docker buildx version` fails, install `docker-buildx-plugin` or `docker-buildx`. On macOS, run
`brew install docker-buildx`. Without buildx, the build stops at the first stage with
`failed to parse platform`.

The image contains:

- the Solana toolchain: `solana`, `solana-keygen`, `spl-token`, `solana-test-validator`,
  `cargo-build-sbf`, `anchor`, `surfpool`;
- a Rust toolchain;
- Node 22 for scripts;
- the built-in skills in `/app/skills`.

The image is `linux/amd64` only. The Solana CLI, Anchor and Surfpool publish no Linux arm64
binaries.

#### Prebuilt image

The release workflow publishes an image to GitHub Container Registry on each `v*` tag, with the
tags `:latest`, `:v<version>` and `:<version>`. The image is `linux/amd64`.

> **No release is published yet.** Until the first `v*` tag of this repository exists, build from
> the checkout as shown above. `docker-compose.ghcr.yml` and the `docker pull` commands below fail
> with `denied` until then. After the first publish, set the package visibility to Public. The
  release workflow checks that the image pulls anonymously, and fails the job if it does not.

The standalone container needs a PostgreSQL database that it can reach. Set the `ARTEX_PG_*`
variables. The example uses `host.docker.internal` of Docker Desktop to reach PostgreSQL on the
host. On native Linux Docker Engine, add `--add-host=host.docker.internal:host-gateway` to
`docker run` for a host database. Or use the hostname of your database server.

```bash
docker pull ghcr.io/guibibeau/artex-program:latest

mkdir -p data keys
docker run -d --name artex \
  --security-opt no-new-privileges:true \
  -p 127.0.0.1:8787:8787 \
  -p 127.0.0.1:8788:8788 \
  -e ARTEX_PG_HOST=host.docker.internal \
  -e ARTEX_PG_PORT=5432 \
  -e ARTEX_PG_USER=artex \
  -e ARTEX_PG_PASSWORD='yourpass' \
  -e ARTEX_PG_DBNAME=artex \
  -e ARTEX_PG_SSLMODE=disable \
  -e ARTEX_JWT_KEY_DIR=/app/keys \
  -v "$PWD/data:/app/data" \
  -v "$PWD/keys:/app/keys" \
  ghcr.io/guibibeau/artex-program:latest
# → http://localhost:8787  (docker logs artex | grep "Setup token")
```

In `docker-compose.yml` and `docker-compose.ghcr.yml`, `POSTGRES_PASSWORD` is passed to ARTEX
Program as its own variable. The server percent-encodes it into the connection URL. A password can
contain `/`, `?`, `%`, `@`, `:` or spaces. In `.env`, quote a value that contains `#`, and write
`$$` for a literal `$`. The `ARTEX_PG_*` variables take the password raw. Quote it in a shell.

The GHCR Compose file persists `./keys` and `./data`. It does not mount a host `skills/` directory,
so the image uses its built-in skills.

#### Compose stack from the prebuilt image

`docker-compose.ghcr.yml` runs the same stack (ARTEX Program and PostgreSQL) from the released
image, with no checkout build. It works after the first image is published. Download the file and
`.env.example` once:

```bash
curl -fsSLO https://raw.githubusercontent.com/GuiBibeau/artex-program/main/docker-compose.ghcr.yml
curl -fsSLO https://raw.githubusercontent.com/GuiBibeau/artex-program/main/.env.example
cp .env.example .env            # set POSTGRES_PASSWORD, optionally an LLM key
docker compose -f docker-compose.ghcr.yml pull
docker compose -f docker-compose.ghcr.yml up -d
# → http://localhost:8787  (docker compose -f docker-compose.ghcr.yml logs artex | grep "Setup token")
```

Update the stack without a git checkout:

```bash
docker compose -f docker-compose.ghcr.yml pull
docker compose -f docker-compose.ghcr.yml up -d
```

Pin a version with `ARTEX_IMAGE_TAG=v0.7.1` in `.env` (or `0.7.1`). If the variable is not set,
`latest` is used. To use custom skills, copy them into a host directory and add
`- ./skills:/app/skills` to the `artex` volumes. The mount then replaces the built-in
`/app/skills`.

For a remote MCP server, you can choose `http` (Streamable HTTP) or `sse` (legacy SSE) in the system
settings. A legacy SSE server usually opens the event stream with `GET /sse` and receives JSON-RPC
over `/message?sessionId=...`. Configure the URL as `/sse`, and send `Authorization: Bearer <token>`.

### Option 3: prebuilt binaries

Each release on the [Releases page](https://github.com/GuiBibeau/artex-program/releases) has a zip
for each platform. No release is published yet. A zip contains `artex`, `start.sh` (`start.bat` on
Windows), `skills/` and `config.example.json`:

```bash
cp config.example.json config.json   # fill in the database connection
./start.sh                           # → http://localhost:8787
```

Start with `start.sh` or `start.bat`, not with `./artex`. The script is a daemon script that
restarts the process after a backoff. To keep it in the background: `nohup ./start.sh >artex.log 2>&1 &`.

#### Verifying a release

Each release attaches `SHA256SUMS` and a CycloneDX SBOM, `artex-<tag>.cdx.json`, next to the zips.
The SBOM lists the Go modules in the linux/amd64 binary, the frontend `package-lock.json`, `go.mod`
and `go.sum`. The zips also carry a GitHub build provenance attestation.

```bash
sha256sum -c SHA256SUMS                                    # add --ignore-missing if you downloaded only some zips
gh attestation verify <zip> --repo GuiBibeau/artex-program   # built by the release workflow of this repository
```

### Option 4: build the single binary from source

```bash
cd web && npm ci && npm run build:static && cd ..
cp -r web/out server/webui/dist
CGO_ENABLED=0 go build -tags embedui -o artex ./cmd/artex
./start.sh
```

Only `-tags embedui` embeds the frontend into the binary.

### Option 5: release archives

`build.sh` builds and embeds the frontend, strips debug info, and writes zips for Linux amd64 and
arm64, macOS amd64 and arm64, and Windows amd64:

```bash
./build.sh --release
# output: dist/artex-<version>-*.zip

ARTEX_TARGETS=linux/amd64,windows/amd64 ./build.sh --release
./build.sh --target linux/amd64 --upx
```

UPX is off by default. Self-extracting binaries can be incompatible with some kernels,
virtualization environments or security policies. Pass `--upx` only after you confirm that the
target runtime supports it.

---

## Configuration

**Database**: `config.json`, `ARTEX_PG_DSN`, or the `ARTEX_PG_*` environment variables. See
[External PostgreSQL](#external-postgresql).

```json
{
  "database": {
    "host": "127.0.0.1", "port": 5432,
    "user": "artex", "password": "yourpass",
    "dbname": "artex", "sslmode": "disable"
  }
}
```

**LLM**: run `export ANTHROPIC_API_KEY=sk-...` (or `OPENAI_API_KEY`), or configure a provider on
the **LLM Configuration** page of the UI. The page can also add a local Ollama server as a keyless
provider (format "Ollama", default base URL `http://127.0.0.1:11434/v1`). Optional variables:
`ARTEX_LLM_PROVIDER` (`anthropic | openai | openai-responses | ollama | bedrock-gateway`),
`ARTEX_LLM_MODEL`, `ARTEX_LLM_BASE_URL`, `ARTEX_LLM_PROXY`.

**Base URL rule.** For the Anthropic format, enter the host **without** `/v1`, for example
`https://api.anthropic.com`. ARTEX Program appends `/v1/messages`. OpenAI-format providers append
`/chat/completions` (or `/responses` for `openai-responses`). Their base URL **ends in** `/v1`, for
example `https://api.openai.com/v1`.

**OpenAI-compatible gateways.** A provider that serves an OpenAI-style `/v1/chat/completions`
endpoint works through the **OpenAI** format. No dedicated format is necessary.

1. Create a profile with format **OpenAI**.
2. Set **Base URL** to the `/v1` root of the gateway.
3. Paste the key.
4. Click the **↻** button next to *Model* to load the catalog of the provider (`GET /v1/models`),
   then pick a model. Models that the provider adds later appear on the next refresh.

**Bedrock gateway.** Select **Bedrock gateway** in System → LLM for a gateway that exposes the
Bedrock `InvokeModel` and `InvokeModelWithResponseStream` paths and accepts bearer-token
authentication.

- Enter the base URL of the gateway (for example, `https://gateway.example.com/bedrock`), the full
  Bedrock model ID, and the token in **Bearer token**.
- If required, enter static headers as one `Name: Value` per line in **Custom gateway headers**.
- ARTEX Program adds `/model/{model}/invoke` (or `/invoke-with-response-stream`), sends
  `anthropic_version: bedrock-2023-05-31`, and decodes the binary event stream of Bedrock.
- Enter the model ID manually. This gateway format has no model-list operation.
- The token and the custom header values are encrypted in the database. They are write-only in the
  UI. When you edit the gateway URL, you must enter the token again. A saved credential can
  therefore not be sent to a different host by a URL change.
- This format is for bearer-authenticated gateways. Direct AWS Bedrock with SigV4 credentials is not
  supported.

For an environment-based setup, set `ARTEX_LLM_PROVIDER=bedrock-gateway`, `ARTEX_LLM_BASE_URL`,
`ARTEX_LLM_MODEL` and `ARTEX_LLM_API_KEY`. The optional `ARTEX_LLM_CUSTOM_HEADERS` uses the same
newline-separated `Name: Value` format.

**Task archive storage**: local disk by default. Set `ARTEX_ARCHIVE_STORAGE=s3` to store cold task
packages in an S3-compatible bucket. The local package space is released after verification.
Existing local archives stay usable. See [Task archive storage](docs/task-archive-storage.md) for
the configuration, permissions, integrity checks, temporary disk requirements and recovery.

**Concurrency**: the number of workers per task, in **System Settings** (default 3).

**Untrusted-output boundary (on by default)**: the tool results for the worker, the planner, the
chat and the main agent are framed between nonce-tagged markers. The model treats them as data, not
as instructions. If the variable is unset or empty, the boundary is on. Set
`ARTEX_UNTRUSTED_BOUNDARY=0` (or `false`, `off`, `no`) to disable it. Cost: about 154 bytes per tool
result, plus one system note.

**Noise profile (off by default)**: `ARTEX_NOISE_PROFILE` paces the requests that ARTEX Program
sends to targets (WebFetch, HTTP custom tools, the enrich probe). If the variable is unset or `off`,
there is no limit and no cache.

- `quiet` = 2 requests per second per host (burst 4, 1 in flight).
- `balanced` = 5 requests per second per host (burst 10, 2 in flight).
- `aggressive` = 20 requests per second per host (burst 40, 4 in flight).

A profile adds jitter. It backs off on 429 and 503: it honors `Retry-After`, bounds the retries, and
applies to GET, HEAD and OPTIONS only. It deduplicates identical GETs that are in flight. It caches
them for a short time (60, 30 or 10 seconds), and never caches POST, PUT, PATCH or DELETE. It logs
one noise line per host each minute. `ARTEX_TARGET_USER_AGENT` sets a User-Agent on target requests that carry
none. With traffic capture on, the proxy also paces the Bash commands that use it (curl, wget,
python-requests). They get the same rate and jitter per host, and the same per-host log lines.
The in-flight caps, the retries and the cache apply only to the requests of ARTEX Program. The
profile reduces noise. It is not a security boundary. A command that bypasses the proxy
(`--noproxy`, or a removed proxy variable) is not paced. With capture off, nothing that starts from Bash is paced.

The **Target noise** card in the system settings sets the profile at run time, with no restart. It
shows the per-host counters. After you save it, that setting takes precedence over
`ARTEX_NOISE_PROFILE`.

**Scanner User-Agent (opt-in per task)** adds a stable ARTEX User-Agent to supported target
requests. Defenders can then correlate the requests with an approved assessment. See the
[configuration and WAF triage guide](docs/scanner-user-agent.md).

**Agent rate guidance (off by default)**: with a noise profile set, each agent that can run commands
receives a prompt block. The block holds the profile limits. It tells
the agents to work on one host at a time. It tells them to back off on 429 and 503, and never to
raise the rate to meet a deadline.

**Run budgets (off by default)**: `ARTEX_TASK_BUDGET_TOKENS` and `ARTEX_TASK_BUDGET_TURNS` cap the
total spend of a task (planner rounds and workers). `ARTEX_INTENT_BUDGET_TOKENS` and
`ARTEX_INTENT_BUDGET_TURNS` cap one worker run. Unset or `0` means unlimited.

- A worker does not start when the remaining task budget cannot cover its estimated cost. The
  estimate is the average of the finished workers. Without a finished worker, it is 50,000 tokens
  and 10 turns.
- Near the wrap-up reserve (the larger of 5% and 20,000 tokens, and 2 turns), the task enters a
  forced wrap-up. The planner writes a summary and a final goal judgment. Workers can only write
  back. The task then ends `done` if its goals are met, else `timeout`.
- Practical floor: below 50,000 tokens, no worker starts. 50,000 to 70,000 gives one worker, then
  wrap-up. Choose a value well above 70,000 for real exploration.
- **The token budget counts every token of every request, with the full context of the turn.** A
  worker context of 80,000 tokens costs 80,000 budget tokens for each turn. This is also true when
  the provider read most of the context from its cache. A worker run of 30 turns then costs about
  2.4 million budget tokens. Size `ARTEX_INTENT_BUDGET_TOKENS` as `turns × context`. Size the task
  budget as the sum over the worker runs that you expect. In a review of two programs, one worker
  context reached 90,000 tokens. 89% of the input tokens were cache reads.
- Planner, worker, judge and compactor rows in `llm_usage` are counted back into the cap when a
  ledger is created. This includes a restart and a revived run. A restored task that is already in
  the wrap-up reserve gets no other worker: only that reserve remains, and the run wraps up.
- Chat, goal decomposition and side questions are stored on the same task and are not counted.
- The average per worker is not restored (usage rows have no run boundary). The estimate starts
  from the defaults until a worker finishes in this process.

**Common flags**: `./start.sh -addr :8787 -proxy :8788`. `-addr` is the frontend and API address.
`-proxy` is the traffic recording proxy address.

### External PostgreSQL

ARTEX Program supports an external PostgreSQL 16 or later instance. The `postgres` service in
`docker-compose.yml` is only the bundled default. The database is startup configuration and the
single source of truth of the system. The UI has no database switcher. To use another instance,
change the environment variables and restart the server.

Configure the connection with a full connection string or with individual parts:

- **`ARTEX_PG_DSN`**: a full PostgreSQL connection URL, for example
  `postgres://artex:pass@db.example.internal:5432/artex?sslmode=require`. When it is set, it takes
  precedence over the `ARTEX_PG_*` variables. You must percent-encode URL-special characters in the
  username and the password.
- **`ARTEX_PG_*` parts** (recommended when a password contains `/`, `?`, `%`, `@`, `:` or spaces).
  ARTEX Program percent-encodes the user and the password, and removes `ARTEX_PG_PASSWORD` from the
  environment at startup:
  - `ARTEX_PG_HOST`: the PostgreSQL host.
  - `ARTEX_PG_PORT`: the PostgreSQL port (default `5432`).
  - `ARTEX_PG_USER`: the database user.
  - `ARTEX_PG_PASSWORD`: the database password (raw, not URL-encoded).
  - `ARTEX_PG_DBNAME`: the database name.
  - `ARTEX_PG_SSLMODE`: the TLS mode (`disable`, `require`, `verify-ca`, `verify-full`).

#### Compose without the bundled `postgres` service

To run against an external PostgreSQL server with Docker Compose, remove the `postgres` service,
the `depends_on` block and the `pgdata` volume from `docker-compose.yml`. Then point `artex` at
your database:

```yaml
services:
  artex:
    image: ghcr.io/guibibeau/artex-program:latest
    security_opt:
      - "no-new-privileges:true"
    environment:
      # Option A: individual parts (recommended; percent-encodes special characters)
      ARTEX_PG_HOST: ${ARTEX_PG_HOST:?Set ARTEX_PG_HOST in .env}
      ARTEX_PG_PORT: ${ARTEX_PG_PORT:-5432}
      ARTEX_PG_USER: ${ARTEX_PG_USER:-artex}
      ARTEX_PG_PASSWORD: ${ARTEX_PG_PASSWORD:?Set ARTEX_PG_PASSWORD in .env}
      ARTEX_PG_DBNAME: ${ARTEX_PG_DBNAME:-artex}
      ARTEX_PG_SSLMODE: ${ARTEX_PG_SSLMODE:-require}
      # Option B: full DSN (overrides ARTEX_PG_* when set)
      # ARTEX_PG_DSN: ${ARTEX_PG_DSN}
      ARTEX_JWT_KEY_DIR: /app/keys
    ports:
      - "${ARTEX_BIND_ADDR:-127.0.0.1}:8787:8787"
      - "127.0.0.1:8788:8788"
    volumes:
      - ./data:/app/data
      - ./keys:/app/keys
    restart: unless-stopped
```

#### Local disk usage (`data/`)

An external PostgreSQL does **not** move the runtime `data/` directory off local disk. `data/` still
stores these items:

- spilled HTTP and TCP traffic blobs (`data/traffic/_blobs`);
- LLM transcripts;
- task working files;
- local task archives.

A heavy workload can fill the host disk when `data/` is on a small volume. Mount `data/` on a volume
of the necessary size. You can also set `ARTEX_ARCHIVE_STORAGE=s3` to move cold task archives to
object storage. See [Task archive storage](docs/task-archive-storage.md).

### Reverse proxy deployment (HTTPS, port 443 only)

The frontend, the API and the SSE stream share port 8787. The binary listens on `127.0.0.1:8787` by
default, and the Docker container publishes on `127.0.0.1`. The activity stream is same-origin by
default, so **`NEXT_PUBLIC_SSE_BASE` is not necessary**. Expose port 443 and keep 8787 internal.

SSE keeps a long-lived connection with continuous pushes. The proxy **must disable buffering**.
Without that, the browser connects but receives no events.

Redirect plain HTTP first. A bookmarked `http://` URL then never reaches the app without
encryption:

```nginx
server {
    listen 80;
    server_name your.domain.com;
    return 301 https://$host$request_uri;
}
```

Then the TLS server:

```nginx
server {
    listen 443 ssl;
    server_name your.domain.com;
    # ssl_certificate / ssl_certificate_key ...

    location / {
        # 512 MiB combined file limit plus 1 MiB of multipart metadata.
        client_max_body_size 513m;
        proxy_request_buffering off;

        proxy_pass http://127.0.0.1:8787;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;

        # SSE essentials: buffering off, long timeouts, HTTP/1.1
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 3600s;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
    }
}
```

**Login rate limit behind a proxy.** Login allows 10 attempts per minute per client IP. Behind a
proxy, each request comes from the address of the proxy. Without more setup, all users share one
budget. An attacker who fails 10 logins blocks everybody, the admin included, for up to 15 minutes.
Keep the `X-Forwarded-For` line. Set `ARTEX_TRUSTED_PROXIES` (comma-separated CIDRs, empty by
default) to the address that ARTEX Program sees the proxy connect from:

- **nginx and ARTEX Program both on the host** (binary, `./start.sh`): the peer is `127.0.0.1`.
  Start with `ARTEX_TRUSTED_PROXIES=127.0.0.1/32`.
- **nginx on the host, ARTEX Program in Docker** (`docker-compose.yml`): the connection reaches the
  container from the Docker bridge gateway (a `172.x` address), not from `127.0.0.1`. Find the
  gateway with `docker network inspect artex_default --format '{{(index .IPAM.Config 0).Gateway}}'`.
  The network is `<project directory>_default`. Put `ARTEX_TRUSTED_PROXIES=<gateway>/32` in `.env`,
  then run `docker compose up -d`.

For a peer in that list, ARTEX Program reads `X-Forwarded-For` from the right and takes the first
address that is not a trusted proxy. From any other peer, the header is ignored. Trust only proxies
that you operate. A trusted peer can claim any client address.

**Security headers.** Each response carries `X-Content-Type-Options: nosniff`, a same-origin
referrer policy and a `frame-ancestors 'self'` CSP. Console documents also get a `Permissions-Policy`
and COOP/CORP. `Strict-Transport-Security` is sent only when the request is known to be HTTPS:
directly over TLS, or through `X-Forwarded-Proto: https` from a trusted proxy. The list above
enables it. With nginx in front, the 443 server sends HSTS after you set `ARTEX_TRUSTED_PROXIES`.

> Set `NEXT_PUBLIC_SSE_BASE` **at build time** only when SSE must come from a different origin than
> the page (for example a dedicated subdomain). `next build` bakes it into the static bundle. A
> value that you set at container run time has no effect.

---

## Operations

### Troubleshoot an empty tool call

In the session transcript of the task, expand the tool call and compare **Command** and **Output**.
The `UNTRUSTED_OUTPUT` markers show the boundary around the tool output. They are expected. Keep
the boundary on while you troubleshoot.

- **Command shows `{}`.** The recorded tool input is an empty JSON object. For a tool that needs a
  command, the command argument was missing. Check which model and model version made the call.
- **Command shows `(empty)` or `(failed to load)`.** The transcript has no input detail to show, or
  could not fetch it. If the session record contains a command, include that difference in a
  report.
- **Output shows `[]`.** This is the recorded result body. Check the success or failure indicator
  and the input of the row separately, before you conclude what ran.

For a problem that comes back, share these items:

- the version or commit of ARTEX Program;
- the model and the model version;
- a sanitized excerpt of the input and the result.

Remove credentials, target data and other sensitive content before you share.

### Backups

`scripts/backup.sh` dumps the PostgreSQL database, and optionally the data directory. The output has
a timestamp and a rotation:

```bash
scripts/backup.sh --with-data          # db dump + data/ archive, keep 14
scripts/backup.sh --keep 7             # db only, keep 7
```

Restore into a running stack:

```bash
gunzip -c backups/artex-db-<timestamp>.sql.gz | docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB"
```

Stored secrets are encrypted in the database. The key is `keys/secret.key`. ARTEX Program generates
it at first start, with mode 0600, next to `jwt.key`. The secrets are:

- LLM API keys, proxies and gateway custom headers;
- the MCP environment;
- task credentials;
- the configuration of the notification channels;
- the Brave and Tavily search keys;
- web search and global proxies;
- the execution spec of custom tools;
- the rewrite rules of the capture proxy.

**A database backup without that key cannot restore these secrets.** Back up `keys/` together with
the dump. On a deployment without a persistent disk, set `ARTEX_SECRET_KEY` (32 bytes, base64 or
hex, for example `openssl rand -base64 32`). ARTEX Program refuses to start when the database holds
encrypted secrets and no key is available.

Daily cron example (03:20, operator crontab):

```
20 3 * * * cd /path/to/ARTEX && scripts/backup.sh --with-data >> backups/backup.log 2>&1
```

### Update

> An upgrade replaces the program and never touches data. The PostgreSQL volume `pgdata`, `./data`
> (traffic index, task files, transcripts), `./keys` (`jwt.key`, `secret.key`) and `./skills` are
> preserved. **No manual database step is necessary.** At each start, ARTEX Program applies its
> embedded `schema.sql` baseline. It then applies each pending numbered migration from
> `db/migrations/`, in order and in its own transaction. A restart of the new binary is the upgrade.
> Back up `./data`, `keys/` and the database before you upgrade.

**Update banner (opt-in, off by default)**: start with `ARTEX_UPDATE_CHECK=1` to ask the public
GitHub releases API for the latest tag. The check runs at most once per 24 hours and is cached in
memory. A failed check is not retried in that window. When the release is newer, the web UI shows a
dismissible strip that links to the releases. The request sends only
`User-Agent: artex-update-check`. Without the variable, ARTEX Program makes no update request and
shows no banner.

#### Option 1: update script

```bash
cd artex-program
./update.sh
```

The script can `git pull` first. It then offers two modes: **① Docker update** (`docker compose
build artex`, then `docker compose up -d artex`) or **② local build update** (rebuild the frontend
static assets, recompile `./artex`, then restart the process).

#### Option 2: Docker

```bash
cd artex-program
git pull
DOCKER_DEFAULT_PLATFORM=linux/amd64 docker compose build artex
docker compose up -d artex
docker image prune -f            # optional
```

If you run the prebuilt container directly, pull the new tag (`docker pull
ghcr.io/guibibeau/artex-program:latest` or a pinned `:v<version>`). Then recreate the container with
the same `/app/data` and `/app/keys` mounts. If you run the prebuilt Compose stack:

```bash
docker compose -f docker-compose.ghcr.yml pull
docker compose -f docker-compose.ghcr.yml up -d
```

#### Option 3: prebuilt binaries

Download the new zip. Stop the old process. Overwrite `artex` and `skills/` (keep `config.json`
and `data/`). Then restart:

```bash
cp -r <extracted dir>/skills ./ && cp <extracted dir>/artex ./
./start.sh
```

#### Option 4: from source

```bash
git pull
cd web && npm ci && npm run build:static && cd ..
cp -r web/out server/webui/dist
CGO_ENABLED=0 go build -tags embedui -o artex ./cmd/artex
# restart ./start.sh
```

---

## Development

```bash
./dev.sh    # backend(:8787) + traffic proxy(:8788) + frontend next dev(:5173) → http://localhost:5173
```

- Backend: `go run ./cmd/artex`. Without `-tags embedui`, the frontend is not embedded.
- Frontend: `cd web && npm run dev`. `/api` is reverse-proxied to the backend, with hot reload.
- Tests: `go test ./...`.
- Reproduce the fast CI checks locally: `./scripts/verify.sh`. Add `--full` for `-race` and the
  static web build. Add `--security` for `govulncheck`.
- Security scan: install `govulncheck` and run `./scripts/verify.sh --security`. CI runs it on each
  push and pull request.
- Mock preview (no backend): `cd web && NEXT_PUBLIC_MOCK=1 npm run dev`.

The Solana toolchain that the agents call is not necessary to build and run the server. Install it
on the host where the agents work: the Solana CLI, the Anchor CLI, Surfpool, a Rust toolchain, and
Node 20 or later.

---

## Architecture

ARTEX Program is an **LLM multi-agent system for autonomous security testing of Solana programs**.
It has a Go backend (with the embedded Next.js UI) and PostgreSQL. The agent capabilities come from
the [`norma`](https://github.com/Autumn-27/norma) SDK (`agentcore`, `tool`, `permission`, `harness`,
`memory`, `transcript`). The core is the **dual-graph architecture**. Two autonomy mechanisms complete
it: **exchange of information between workers at process level**, and the **todolist that the
planner shares across rounds**.

```mermaid
flowchart TB
  subgraph FE["Frontend Next.js (go:embed embedded single binary)"]
    UI["Dashboard · Tasks · Assets · Coverage graph · Traffic · Workspace · System settings"]
  end
  subgraph SRV["server (Go net/http)"]
    API["REST /api/*  JWT auth  SSE"]
    ENG["engine scheduling loop"]
    MGR["Manager  task/engine/store lifecycle"]
  end
  subgraph AG["agent (norma SDK)"]
    GO["goals  goal decomposition + scope extraction"]
    PL["planner  the planner (sole intent generator)"]
    WK["worker  executor ×N"]
    MA["mainagent  human-in-the-loop"]
  end
  subgraph DB["PostgreSQL"]
    AGRAPH["Asset graph  assets / companies / task_scope"]
    EGRAPH["Exploration graph  exploration_nodes / anchors / activity"]
  end
  subgraph SUB["Supporting subsystems"]
    PROXY["Traffic recording proxy  MITM + CA tracing"]
    GUARD["guard / intercept  tool approval gate"]
    ENR["enrich  DNS / HTTP async enrichment"]
    EXT["MCP · skills · memory · report"]
  end

  UI -->|HTTP| API
  API --> MGR --> ENG
  ENG --> PL
  ENG --> WK
  API --> MA
  API --> GO
  PL --> DB
  WK --> DB
  MA --> DB
  GO --> DB
  WK -->|"Bash / HTTP fully traced"| PROXY
  WK --> GUARD
  WK --> ENR
  PL -.-> EXT
  WK -.-> EXT
  MA -.-> EXT
```

| Layer | Responsibility |
| --- | --- |
| **Frontend** | Next.js static export, embedded with `go:embed`. Tasks, assets, exploration and coverage graphs, traffic, human-in-the-loop chat |
| **server** | `net/http` routing, JWT auth, SSE. `Manager` owns the tasks, the engines and the DB stores |
| **engine** | One `plannerLoop` and N worker goroutines per task. Intent claiming, timeout, pause, drain |
| **agent** | goals, planner, worker, mainagent. `ToolSet` exposes the dual graphs as LLM tools |
| **db** | PostgreSQL persistence (pgx). The embedded schema and the numbered migrations are applied at startup |
| **Supporting** | Recording proxy, approval gate, async enrichment, MCP, skills, memory, report |

For the code-level map (execution flow, trust boundaries, decisions), see
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). For startup, configuration precedence, backup and key
rotation, see [docs/OPS.md](docs/OPS.md).

---

## Security

Report vulnerabilities in private. See [SECURITY.md](SECURITY.md).

Operational security properties:

- **Secrets at rest.** LLM keys, proxies, gateway headers, the MCP environment, task credentials,
  the notification configuration, search keys, proxies, the execution specs of custom tools, the
  rewrite rules of the capture proxy and the traffic evidence snapshots are encrypted with
  AES-256-GCM. The key is `keys/secret.key` (or `ARTEX_SECRET_KEY`). ARTEX Program refuses to start
  when encrypted data exists and no usable key is available.
- **First-run claim.** `/setup` needs a one-time token that the server prints at startup. The UI
  binds to `127.0.0.1` by default.
- **Login protection.** 10 attempts per minute per client IP. Use `ARTEX_TRUSTED_PROXIES` for the
  correct client attribution behind a reverse proxy.
- **Untrusted tool output.** Framed as data for each agent by default. This is the
  prompt-injection boundary.
- **Untrusted on-chain data.** Account data, program logs and RPC responses come from the chain.
  The prompts tell the agents to treat them as data.
- **Release integrity.** SHA-256 sums, a CycloneDX SBOM and GitHub build provenance attestations for
  the release archives.

---

## About this project

ARTEX Program is a fork of [ARTEX](https://github.com/BaaRai/ARTEX). ARTEX is the community
continuation of the original project by [Autumn-27](https://github.com/Autumn-27). The fork
re-orients the platform to Solana programs, with the same AGPL-3.0 license. The changes are in
[CHANGELOG.md](CHANGELOG.md) and in the section [Changes from ARTEX](#changes-from-artex).

---

## References and prior art

- [Cairn](https://github.com/oritera/Cairn)
- The approval-details UI is inspired by [AegisHook](https://github.com/RuoJi6/AegisHook)

---

## License and disclaimer

### Open source license

This project is licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**. See the
[LICENSE](LICENSE) file for the full terms.

Anyone can use, modify and distribute this project. **Derivative works must also be open source
under AGPL-3.0.** In particular, **if you modify this project and provide it to users over a
network (for example as an online service), you must make the corresponding complete source code
available to those users.**

> **Important**: the open source license does not restrict how you use the software. The usage
> restrictions and the disclaimer below are additional terms from the maintainers.

**ARTEX Program is a security testing tool. Use it only against systems that you own, or that you
are explicitly authorized to test: your own lab, the assets of your organization, or the targets of
a bug bounty or vulnerability disclosure program, within its published scope and rules of
engagement. Any use without authorization violates these terms.**

### Permitted use

- Read, learn and study the source code of this project.
- Run it against systems that you own, or in an isolated lab.
- Run it against authorized bug bounty or vulnerability disclosure targets, within their published
  scope and rules of engagement.
- Academic research and code review.

### Prohibited actions

- Scan, probe, exploit or attack any program, website, online service or networked system without
  the explicit authorization of its owner.
- Test outside the published scope of a program, or in violation of its rules of engagement.
- Illegal intrusion, data theft, extortion, denial of service, or any destructive or criminal
  activity.
- Any use that violates the laws and regulations of your country or region.

### Compliance responsibility

Users must comply with all laws and regulations of their country or region about cybersecurity,
data protection and computer crime. **The user bears all legal liability and consequences that come
from the use of this tool.**

### Disclaimer

This project is provided "as is", without any express or implied warranty. The authors and the
contributors are not liable for any direct or indirect loss, data loss, system damage or legal
dispute that the use of this tool causes, whether the use is proper or not. **When you download,
install or use this project, you confirm that you read, understood and agreed to all the terms
above.**
