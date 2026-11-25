# HAR 1.2 import and export of traffic

ARTEX Program can import and export captured HTTP traffic in the **HAR 1.2** JSON format. You can
bring captures from browser DevTools or an external proxy into the traffic store, and export a
filtered subset.

Both routes need the standard administrator JWT (`Authorization: Bearer <token>`), like the rest of
`/api/traffic`. When the traffic store is not available, they return `503`.

---

## Import: `POST /api/traffic/import/har`

### Request format

Send a raw HAR 1.2 JSON document as the HTTP request body (`Content-Type: application/json`). The
standard API JSON body limit applies (**32 MiB**). A larger body gets `413 Request Entity Too Large`.

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @capture.har \
  http://127.0.0.1:8787/api/traffic/import/har
```

The traffic page also has an **Import HAR** file picker, with the same 32 MiB limit. It reads a
`.har` or `.json` file, posts it to this endpoint, shows the loading state, the errors and a count
summary, and refreshes the table.

### Validation and errors

The request gets `400 Bad Request` with the reason when:

- the body is empty or is not valid JSON (content after the root object included);
- the `log` object is missing;
- `log.version` is not `"1.2"`;
- `log.entries` is missing or `null`;
- `log.entries` has more than the entry cap per import (`2000` entries).

A malformed entry does **not** fail the whole import. These are examples:

- an invalid `startedDateTime`;
- a URL that is not HTTP or HTTPS;
- an invalid HTTP method;
- a header or cookie name or value with CR, LF or NUL;
- a status out of range;
- invalid base64 or an unsupported body encoding.

The import counts a malformed entry in `failed` and reports it in `errors` (up to 50 details). The
valid entries continue to import. HTTP/2 pseudo-headers that start with `:` (such as `:authority`
or `:method` from browser DevTools) are skipped.

### Response

```json
{
  "imported": 12,
  "skipped": 3,
  "failed": 1,
  "truncated": false,
  "errors": [
    { "index": 4, "url": "notaurl", "reason": "invalid request url \"notaurl\"" }
  ]
}
```

### Deterministic IDs, round-trip fidelity and idempotence

- **Exports of ARTEX Program (`_artexExchangeId`).** Each exported entry carries the HAR 1.2 custom
  field `_artexExchangeId` with the stored ID of the exchange. When an import contains a valid
  `_artexExchangeId` (`[A-Za-z0-9._:-]`, 1 to 128 characters), ARTEX Program preserves it. Two
  exchanges that were recorded in the same second with the same method, URL, status and bodies
  therefore stay two rows in a new store. A second import of an export into the source store is
  idempotent (`skipped`).
- **External HAR files (no `_artexExchangeId`).** Each entry gets a deterministic ID from its
  content:

```text
har-<hex(sha256(ts_unix_seconds + NUL + METHOD + NUL + canonical_url + NUL + status + NUL + sha256(req_body) + NUL + sha256(resp_body)))>
```

  `ts_unix_seconds` is `startedDateTime`, parsed as RFC 3339 and truncated to UTC Unix seconds
  (the same as `exchanges.ts`). A second import of the same external HAR skips the IDs that exist
  (`INSERT OR IGNORE`; the hot rows always win). Identical entries in one external HAR with the
  same `(ts_unix_seconds, method, canonical_url, status, req_body, resp_body)` are deduplicated:
  the first is imported, the duplicates are counted in `skipped`.

### Storage, bodies, cookies, `postData.params` and userinfo

- **Userinfo in the URL.** A `user:pass@` part in `request.url` is removed before the import
  stores, indexes, lists or exports it, or reports an error. The asset store of ARTEX Program does
  the same.
- `host` comes from `URL.Hostname()`. `url_template` comes from `db.TemplatePath(URL.EscapedPath())`.
  `path` stays empty.
- **Bodies and base64.** `response.content.text` and `request.postData.text` are stored as raw
  UTF-8 bytes when `encoding` is empty. They are decoded with standard base64 when `encoding` is
  `"base64"`. When `request.postData.text` is empty and `request.postData.params` has values,
  ARTEX Program rebuilds the form body as `application/x-www-form-urlencoded`, in the order of the
  parameters.
- **Cookie arrays.** When `request.headers` has no `Cookie` header and `request.cookies` is not
  empty, ARTEX Program builds a `Cookie` header from `request.cookies`. When `response.headers` has
  no `Set-Cookie` header and `response.cookies` is not empty, it builds `Set-Cookie` headers from
  `response.cookies`. Headers that are present stay as they are, without a duplicate.
- `req_len` and `resp_len` record the decoded byte lengths.
- `content_type` comes from `response.content.mimeType`. When that is missing, it comes from the
  `Content-Type` header of the response.
- A body above the inline threshold of 256 KiB spills to the content-addressed blob store
  (`_blobs/sha256/...`), with `blob_refs` rows. Searchable text bodies go to the `ex_fts`
  full-text index. This is the same as for proxy capture and `ImportArchive`.

### Capture rewrite rules are not applied

A HAR import writes directly to the traffic store. It does not pass through the MITM capture proxy.
**Capture rewrite rules are never applied to imported exchanges.** No `exchange_rewrites` rows are
created. Imported rows always have `rewritten: false`.

### Import limits

| Limit | Default | Behavior when exceeded |
|---|---|---|
| Raw HTTP request body | 32 MiB | `413 Request Entity Too Large` |
| Entries per HAR | 2,000 | `400 Bad Request` |
| Total decoded request and response body bytes | 32 MiB | The import of the remaining entries stops. The response has `"truncated": true`. The remaining entries are counted in `skipped`. An explanatory item is added to `errors`. |

---

## Export: `GET /api/traffic/export/har`

### Request and filters

The route returns an `application/json` HAR 1.2 document
(`Content-Disposition: attachment; filename="artex-traffic.har"`). The order is deterministic:
`ts` ascending, then `id` ascending.

The export uses the same filter parser and SQL predicate builder as `GET /api/traffic`. It supports
these filters: `host`, `method`, `path`, `status`, `q`, `body`, `rewritten` (`all`, `rewritten`,
`original`, `true`, `false`), `resp_min`, `resp_max`.

### Compliance with HAR 1.2 and placeholders for unknown values

- `log.version` is `"1.2"`. `log.creator` is `{"name": "ARTEX", "version": "<build-version>"}`.
- `startedDateTime` is formatted from `exchanges.ts` as UTC RFC 3339. `_artexExchangeId` carries the
  stored ID of the exchange.
- The request and response headers are parsed from the stored raw heads (`req_head` and
  `resp_head`). ARTEX Program does not invent a `serverIPAddress` or a header that is not in the
  store.
- A required HAR 1.2 field whose value is not recorded gets an explicit **placeholder for an
  unknown value**. The value is never fabricated:
  - `entry.time`: `0`
  - `entry.cache`: `{}`
  - `entry.timings`: `{"send": -1, "wait": -1, "receive": -1}` (unknown, as in HAR 1.2)
  - `request.cookies`, `request.queryString`, `response.cookies`: `[]`
  - `request.headersSize`, `response.headersSize`: `-1` (unknown, as in HAR 1.2)
  - `response.redirectURL`: `""`
- The bodies are read in full, from the inline columns or from the spilled blobs. A valid UTF-8 body
  without NUL is emitted as plain `text`. A binary body or a body that is not UTF-8 is emitted with
  `encoding: "base64"`.

### Export limits, compatibility with the import, and the report of a truncation

| Limit | Default | `truncatedReason` |
|---|---|---|
| Exported entries | 1,000 | `max_entries` |
| Total raw request and response headers and bodies | 20 MiB | `max_bytes` |
| Size of the marshaled JSON (final limit) | 31 MiB | `max_bytes` |

**Invariant of the limits.** Each HAR file from `GET /api/traffic/export/har` fits in the **32 MiB**
body limit of `POST /api/traffic/import/har`, and in the 32 MiB file limit of the traffic page. The
raw headers and bodies are capped at **20 MiB**. The worst-case expansion of base64 (`4/3`, about
26.7 MiB) plus the JSON structure then stays below 32 MiB. An explicit limit of **31 MiB** for the
marshaled JSON is the final guard. A payload with heavy JSON escaping then also cannot cause a
`413` on a re-import.

A truncation is never silent. Each export reports it in the HTTP response headers and in the custom
(`_`-prefixed) fields of the HAR `log` object:

- Headers:
  - `X-ARTEX-HAR-Truncated: true|false`
  - `X-ARTEX-HAR-Truncated-Reason: max_entries|max_bytes` (when truncated)
  - `X-ARTEX-HAR-Exported: <count>`
  - `X-ARTEX-HAR-Matched: <total matching rows>`
- JSON (the `log` object; omitted when not truncated):
  - `_truncated: true`
  - `_truncatedReason: "max_entries" | "max_bytes"`
  - `_totalMatched: <total matching rows>`
