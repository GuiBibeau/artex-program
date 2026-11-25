# SOCKS5 UDP ASSOCIATE and raw UDP recording (opt-in)

ARTEX Program extends the opt-in SOCKS5 listener with **RFC 1928 `UDP ASSOCIATE` (`0x03`)**. The
support is **IPv4 first**. The listener enforces the policy for the destination of each datagram,
and records the forwarded datagrams separately from HTTP traffic in SQLite. Transparent UDP
(`IP_RECVORIGDSTADDR`) is not part of this feature.

## Configuration (off by default)

| Environment variable | Default | Behavior |
|---|---|---|
| `ARTEX_SOCKS5_UDP_ENABLED` | unset (off) | Set to `1`, `true`, `on` or `yes` to enable RFC 1928 `UDP ASSOCIATE` on the SOCKS5 listener. The listener then starts on `ARTEX_SOCKS5_ADDR` or `127.0.0.1:1080`, unless `ARTEX_SOCKS5_ENABLED` is explicitly `0`, `false`, `off` or `no`. |
| `ARTEX_SOCKS5_ENABLED` | unset (off) | An explicit `0`, `false`, `off` or `no` disables the whole SOCKS5 listener (TCP and UDP), also when `ARTEX_SOCKS5_UDP_ENABLED` is set. |
| `ARTEX_SOCKS5_ADDR` | `127.0.0.1:1080` | The bind address of the SOCKS5 TCP control listener. The IPv4 UDP relay socket of each association binds on the same local IPv4 interface, on an ephemeral port (`:0`). |

- **Flag off means no listener.**
  - With all SOCKS5 flags unset or off, no SOCKS5 listener and no UDP relay socket start.
  - With SOCKS5 TCP on and `ARTEX_SOCKS5_UDP_ENABLED` off, `UDP ASSOCIATE` (`0x03`) gets the RFC 1928
    reply `0x07` (*Command not supported*). No UDP socket opens.
- **The traffic recorder is necessary.** The server starts the SOCKS5 listener only when the
  traffic recorder is on (inside the traffic proxy block in `server/manager.go`).
- **Warning for an address that is not loopback.** The caveat of
  [docs/traffic-socks5.md](traffic-socks5.md) applies: the RFC 1929 username and password method
  accepts each credential. Keep the listener on loopback (`127.0.0.1`), or restrict the access in
  the firewall of the host.

## Protocol scope and lifetime

- **IPv4 first.**
  - `UDP ASSOCIATE` accepts IPv4 (`0x01`) and domain (`0x03`) requests, with `0.0.0.0:0` as in
    RFC 1928 §4. An IPv6 (`0x04`) `UDP ASSOCIATE` request gets `0x08` (*Address type not
    supported*). An IPv6 UDP datagram destination is dropped.
  - The listener parses the header of each client datagram on its own (`RSV=0x0000`, `FRAG=0x00`,
    `ATYP=0x01` for IPv4, or `ATYP=0x03` for a domain that resolves to IPv4).
- **No reassembly of fragments.**
  - As in RFC 1928 §7, a datagram with `FRAG != 0` is dropped. It is not reassembled.
  - Each forwarded datagram with `FRAG == 0` and each upstream reply datagram is recorded as it
    arrives. There is no reassembly across datagrams.
- **The source of the client is pinned.**
  - A client datagram on the ephemeral relay port is accepted only from the expected IPv4 client
    address: the `DST.ADDR` and `DST.PORT` of `UDP ASSOCIATE`, or the peer IP of the TCP control
    connection that was pinned on the first valid datagram.
  - An upstream reply datagram is accepted only from a destination (`ip:port`) to which the
    association forwarded an allowed datagram before.
- **The association is tied to the TCP control connection.**
  - When the SOCKS5 TCP control connection closes, both UDP sockets close at once, and the
    association ends.
  - `UDPIdleTimeout`, `UDPMaxDatagrams`, `UDPMaxBytes` and `Traffic.Close()` also close the
    association and its TCP control connection.

## Policy checks for the destination of each datagram

Before the listener forwards **each** client-to-server UDP datagram to `(host, port)`:

1. **Target gate** (`intercept.Interceptor.ResolveUDP4` and `intercept.PinnedUDP4Resolver`). It
   enforces the global asset block rules (domain, IP and CIDR, before and after a fresh IPv4 DNS
   resolution), and the pins and exemptions of an enabled `intercept.TargetScope`. It returns the
   verified IPv4 `*net.UDPAddr`.
2. **Egress governor** (`egress.Do(egress.Current(), ...)`). It enforces the active noise profile
   before the send (`PaceTimeout`, default `30s`).
3. **A failed check drops the datagram.** There is no silent passthrough. The listener drops a
   datagram in three cases:

   - the target gate denies the destination;
   - the DNS or IPv4 resolution fails;
   - the egress governor refuses the datagram.

   The listener sends no bytes upstream and records no forwarded datagram. The allowed datagrams
   that follow on the same association continue normally.

## Storage of raw UDP (SQLite)

Each forwarded datagram writes one row to the table `raw_udp_datagrams` in the SQLite traffic index
(`traffic/traffic.go`, `indexSchema`). There is no PostgreSQL migration.

- **Columns**: `id` (`udp-<ms>-<seq>`), `association_id` (`udpa-<ms>-<seq>`), `protocol` (`'udp'`),
  `host`, `port`, `direction` (`'c2s'` or `'s2c'`), `ts` (unix milliseconds), `size`, `preview`,
  `blob`.
- **No HTTP exchange row.** A raw UDP datagram never inserts into `exchanges`.
- **Preview and atomic blob spill.**
  - A payload up to `UDPPreviewBytes` (default `8 KiB`) stays inline in `preview`.
  - A payload above `UDPPreviewBytes` keeps the leading `UDPPreviewBytes` inline. The full payload
    of the datagram goes to `_blobs/sha256/` in one atomic step (a temporary file, then a rename).
    A reference is registered in `blob_refs` under the `udp-*` ID of the datagram.

## Operational limits

| Parameter | Default |
|---|---|
| `MaxConcurrent` | `64` active TCP streams and UDP associations combined |
| `UDPMaxDatagrams` | `1000` forwarded datagrams (`c2s` and `s2c`) per association |
| `UDPMaxBytes` | `4 MiB` forwarded payload (`c2s` and `s2c`) per association |
| `UDPPreviewBytes` | `8 KiB` inline preview per datagram before the blob spill |
| `UDPIdleTimeout` | `2m` timeout without activity per association |
| `HandshakeTimeout` | `10s` for the greeting, the authentication and the `UDP ASSOCIATE` request |
| `DialTimeout` | `30s` for the target gate and the DNS lookup of each datagram |
| `PaceTimeout` | `30s` maximum wait in the egress governor for each datagram |
