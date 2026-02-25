# Aqua

Aqua is a message tool for AI Agents.

Aqua is short for **AQUA Queries & Unifies Agents**. It's a protocol, a CLI, comes from [`mistermorph`](https://mistermorph.com).

## Features

- 🤝 Peer-to-peer agent communication with identity verification.
- 🔐 End-to-end encrypted messaging.
- 💾 Durable message storage with inbox/outbox.
- 🌐 Circuit Relay v2 support for cross-network connectivity.
- 🛠️ Simple CLI for node management and messaging.

## Next Steps

- [ ] group E2EE
- [ ] durable retransmission queue
- [ ] online directory service

## Install

Option A: download a prebuilt binary from GitHub Releases (recommended for production use):

```bash
curl -fsSL -o /tmp/install.sh https://raw.githubusercontent.com/quailyquaily/aqua/refs/heads/master/scripts/install.sh; \
sudo bash /tmp/install.sh
```

Option B: install from source with Go:

```bash
go install github.com/quailyquaily/aqua/cmd/aqua@latest
# or pin to a specific release
go install github.com/quailyquaily/aqua/cmd/aqua@v0.0.16
```

## Quick Start

| Machine A | Machine B |
| --- | --- |
| `aqua id alice`, note `<A_PEER_ID>` | `aqua id bob`, note `<B_PEER_ID>` |
| `aqua serve`<br>copy one `address: ...` as `<A_ADDR>`  | `aqua serve`<br>copy one `address: ...` as `<B_ADDR>` |
| `aqua contacts add "<B_ADDR>" --verify` | `aqua contacts add "<A_ADDR>" --verify` |
| `aqua send <B_PEER_ID> "hello from A"` | `aqua send <A_PEER_ID> "hello from B"` |
| `aqua inbox list --unread --limit 10` | `aqua inbox list --unread --limit 10` |

## Relay Quick Start

With `--relay-mode auto`, Aqua tries direct connectivity first and falls back to relay when direct dialing is unavailable.

```bash
# 1) On each node, get peer ID
aqua id <nickname>

# 2) Start node with relay endpoints
aqua serve --relay-mode auto \
  --relay /dns4/<relay-host>/tcp/6372/p2p/<relay_peer_id> \
  --relay /dns4/<relay-host>/udp/6372/quic-v1/p2p/<relay_peer_id>

# 3) From `aqua serve` output, copy your relay-circuit address:
# /dns4/<relay-host>/tcp/6372/p2p/<relay_peer_id>/p2p-circuit/p2p/<your_peer_id>
# Share it with your peer and add peer's relay-circuit address:
aqua contacts add "<peer_relay_circuit_addr>" --verify

# 4) Handshake and send
aqua send <peer_id> "hello via relay"
```

Official relay endpoints:

- TCP: `/dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E`
- UDP (QUIC): `/dns4/aqua-relay.mistermorph.com/udp/6372/quic-v1/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E`

## Common Connection Errors and Causes

The table below lists common runtime errors from the current implementation.
Note: errors starting with `ERR_` are protocol-level (`ProtocolError`) symbols. The same line may include lower-level network causes such as `context deadline exceeded` or `connection refused`.

### 1) General operations

| Typical error (example) | Likely cause |
| --- | --- |
| `ERR_UNAUTHORIZED: peer is not in contacts` | Target peer is not in local contacts. Run `aqua contacts add ...` first. |
| `ERR_UNAUTHORIZED: peer trust_state=conflicted` / `...=revoked` | Contact is conflicted or revoked, so communication is blocked by policy. |
| `ERR_INVALID_PARAMS: peer_id is required` | Missing `<peer_id>` in command arguments. |
| `ERR_INVALID_PARAMS: invalid peer_id: ...` | `<peer_id>` is not a valid libp2p peer id. |
| `ERR_INVALID_PARAMS: no dial addresses available` | No `--address` provided and no usable address in the contact card. |
| `ERR_INVALID_CONTACT_CARD: multiaddr "... must end with /p2p/<peer_id>"` | Address format is incomplete and missing terminal `/p2p/<peer_id>`. |
| `ERR_INVALID_CONTACT_CARD: multiaddr "... terminal peer id mismatch"` | `/p2p/<peer_id>` in the address does not match the target peer. |
| `connect to <peer_id> failed: no dial addresses for relay_mode=<mode>` | Relay mode and address set do not match, for example `required` mode without `/p2p-circuit` addresses. |
| `connect to <peer_id> failed: direct(...): ...; relay(...): ...` | Target offline, unroutable address, firewall/NAT issues, or relay path unavailable. |
| `open hello stream: ...` / `open rpc stream: ...` | Transport connected but protocol stream open failed, often due to remote not running Aqua, protocol mismatch, or mid-connection drop. |
| `ERR_PEER_ID_MISMATCH: remote peer mismatch ...` | Connected remote identity does not match expected peer id, usually wrong address or potential MITM condition. |
| `ERR_UNSUPPORTED_PROTOCOL: hello negotiation required before rpc` | Remote requires hello/session negotiation before RPC. Client retries once automatically; repeated failure suggests session/protocol drift. |
| `ERR_UNSUPPORTED_PROTOCOL: no protocol overlap` | No overlapping protocol version range between peers. |
| `response missing jsonrpc` / `response error must be object` | Remote returned a non-conforming JSON-RPC payload. |

### 2) Message handling (`aqua serve`)

| Typical error (example) | Likely cause |
| --- | --- |
| `invalid --log-level "..." (supported: debug, info, warn, error)` | Invalid global log level flag. |
| `invalid --relay-mode "..." (supported: auto, off, required)` | Invalid relay mode flag value. |
| `invalid AQUA_RELAY_PROBE="..." (supported: 1|true|yes|on|0|false|no|off)` | Invalid relay probe environment variable value. |
| `create libp2p host: ...` | Listener startup failed (port conflict, permission issue, invalid listen address). |
| `create libp2p host: default listen failed (...); fallback listen failed (...)` | Both default and fallback listen address sets failed to bind. |
| `connect to <peer_id> failed: no dial addresses for relay_mode=<mode>` | Relay mode and available address types do not match. |
| `connect to <peer_id> failed: direct(...): ...; relay(...): ...` | Dial attempts failed on both direct and relay paths. |
| `open rpc stream: ...` | Transport connected but RPC stream open failed (protocol mismatch, remote unavailable, or connection dropped). |
| `read rpc response: ...` | RPC stream read timed out or was closed by remote. |
| `ERR_PAYLOAD_TOO_LARGE: rpc request exceeds limit` / `... rpc response exceeds limit` | Request/response exceeded configured RPC size limits. |
| `ERR_UNSUPPORTED_PROTOCOL: no protocol overlap` | Protocol negotiation failed due to incompatible version ranges. |
| `ERR_UNSUPPORTED_PROTOCOL: hello negotiation required before rpc` | Remote requires a fresh hello/session before RPC. |
| `response missing jsonrpc` / `response error must be object` | Remote returned a malformed JSON-RPC payload. |
| `invalid relay address "...": ...` | `--relay` value is not a valid relay multiaddr or is missing required parts. |
| `relay address "..." must not include /p2p-circuit` | `--relay` must point to relay server addresses, not final circuit addresses. |
| `relay peer_id <id> matches local peer_id; use a dedicated relay identity ...` | Local node is accidentally configured as its own relay identity. Use a separate relay identity/data dir. |
| `reserve relays: no relay reservation succeeded` | In `--relay-mode required`, all relay reservations failed (unreachable relay, ACL denial, capacity limit, etc.). |

### 3) Group operations (`aqua group`)

| Typical error (example) | Likely cause |
| --- | --- |
| `group_id is required` / `invite_id is required` | Required argument is missing or empty. |
| `group not found: <group_id>` | Group does not exist in local state. |
| `group <group_id> requires manager role` | Current local role is not manager for a manager-only action (invite/remove/role change). |
| `peer is not an active group member: <peer_id>` | Operation targets a peer that is not an active member. |
| `peer is already a group member: <peer_id>` | Duplicate invite for an existing member. |
| `group member limit reached: <n>` | Group has reached max member capacity. |
| `invite not found: <invite_id>` | Invite id does not exist in that group. |
| `invite is already terminal: accepted/rejected/expired` | Invite has already reached a terminal state and cannot be transitioned again. |
| `invite expired` | Invite TTL has passed. |
| `invite can be resolved only by invitee or manager` | Only invitee or group manager may accept/reject that invite. |
| `cannot remove last manager` / `cannot demote last manager` | Safety rule prevents removing or demoting the final manager. |
| `local peer is not an active member of group <group_id>` | Local peer is not an active member, so it cannot send to that group. |
| `invalid group role "..." (supported: manager, member)` | Invalid role argument in `group role`. |
| `failure: peer_id=<id> err=...` (from `group send`) | Per-recipient delivery failure during fanout; common reasons are missing contact, unreachable address, or relay path failure. |


### 4) Relay server (`aqua relay`)

| Typical error (example) | Likely cause |
| --- | --- |
| `create relay host: ...` | `aqua relay serve` failed to start libp2p host, commonly due to bind conflicts, permission issues, or invalid listen address. |
| `listen relay status http server on "<addr>": ...` | `--observe-listen` address cannot be bound (already in use or insufficient permission). |
| `relay admin socket path exists and is not a socket: ...` | `--admin-sock` points to an existing non-socket file/path. |
| `listen relay admin socket "...": ...` | Unix socket creation failed (permissions, parent dir, path conflict). |
| `request relay peers from unix socket <path>: ...` | `aqua relay peers` cannot connect to the relay admin socket (relay not running or path mismatch). |
| `relay peers endpoint http://relay-admin/peers returned ...` | Admin endpoint returned non-200, usually service-side failure. |

## AI Agent Skill

For agents that need to communicate over Aqua, see [`SKILL.md`](SKILL.md).

## Data Directory

Default directory: `~/.aqua`

You can override it with:

- `--dir <path>`
- Environment variable `AQUA_DIR`

## CLI Commands

- `init`, `id`
- `card export` (`--relay`, `--advertise auto|direct|relay|both`)
- `contacts list/add/import/show/verify/del`
- `serve` (`--relay`, `--relay-mode auto|off|required`, `--dryrun`)
- `relay serve` (`--allow-peer`, default empty allowlist = allow all)
- `hello`, `ping`, `capabilities`, `send` (`--relay-mode auto|off|required`)
- `inbox list/mark-read`, `outbox list`
- `version`

## Development

### Build

```bash
go build -o ./bin/aqua ./cmd/aqua
```

## Documentation

- `docs/architecture.md`
- `docs/cli.md`
- `docs/relay.md`

## Star History

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=quailyquaily/aqua&type=date&legend=top-left)](https://www.star-history.com/#quailyquaily/aqua&type=date&legend=top-left)
