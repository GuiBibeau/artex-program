# LLM context size telemetry

ARTEX Program measures the size of the context of each LLM call. The telemetry covers each `llmrec`
`Stream` and `Complete` call. It is **on by default**. `ARTEX_CONTEXT_TELEMETRY=0` (or `false`, `off`
or `no`) disables it. No sizing runs then, and no row is written to PostgreSQL.

The telemetry changes no prompt, no compaction policy and no model behavior.

## Configuration

| Environment variable | Values | Default |
|---|---|---|
| `ARTEX_CONTEXT_TELEMETRY` | `0`, `false`, `off`, `no` disable it (case-insensitive). Any other value, or no value, keeps it on. | on |

The telemetry is independent of `llm_record` (`recordBodies`). It stays on when the recording of the
full request and response bodies is off. It stores **no prompt, tool or message content**. It stores
only byte lengths, counts, token usage and metadata.

### Stop, retention and write cost

With the telemetry on, each LLM call does one small `INSERT` into `llm_context` (about 200 bytes per
row, plus index entries, without bodies). The table grows by one row per call. When a measurement
campaign is complete, set `ARTEX_CONTEXT_TELEMETRY=0` and restart. That stops all writes and all
sizing work. You can also remove old rows on a schedule:

```sql
DELETE FROM llm_context WHERE ts < now() - interval '30 days';
```

---

## What is measured (`llmrec.MeasureContext`)

Each enabled `Stream` and `Complete` call measures the assembled `llm.CompletionRequest` before it
goes to the provider. On completion or on error, it writes one row to `llm_context` (migration
`0009_llm_context.sql`).

- **`system_static_bytes` and `system_dynamic_bytes`.**
  - `CompletionRequest.System` of Norma is a slice of segments. `llm.joinSystem` trims each segment
    (`strings.TrimSpace`), drops a segment that is empty, and joins the segments with blank lines
    (`"\n\n"`). `MeasureContext` applies the same three rules to the slice.
  - When `DynamicBoundary > 0`, `System` is split at `b = min(DynamicBoundary, len(System))`.
    `System[:b]` is measured into `system_static_bytes`. `System[b:]` is measured into
    `system_dynamic_bytes`.
  - When `DynamicBoundary <= 0`, there is no cache split that the provider can see. The whole
    `System` slice is then measured as one block in `system_static_bytes`, and
    `system_dynamic_bytes` is `0`.
  - **Reason for this choice.** `agent/deferred.go` returns `boundary = 0` when no global
    deferred-MCP block exists, and `boundary = len(system)` when one exists. The single unsplit
    block goes into `system_static_bytes`. `system_static_bytes + system_dynamic_bytes` then always
    equals the total of the system bytes. `system_dynamic_bytes > 0` then means exactly one thing: a
    dynamic suffix is placed after a cache breakpoint.
- **`tool_count` and `tools_bytes`.** `len(req.Tools)` and `len(json.Marshal(req.Tools))` (`0`
  without tools). The `encoding/json` of Go sorts map keys deterministically.
- **`message_count`, `messages_user_bytes`, `messages_assistant_bytes`, `messages_other_bytes`,
  `last_message_bytes`.** Each `llm.Message` is measured as `len(json.Marshal(msg))` and accumulated
  by role (`user`, `assistant` or other). `last_message_bytes` is the serialized size of the final
  message.
- **`tool_result_bytes`.** The serialized byte length of the `tool_result` content blocks in the
  messages. It is an **informational subset** of `messages_user_bytes`. It is not added to the
  denominator of the partition, so nothing is counted twice.

### Attribution of tokens by byte proportion

ARTEX Program ships no tokenizer per provider. The share of each section in the real `input_tokens`
is therefore computed per call, in proportion to its bytes:

```text
total_bytes(call) = system_static_bytes + system_dynamic_bytes + tools_bytes
                  + messages_user_bytes + messages_assistant_bytes + messages_other_bytes

share(section) = SUM(input_tokens * (section_bytes / total_bytes))
               / SUM(input_tokens)          -- over calls with total_bytes > 0 and input_tokens > 0
```

The six shares of the partition add up to `1.0`.
`cache_read_ratio = SUM(cache_read) / NULLIF(SUM(input_tokens), 0)`. In ARTEX Program and Norma,
`input_tokens` already includes the cached tokens.

---

## Query and aggregation

### HTTP endpoint (administrator JWT necessary)

```text
GET /api/llm/context/summary?days=30&top=20&task=<optional-task-id>
```

The endpoint returns `enabled` and `by_worker_model`: the calls, the totals, the `cache_read_ratio`,
`input_p50`, `p90`, `p99` and `max`, the average size of the tools, the messages and the last
message, and the `shares` by byte proportion. It also returns `top_tasks`, ordered by the total
`input_tokens`. There is no UI for this endpoint.

### SQL query for `psql`

```sql
WITH base AS (
    SELECT *,
           (system_static_bytes + system_dynamic_bytes + tools_bytes
            + messages_user_bytes + messages_assistant_bytes + messages_other_bytes) AS total_bytes
    FROM llm_context
    WHERE ts >= now() - interval '30 days'
)
SELECT COALESCE(NULLIF(worker,''), '(unknown)') AS worker,
       COALESCE(NULLIF(model,''), '(unknown)')  AS model,
       COUNT(*)                                  AS calls,
       SUM(input_tokens)                         AS input_tokens,
       ROUND((SUM(cache_read)::numeric / NULLIF(SUM(input_tokens), 0))::numeric, 3) AS cache_read_ratio,
       PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY input_tokens) AS p50_input,
       PERCENTILE_CONT(0.90) WITHIN GROUP (ORDER BY input_tokens) AS p90_input,
       PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY input_tokens) AS p99_input,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (system_static_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_sys_static,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (system_dynamic_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_sys_dynamic,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (tools_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_tools,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (messages_user_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_user,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (tool_result_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_tool_result_subset,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (messages_assistant_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_assistant
FROM base
GROUP BY 1, 2
ORDER BY input_tokens DESC;
```

---

## Cache prefix split for workers (`ARTEX_CACHE_PREFIX=1`)

`ARTEX_CACHE_PREFIX=1` (it accepts `1`, `true`, `on` and `yes`, case-insensitive; **off by
default**) enables a split of the system prompt for the **`worker`** lane only. The split does not
reorder any text. Workers that are siblings in the same task then share a static prefix that the
provider can cache. The text that the model sees does not change.

- **Static segment** (`System[0]`, before `DynamicBoundary = 1`):
  `body + untrustedContentTail + workerTrafficBlock`.
- **Dynamic segment** (`System[1]`, after `DynamicBoundary = 1`):
  `workerArtifactSpec(runDir) + noiseProfileBlock + lessons + constraints + renderIntentTask(intent) + target assets + launch instruction + FindingGuidance + <available-deferred-tools>`,
  in the original order. The leading `"\n\n"` of `workerArtifactSpec` is absorbed, because the
  provider joins the segments with `"\n\n"`. `strings.Join(System, "\n\n")` stays byte-identical
  to the unsplit prompt of the earlier layout.
- **Without the flag.** When `ARTEX_CACHE_PREFIX` is unset or false, the segments of the worker and
  `DynamicBoundary` are byte-identical to the earlier layout. `planner`, `mainagent`, `chat` and
  `goals` do not change, with or without the flag.
- **Limit for custom prompts.** The split covers only the built-in suffix that starts at
  `workerArtifactSpec(runDir)`. When an operator changes the `worker` prompt in the database and
  adds `{.Now}` (a resolution of one second), that timestamp stays in `System[0]`. The static
  prefix then changes again for each new worker.
- **Measure the effect on real tasks.** Set `ARTEX_CONTEXT_TELEMETRY=1` and `ARTEX_CACHE_PREFIX=1`.
  Compare the `system_static` and `system_dynamic` shares and the `cache_read_ratio` through
  `GET /api/llm/context/summary`.
