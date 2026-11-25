# Linux transparent TCP inbound and raw TCP recording (opt-in)

ARTEX Program has an opt-in **transparent IPv4 TCP listener for Linux only** in `traffic/`. It
accepts the connections that `iptables` or `nftables` `REDIRECT` rules send to it. It recovers the
original IPv4 destination of each socket with `getsockopt(SOL_IP, SO_ORIGINAL_DST)`. It enforces the
target gate and the egress governor before the dial. It records the connection in the SQLite table
`raw_tcp_streams`.

## Configuration (off by default)

| Environment variable | Default | Behavior |
|---|---|---|
| `ARTEX_TRANSPARENT_TCP_ENABLED` | unset (off) | Set to `1`, `true`, `on` or `yes` to start the transparent TCP listener on `127.0.0.1:1081` (IPv4 loopback). An explicit `0`, `false`, `off` or `no` forces it off, also when `ARTEX_TRANSPARENT_TCP_ADDR` is set. |
| `ARTEX_TRANSPARENT_TCP_ADDR` | `127.0.0.1:1081` | The IPv4 TCP bind address. A value that is not empty also enables the listener, unless `ARTEX_TRANSPARENT_TCP_ENABLED` is explicitly false. Use `0.0.0.0:1081` only when you redirect traffic from other interfaces (`PREROUTING`). Protect the port in the firewall of the host. |

- **Flag off**: no transparent TCP listener starts. No firewall rule changes. The HTTP MITM proxy
  is unchanged.
- **The traffic recorder is necessary.** The server starts the transparent TCP listener only when
  the traffic recorder is on (inside the traffic proxy block in `server/manager.go`). With traffic
  capture off, these environment variables do not open a listener.
- **Linux only.** On `windows` and `darwin`, a flag that is off does nothing. An explicit enable
  logs `ErrTransparentTCPNotLinux`, and the listener stays off.
- **IPv4 only.** The listener recovers `SO_ORIGINAL_DST` (`SOL_IP`, `AF_INET`) for IPv4 TCP sockets
  only. IPv6 (`IP6T_SO_ORIGINAL_DST`) is **not covered**. An IPv6 bind address and an IPv6 socket
  are refused.
- **ARTEX Program never changes the firewall.** It does not install, change or delete `iptables` or
  `nftables` rules. Root or `CAP_NET_ADMIN` is necessary **only** for the operator, to install and
  to remove the redirect rules of the host. It is never necessary to run ARTEX Program.

## Firewall rules and cleanup (managed by the operator)

Select **one** of `iptables` or `nftables`. Always exclude the UID that runs ARTEX Program
(example: `artex`) from the local `OUTPUT` redirect rules. The outbound dials of ARTEX Program then
do not loop back into the transparent listener.

### Option 1: `iptables` (IPv4, local `OUTPUT` REDIRECT)

Install the rules in a dedicated chain. The cleanup is then atomic:

```bash
# Requires root or CAP_NET_ADMIN
sudo iptables -t nat -N ARTEX_TRANSPARENT
# Never redirect the outbound dials of ARTEX Program or loopback traffic
sudo iptables -t nat -A ARTEX_TRANSPARENT -m owner --uid-owner artex -j RETURN
sudo iptables -t nat -A ARTEX_TRANSPARENT -d 127.0.0.0/8 -j RETURN
# Example: redirect outbound TCP to the lab target 198.51.100.0/24 into ARTEX Program on 127.0.0.1:1081
sudo iptables -t nat -A ARTEX_TRANSPARENT -p tcp -d 198.51.100.0/24 -j REDIRECT --to-ports 1081
sudo iptables -t nat -A OUTPUT -p tcp -j ARTEX_TRANSPARENT
```

Cleanup (exact removal):

```bash
sudo iptables -t nat -D OUTPUT -p tcp -j ARTEX_TRANSPARENT
sudo iptables -t nat -F ARTEX_TRANSPARENT
sudo iptables -t nat -X ARTEX_TRANSPARENT
```

For traffic that arrives on another interface (a container or a routed VM), bind
`ARTEX_TRANSPARENT_TCP_ADDR=0.0.0.0:1081`, attach `ARTEX_TRANSPARENT` to `PREROUTING` instead of
`OUTPUT`, and drop the direct packets to port 1081 that are not redirected in the firewall of the
host.

### Option 2: `nftables` (IPv4, local `OUTPUT` REDIRECT)

Install the rules in a dedicated table. The cleanup is one command:

```bash
# Requires root or CAP_NET_ADMIN
sudo nft -f - <<'NFT'
table ip artex_transparent {
  chain output {
    type nat hook output priority -100; policy accept;
    meta skuid "artex" return
    ip daddr 127.0.0.0/8 return
    ip daddr 198.51.100.0/24 tcp redirect to :1081
  }
}
NFT
```

Cleanup (exact removal):

```bash
sudo nft delete table ip artex_transparent
```

## Policy checks, loop guard and storage (shared with the SOCKS5 listener)

For each accepted TCP connection:

1. **Recovery of the original destination.** The listener reads `getsockopt(SOL_IP,
   SO_ORIGINAL_DST)` within `HandshakeTimeout` (default `10s`). It parses `struct sockaddr_in` and
   rejects a short buffer, a family other than `AF_INET` (IPv6 included), port `0`, and `0.0.0.0`.
   It rejects a **self-redirect** before the dial: a direct connection that is not redirected, where
   `SO_ORIGINAL_DST` equals the address and port of the listener.
2. **Egress governor** (`egress.Do(egress.Current(), ...)`). It enforces the active noise profile
   before the dial (`PaceTimeout`, default `30s`).
3. **Target gate** (`intercept.Interceptor.DialSOCKS5` and `intercept.PinnedDialer`). It enforces the
   global asset block rules, and the pins and exemptions of an enabled `intercept.TargetScope`,
   before an outbound socket opens. The transparent listener knows only the recovered destination
   IP, never a DNS host name. A block or allow rule on a host name therefore cannot match. The IP
   and CIDR rules apply. A refused connection closes at once, without a dial and without a row in
   the store.
4. **Relay and limits.** The listener reuses the full-duplex relay of the SOCKS5 listener, with the
   half-close (`CloseWrite`), and its defaults: `MaxConcurrent=64`, `MaxBytesPerDir=4 MiB`,
   `PreviewBytes=8 KiB`, `IdleTimeout=2m`, `HandshakeTimeout=10s`, `DialTimeout=30s`,
   `PaceTimeout=30s`.
5. **SQLite storage.** Each completed connection writes one row to `raw_tcp_streams`
   (`protocol='tcp'`), with an atomic spill to `_blobs/sha256/` (a temporary file, then a rename)
   and a reference in `blob_refs`. There are **no** HTTP `exchanges` rows and no PostgreSQL
   migration.

`Traffic.Close` stops new transparent connections, closes the active sockets, and waits for all
handler goroutines to exit. Then it closes SQLite.
