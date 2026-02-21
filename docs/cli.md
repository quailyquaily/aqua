# Aqua CLI Guide

## Quick Start

This section is for quickly experiencing Aqua on two computers (Machine A and Machine B).

### 1) Initialize identity on both machines

Machine A:

```bash
aqua init
aqua id "alice"
```

Machine B:

```bash
aqua init
aqua id "bob"
```

Assume:

- `<A_PEER_ID>` is Machine A peer ID
- `<B_PEER_ID>` is Machine B peer ID

### 2) Start node on both machines

Machine A:

```bash
aqua serve
```

Machine B:

```bash
aqua serve
```

From each `serve` output, copy one printed `address: ...` line:

- `<A_ADDR>` from Machine A (ends with `/p2p/<A_PEER_ID>`)
- `<B_ADDR>` from Machine B (ends with `/p2p/<B_PEER_ID>`)

### 3) Add each other as contacts (no card exchange)

Machine A:

```bash
aqua contacts add "<B_ADDR>" --verify
```

Machine B:

```bash
aqua contacts add "<A_ADDR>" --verify
```

### 4) Handshake and send a message

Machine A:

```bash
aqua hello <B_PEER_ID>
aqua push <B_PEER_ID> "hello from A"
```

You should see inbound event output on Machine B (`serve` process).

### 5) Inspect inbox/outbox

Machine B:

```bash
aqua inbox list --limit 10
```

Machine A:

```bash
aqua outbox list --limit 10
```

## Quick Start (With Relay, After Relay Support)

This section assumes relay-related flags are available in a future Aqua release.

### 1) Prepare identities on both machines

Machine A:

```bash
aqua init
aqua id "alice"
aqua id
```

Machine B:

```bash
aqua init
aqua id "bob"
aqua id
```

Assume:

- `<A_PEER_ID>` is Machine A peer ID
- `<B_PEER_ID>` is Machine B peer ID
- `<RELAY_ADDR>` is relay node multiaddr

### 2) Start both nodes with relay enabled

Machine A:

```bash
aqua serve \
  --relay "<RELAY_ADDR>" \
  --relay-mode auto
```

Machine B:

```bash
aqua serve \
  --relay "<RELAY_ADDR>" \
  --relay-mode auto
```

From each `serve` output, copy one printed `address: ...` line:

- `<A_ADDR>` from Machine A (ends with `/p2p/<A_PEER_ID>`)
- `<B_ADDR>` from Machine B (ends with `/p2p/<B_PEER_ID>`)

### 3) Add each other as contacts (relay-aware path)

Machine A:

```bash
aqua contacts add "<B_ADDR>" --verify
```

Machine B:

```bash
aqua contacts add "<A_ADDR>" --verify
```

### 4) Connect and send without explicit address

Machine A:

```bash
aqua hello <B_PEER_ID>
aqua push <B_PEER_ID> "hello with relay fallback"
```

Expected behavior:

- Aqua tries direct addresses first.
- If direct dial fails, Aqua retries relay addresses automatically.

## Detailed Parameters

### Global

- `--dir <path>`: Aqua state directory override.
- `AQUA_DIR`: used when `--dir` is not provided.

### Identity

#### `aqua init`

Usage:

```bash
aqua init [--json]
```

Flags:

- `--json`: print structured JSON output.

#### `aqua id`

Usage:

```bash
aqua id [nickname] [--json]
```

Flags:

- `--json`: print structured JSON output.

Behavior notes:

- With no `nickname` argument, show current identity info.
- With `nickname`, update local identity nickname, then print identity.

### Contact Cards

#### `aqua card export`

Usage:

```bash
aqua card export \
  [--address <multiaddr> ...] \
  [--listen <multiaddr> ...] \
  [--out <file>] \
  [--min-protocol <int>] \
  [--max-protocol <int>] \
  [--expires-in <duration>]
```

Flags:

- `--address` (repeatable): contact card dial addresses. Must end with `/p2p/<peer_id>`.
- `--listen` (repeatable): fallback source when `--address` is empty.
- `--out`: output file path. Default is stdout.
- `--min-protocol`: minimum supported protocol version. Default `1`.
- `--max-protocol`: maximum supported protocol version. Default `1`.
- `--expires-in`: relative expiration like `720h`. `0` means no expiry.

Behavior notes:

- If `--address` is present, it is used directly.
- If `--address` is empty, addresses are derived from `--listen`.
- In interactive terminals, if multiple valid derived addresses exist, Aqua prompts for selection.
- In non-interactive mode with ambiguous addresses, Aqua returns an error.

### Contacts

#### `aqua contacts list`

Usage:

```bash
aqua contacts list [--json]
```

Flags:

- `--json`: print structured JSON output.

Behavior notes:

- Text output uses `display_name` first; if empty, it falls back to remote-reported `nickname`.

#### `aqua contacts import`

Usage:

```bash
aqua contacts import <contact_card.json|-> [--display-name <name>]
```

Flags:

- `--display-name`: optional local alias for the contact.

#### `aqua contacts add`

Usage:

```bash
aqua contacts add <address> [--display-name <name>] [--verify] [--json]
```

Flags:

- `--display-name`: optional local alias for the contact.
- `--verify`: mark imported contact as verified immediately.
- `--json`: print structured JSON output.

Behavior notes:

- `<address>` must end with `/p2p/<peer_id>`.
- Requires peer support for RPC method `agent.card.get`.
- Does not require prior contact import for the target peer.
- Without `--verify`, imported contact starts as unverified trust state (`tofu` by default).
- Remote card `nickname` is imported into contact metadata.

#### `aqua contacts del`

Usage:

```bash
aqua contacts del <peer_id>
```

#### `aqua contacts show`

Usage:

```bash
aqua contacts show <peer_id> [--json]
```

Flags:

- `--json`: print structured JSON output.

Behavior notes:

- Output includes both local `display_name` and remote-reported `nickname`.

#### `aqua contacts verify`

Usage:

```bash
aqua contacts verify <peer_id>
```

### Node Runtime

#### `aqua serve`

Usage:

```bash
aqua serve [--listen <multiaddr> ...] [--json]
```

Flags:

- `--listen` (repeatable): listen multiaddrs.
  - default preferred: `/ip4/0.0.0.0/udp/6371/quic-v1`
  - default preferred: `/ip4/0.0.0.0/tcp/6371`
  - fallback on bind failure: random ports (`/udp/0` and `/tcp/0`)
- `--json`: print ready/event output as JSON.

Behavior notes:

- For wildcard listen addresses (`0.0.0.0` or `::`), Aqua auto-expands printed addresses with detected local interface IPs (for example Tailscale).

### Dialing Commands

`hello`, `ping`, and `capabilities` share the same address behavior:

- `--address` is optional and repeatable.
- If omitted, Aqua uses addresses from the contact card for `<peer_id>`.

#### `aqua hello`

Usage:

```bash
aqua hello <peer_id> [--address <multiaddr> ...] [--json]
```

#### `aqua ping`

Usage:

```bash
aqua ping <peer_id> [--address <multiaddr> ...] [--json]
```

#### `aqua capabilities`

Usage:

```bash
aqua capabilities <peer_id> [--address <multiaddr> ...] [--json]
```

### Push

#### `aqua push`

Usage:

```bash
aqua push <peer_id> \
  [message] \
  [--address <multiaddr> ...] \
  [--topic <topic>] \
  [--message <message>] \
  [--content-type <type>] \
  [--idempotency-key <key>] \
  [--session-id <uuid_v7>] \
  [--reply-to <message_id>] \
  [--notify] \
  [--json]
```

Flags:

- `--address` (repeatable): optional dial override.
- `--topic`: message topic. Default `chat.message`.
- `[message]`: positional message payload.
- `--message`: optional message payload flag (use this or positional argument).
- `--content-type`: default `application/json`.
- `--idempotency-key`: default derived from generated message ID.
- `--session-id`: optional UUIDv7. If omitted, Aqua auto-generates one.
- `--reply-to`: reply target message ID.
- `--notify`: send as JSON-RPC notification (no response expected).
- `--json`: print structured JSON output.

Behavior notes:

- If `--session-id` is provided, it must be UUIDv7.
- Message can be provided either as positional argument or via `--message` (not both).
- Dialogue topics (for example `dm.reply.v1`) require a valid session ID; omitted session ID is auto-generated.
- For `application/json`, Aqua wraps text into JSON payload fields (`message_id`, `text`, `sent_at`).

### Audit and Mailboxes

#### `aqua audit list`

Usage:

```bash
aqua audit list [--peer-id <peer_id>] [--action <symbol>] [--limit <n>] [--json]
```

Flags:

- `--peer-id`: filter by peer ID.
- `--action`: filter by audit action symbol.
- `--limit`: max records. `<= 0` means all. Default `100`.
- `--json`: print structured JSON output.

#### `aqua inbox list`

Usage:

```bash
aqua inbox list [--from-peer-id <peer_id>] [--topic <topic>] [--limit <n>] [--json]
```

Flags:

- `--from-peer-id`: filter by sender peer ID.
- `--topic`: filter by topic.
- `--limit`: max records. `<= 0` means all. Default `50`.
- `--json`: print structured JSON output.

#### `aqua outbox list`

Usage:

```bash
aqua outbox list [--to-peer-id <peer_id>] [--topic <topic>] [--limit <n>] [--json]
```

Flags:

- `--to-peer-id`: filter by destination peer ID.
- `--topic`: filter by topic.
- `--limit`: max records. `<= 0` means all. Default `50`.
- `--json`: print structured JSON output.
