# ARTEX Program operations

## 1. Startup

Docker (recommended):

```bash
cp .env.example .env          # set POSTGRES_PASSWORD; optionally an LLM key
docker compose up -d --build
docker compose logs artex | grep "Setup token"
```

On an arm64 host, set `DOCKER_DEFAULT_PLATFORM=linux/amd64` first. The image is `linux/amd64` only.

Open `http://localhost:8787`. The first visit to `/setup` asks for the one-time setup token that the
server prints at startup (`[auth] no admin password is set. Setup token for the /setup page: ...`
in `server/auth.go`).

From source, without the embedded UI:

```bash
go run ./cmd/artex                       # backend only, 127.0.0.1:8787
cd web && npm run dev                    # UI on :5173, /api proxied to the backend
# or: ./dev.sh                           # both, plus the traffic proxy on :8788
```

When the server does not run as root on Linux, the agent isolation is off. The server logs a banner
that says so. In that mode, agent processes run with the identity of the server. Use that mode only
on a host that holds no other secrets.

The process prints one `[config] snapshot {...}` line at startup. It is the authoritative record of
the effective configuration:

- the config path;
- the DSN source and the **redacted** DSN;
- the data, skill and key directories;
- the listen address and the proxy;
- the noise profile;
- the untrusted-output boundary;
- the trusted proxies.

No password or token value is ever logged there.

## 2. Configuration precedence

| Setting | Resolution order | Code |
| --- | --- | --- |
| Config file | `ARTEX_CONFIG` → `./config.json` → `config.json` next to the binary | `config.Path` |
| PostgreSQL DSN | `ARTEX_PG_DSN` → `ARTEX_PG_HOST` / `ARTEX_PG_PORT` / `ARTEX_PG_USER` / `ARTEX_PG_PASSWORD` / `ARTEX_PG_DBNAME` / `ARTEX_PG_SSLMODE` → `database.dsn` → assembled `host/port/user/password/dbname` | `config.PostgresDSN` |
| Skill directory | `ARTEX_SKILL_DIR` → `skill_dir` → `<binary dir>/skills` | `config.SkillDir` |
| JWT / secret key directory | `ARTEX_JWT_KEY_DIR` → `<binary dir>` (never the browsable data dir) | `config.KeyDir` |
| Secret encryption key | `ARTEX_SECRET_KEY` → `<key dir>/secret.key` → newly generated key file | `db.InitSecrets` |
| Noise profile | `ARTEX_NOISE_PROFILE` (`off` default; `quiet`, `balanced`, `aggressive`), also settable in UI | `egress.ProfileFromEnv` |
| Untrusted-output boundary | on by default; `ARTEX_UNTRUSTED_BOUNDARY=0\|false\|off\|no` disables it | `agent/untrusted_boundary.go` |
| Trusted proxies for login rate limit | `ARTEX_TRUSTED_PROXIES` (comma-separated IPs/CIDRs; empty trusts none) | `server.ParseTrustedProxies` |
| Target user agent | `ARTEX_TARGET_USER_AGENT` (applied by non-`off` noise profiles) | `egress.ProfileByName` |
| Budgets | `ARTEX_TASK_BUDGET_TOKENS` / `_TURNS`, `ARTEX_INTENT_BUDGET_TOKENS` / `_TURNS` | `agent/budget.go` |
| Live model judge test | `ARTEX_REVIEW_LIVE_CONFIG` (opt-in integration test only) | `server/intercept_live_test.go` |

Agent profiles, providers, notifications and the other runtime settings live in the database. Edit
them in **System → LLM / Settings / Notify**, not in `config.json`.

The LLM settings can also come from the environment at startup: `ARTEX_LLM_PROVIDER`,
`ARTEX_LLM_MODEL`, `ARTEX_LLM_BASE_URL`, `ARTEX_LLM_PROXY`, and `ANTHROPIC_API_KEY` or
`OPENAI_API_KEY`.

The token budgets count the full context of each request, including cached tokens. See
[Run budgets](README.md#configuration) before you size them.

## 3. Backup and restore

```bash
scripts/backup.sh                       # database dump only, 14 rotations
scripts/backup.sh --with-data --keep 30 # also archive the data/ bind mount
```

The dump runs inside the `postgres` compose service, so no host `psql` is necessary. Restore:

```bash
gunzip -c backups/artex-db-<ts>.sql.gz | docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB"
```

Back up the key directory too. Without it, JWTs cannot be verified, and sealed secrets cannot be
decrypted. Keep the key directory outside the database backup, but under the same access controls.

## 4. Key rotation

- **`jwt.key`** is the signing key for the 7-day HS256 session tokens. When you replace it, every
  existing session is invalid, and all users must log in again. Startup never overwrites an
  existing key file (`loadOrCreateJWTKey`).
- **`secret.key`** (or `ARTEX_SECRET_KEY`) is the AES-256-GCM key for API keys, the MCP
  environment, task credentials and captured auth headers. Startup never overwrites an existing
  key. When the key changes, values that were sealed under the old key cannot be decrypted. The
  code returns an error. It does not leak ciphertext as plaintext. Enter these secrets again in the
  UI, or keep the previous key available until the migration is complete.

Plan a short maintenance window for each rotation.

## 5. Diagnostics and local verification

- `GET /api/health` shows liveness, the build version and the provider health.
- `GET /api/logs` and `GET /api/logs/stream` are the live backend log sink. The UI shows it under
  **System → Logs**. The startup snapshot line is there too.
- `scripts/verify.sh` reproduces the CI checks locally: gofmt, build, vet, tests, the web type check
  and unit tests, shellcheck, and the python checks. `--full` adds `-race` and the web static
  build. `--go-only` and `--web-only` narrow the run.
- `./scripts/backup.sh --help` shows the backup options.

When PostgreSQL is not configured, the tests that need a database skip themselves. That is
expected. Set `ARTEX_PG_DSN` to run them. CI uses `go test -p 1 ./...`, because the test packages
share one PostgreSQL service and use suffixed per-package databases (`SuffixTestDatabase`).
