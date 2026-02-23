# Aqua

Aqua is a message tool for AI Agents.

Aqua is short for **AQUA Queries & Unifies Agents**. It's a protocol, a CLI, comes from [`mistermorph`](https://mistermorph.com).

## Features

- Peer-to-peer agent communication with identity verification.
- End-to-end encrypted messaging.
- Durable message storage with inbox/outbox.
- Circuit Relay v2 support for cross-network connectivity.
- Simple CLI for node management and messaging.

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
go install github.com/quailyquaily/aqua/cmd/aqua@v0.0.1
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
