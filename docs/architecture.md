# Aqua Architecture

`aqua` is a standalone Aqua program extracted from `mistermorph`. Module name:

- `github.com/quailyquaily/aqua`

The goal is to make Aqua independently buildable, runnable, and releasable without depending on `mistermorph` packages.

## Directory Layout

- `cmd/aqua/main.go`
  - Standalone CLI entrypoint with common commands for identity/contact/card/node/RPC.
- `aqua/`
  - Aqua core domain model and protocol implementation (identity, contact card, hello/RPC/data.push, service, store).
- `internal/fsstore/`
  - File storage foundations (atomic writes, file locks, JSON/JSONL read/write, index helpers).

## Runtime Modes

- `aqua serve`
  - Starts a libp2p node and handles `hello` and JSON-RPC.
  - Writes inbound `agent.data.push` messages to local inbox and can emit events to stdout.
- `aqua hello/ping/capabilities/push`
  - Acts as a dialing client to connect to peer nodes.

## Data Persistence

Default directory: `~/.aqua` (overridable via `--dir` or `AQUA_DIR`).

Core files:

- `identity.json`: local identity (including peer_id and key material)
- `contacts.json`: contact registry
- `inbox_messages.jsonl`: received messages
- `inbox_read_state.json`: read/unread state for inbox messages
- `outbox_messages.jsonl`: sent messages
- `dedupe_records.json`: idempotency/deduplication records
- `protocol_history.json`: protocol negotiation history
- `.fslocks/`: file lock directory

## Dependency Boundary

Aqua depends only on public third-party libraries (libp2p, cobra, uuid, etc.) and its own packages, and no longer imports `github.com/quailyquaily/mistermorph/...`.
