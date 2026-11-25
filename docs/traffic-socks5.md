# SOCKS5 inbound proxy and raw TCP recording (opt-in)

ARTEX Program has an opt-in **RFC 1928 SOCKS5 `CONNECT`** listener in `traffic/`. It records raw TCP
connections separately from HTTP traffic.

## Configuration (off by default)

| Environment variable | Default | Behavior |
|---|---|---|
| `ARTEX_SOCKS5_ENABLED` | unset (off) | Set to `1`, `true`, `on` or `yes` to start the SOCKS5 listener on `127.0.0.1:1080` (loopback). An explicit `0`, `false`, `off` or `no` forces it off, also when `ARTEX_SOCKS5_ADDR` is set. |
| `ARTEX_SOCKS5_ADDR` | `127.0.0.1:1080` | The TCP bind address. A value that is not empty also enables the listener, unless `ARTEX_SOCKS5_ENABLED` is explicitly false. |

- **Flag off**: no SOCKS5 listener starts. The HTTP MITM recording proxy is byte-identical.
- **The traffic recorder is necessary.** The server starts the SOCKS5 listener only when the
  traffic recorder is on (inside the traffic proxy block in `server/manager.go`). With traffic
  capture off, `ARTEX_SOCKS5_ENABLED` and `ARTEX_SOCKS5_ADDR` do not open a listener.
- **The agents do not change.** Workers and tools keep `HTTP_PROXY`, `HTTPS_PROXY` and `ALL_PROXY`
  on the HTTP proxy. ARTEX Program does not switch the agents to SOCKS5.
- **Warning for a bind address that is not loopback.** The RFC 1929 username and password method
  (`0x02`) accepts each username and password. It does not verify credentials. The pace marker
  `artex-pace` uses the username for that reason. When `ARTEX_SOCKS5_ADDR` is bound to an address
  that is not loopback, the listener is an **unauthenticated proxy** for everybody who can reach
  the port. The target gate and the egress governor apply to each connection. Keep the listener on
  loopback (`127.0.0.1`), or restrict the access in the firewall of the host.

## Protocol scope

- **Commands.** `CONNECT` (`0x01`) is the default. `BIND` (`0x02`) and unknown commands get the
  RFC 1928 reply `0x07` (*Command not supported*). `UDP ASSOCIATE` (`0x03`) also gets `0x07`,
  unless `ARTEX_SOCKS5_UDP_ENABLED=1` is set. See [docs/traffic-socks5-udp.md](traffic-socks5-udp.md).
- **Address types.** IPv4 (`0x01`), domain name (`0x03`) and IPv6 (`0x04`). An unknown address type
  gets `0x08` (*Address type not supported*).
- **Authentication.**
  - A peer on loopback (`net.IP.IsLoopback()`) can connect with **No Auth** (`0x00`). A peer that
    is not on loopback and offers only `0x00` gets `0x05 0xFF`.
  - **Username and password** (`0x02`, RFC 1929) is accepted without a check of the credentials. A
    client can then pass the pace marker (`artex-pace`, `egress.PaceUser`) in the username.
- **Relay.** Full duplex. It is aware of a half-close (`CloseWrite` on EOF in each direction).

## Policy checks before the dial

Before the listener opens an outbound TCP socket, it runs the same controls as the HTTP path:

1. **Egress governor** (`egress.Do(egress.Current(), ...)`). It enforces the active noise profile
   (`off` is a passthrough without a delay). When the wait for a slot or a token is longer than
   `PaceTimeout`, the connection is refused before the dial, with the SOCKS5 reply `0x02`
   (*Connection not allowed by ruleset*).
2. **Target gate** (`intercept.Interceptor.DialSOCKS5` and `intercept.PinnedDialer`). It enforces
   the global asset block rules (before and after a fresh DNS resolution), and the pins and
   exemptions of an enabled `intercept.TargetScope`. It dials only verified addresses. A blocked
   target gets `0x02` before the dial. A failed DNS lookup gets `0x04` (*Host unreachable*).

## Storage of raw TCP (SQLite)

Each completed relayed connection writes one row to the table `raw_tcp_streams` in the SQLite
traffic index (`traffic/traffic.go`, `indexSchema`). There is no PostgreSQL migration.

- **Columns**: `id` (`tcp-<ms>-<seq>`), `protocol` (`'tcp'`), `host`, `port`, `started_at`,
  `ended_at`, `duration_ms`, `c2s_bytes`, `s2c_bytes`, `truncated`, `c2s_preview`, `c2s_blob`,
  `s2c_preview`, `s2c_blob`.
- **No HTTP exchange row.** A raw TCP stream never inserts into `exchanges`.
- **Preview and blob spill.**
  - A payload of a direction up to `PreviewBytes` (default `8 KiB`) stays inline in `c2s_preview`
    or `s2c_preview`.
  - A payload above `PreviewBytes` keeps the leading `PreviewBytes` inline. The captured bytes (up
    to `MaxBytesPerDir`) go to `_blobs/sha256/` in one atomic step (a temporary file, then a
    rename). A reference is registered in `blob_refs` under the `tcp-*` ID of the stream.
  - A payload above `MaxBytesPerDir` sets `truncated = 1`. The exact totals of `c2s_bytes` and
    `s2c_bytes` continue to count.

## Operational limits

| Parameter | Default |
|---|---|
| `MaxConcurrent` | `64` active streams. A connection above the limit gets `0x01`. |
| `MaxBytesPerDir` | `4 MiB` captured per direction |
| `PreviewBytes` | `8 KiB` inline preview per direction |
| `IdleTimeout` | `2m` read and write deadline per direction |
| `HandshakeTimeout` | `10s` for the greeting, the authentication and the `CONNECT` request |
| `DialTimeout` | `30s` timeout of the outbound TCP connect |
| `PaceTimeout` | `30s` maximum wait in the egress governor |

`Traffic.Close` stops new SOCKS5 connections, closes the active sockets, and waits for all handler
goroutines to exit. Then it closes SQLite.
