# Aqua

Aqua is **AQUA Queries & Unifies Agents**. It's a protocol, a cli, comes from [`mistermorph`](https://mistermorph.com).

## Features

- Peer-to-peer agent communication with identity verification.
- End-to-end encrypted messaging.
- Durable message storage with inbox/outbox/audit.
- Relay support for NAT traversal and connectivity (WIP).
- Simple CLI for node management and messaging, designed for agent operators (SKILL.md included).

## Install

```bash
go install github.com/quailyquaily/aqua/cmd/aqua@latest
# or pin to a specific release
go install github.com/quailyquaily/aqua/cmd/aqua@v0.0.1
```

Make sure your `$GOBIN` (or `$GOPATH/bin`) is in `PATH`.

## Build

```bash
go build -o ./bin/aqua ./cmd/aqua
```

## Quick Start

```bash
# 1) Initialize identity
aqua init

# 2) Start node
aqua serve

# 3) Add peer contact directly (no card file exchange)
aqua contacts add /ip4/1.2.3.4/tcp/6371/p2p/<peer_id> --verify

# 4) Handshake and send
aqua hello <peer_id>
aqua push <peer_id> "hello"
```

## Data Directory

Default directory: `~/.aqua/aqua`

You can override it with:

- `--dir <path>`
- Environment variable `AQUA_DIR`

## CLI Commands

- `init`, `id`
- `card export`
- `contacts list/add/import/show/verify/del`
- `serve`, `hello`, `ping`, `capabilities`, `push`
- `audit list`, `inbox list`, `outbox list`

## Documentation

- `docs/architecture.md`
- `docs/cli.md`
