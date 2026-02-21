# Aqua

Aqua is a **standalone MAEP program** extracted from `mistermorph`.

- Module: `github.com/quailyquaily/aqua`
- Goal: allow MAEP (identity, contacts, handshake, RPC, `data.push`) to be built and run independently.

## Name Origin

`AQUA` is a recursive acronym: **AQUA Queries & Unifies Agents**.

- **Queries** highlights discovery, handshake, and request/response interactions between agents.
- **Unifies** highlights a single protocol and CLI workflow for otherwise fragmented agent capabilities.

## Features

- Standalone MAEP core package: `maep/`
- Standalone CLI: `aqua`
- Local file storage (JSON/JSONL + file locks)
- No dependency on `github.com/quailyquaily/mistermorph/...` packages

## Build

```bash
go build -o ./bin/aqua ./cmd/aqua
```

## Quick Start

```bash
# 1) Initialize identity
aqua init

# 2) Start node
aqua serve --listen /ip4/0.0.0.0/tcp/4001

# 3) Import peer card
aqua contacts import ./peer.card.json

# 4) Handshake and send
aqua hello <peer_id>
aqua push <peer_id> --topic chat.message --text "hello"
```

## Data Directory

Default directory: `~/.aqua/maep`

You can override it with:

- `--dir <path>`
- Environment variable `AQUA_MAEP_DIR`

## CLI Commands

- `init`, `id`
- `card export`
- `contacts list/import/show/verify`
- `serve`, `hello`, `ping`, `capabilities`, `push`
- `audit list`, `inbox list`, `outbox list`

## Documentation

- `docs/architecture.md`
- `docs/cli.md`
