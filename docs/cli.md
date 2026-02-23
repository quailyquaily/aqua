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
aqua send <B_PEER_ID> "hello from A"
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

## Quick Start (With Relay)

This section shows a 3-node setup: Relay R, Machine A, and Machine B.

### 1) Start a relay node (Machine R)

```bash
aqua init
aqua relay serve \
  --listen /ip4/0.0.0.0/tcp/6372 \
  --listen /ip4/0.0.0.0/udp/6372/quic-v1
```

From relay output, copy one printed `address: ...` line:

- `<RELAY_ADDR>` ends with `/p2p/<RELAY_PEER_ID>`

### 2) Prepare identities on both edge machines

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

### 3) Start both edge nodes with relay enabled

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

From each `serve` output, copy one printed `address: ...` line (prefer relay-circuit addresses when present):

- `<A_ADDR>` from Machine A (ends with `/p2p/<A_PEER_ID>`)
- `<B_ADDR>` from Machine B (ends with `/p2p/<B_PEER_ID>`)

### 4) Add each other as contacts (relay-aware path)

Machine A:

```bash
aqua contacts add "<B_ADDR>" --verify
```

Machine B:

```bash
aqua contacts add "<A_ADDR>" --verify
```

### 5) Connect and send without explicit address

Machine A:

```bash
aqua hello <B_PEER_ID>
aqua send <B_PEER_ID> "hello with relay fallback"
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
- If identity is not initialized, `aqua id` auto-initializes it before showing/updating.

### Contact Cards

#### `aqua card export`

Usage:

```bash
aqua card export \
  [--address <multiaddr> ...] \
  [--listen <multiaddr> ...] \
  [--relay <multiaddr> ...] \
  [--advertise auto|direct|relay|both] \
  [--out <file>] \
  [--min-protocol <int>] \
  [--max-protocol <int>] \
  [--expires-in <duration>]
```

Flags:

- `--address` (repeatable): contact card dial addresses. Must end with `/p2p/<peer_id>`.
- `--listen` (repeatable): fallback source when `--address` is empty.
- `--relay` (repeatable): relay endpoint addresses (`.../p2p/<relay_peer_id>`) used to construct relay-circuit advertise addresses.
- `--advertise`: address publish strategy. Default `auto`.
- `--out`: output file path. Default is stdout.
- `--min-protocol`: minimum supported protocol version. Default `1`.
- `--max-protocol`: maximum supported protocol version. Default `1`.
- `--expires-in`: relative expiration like `720h`. `0` means no expiry.

Behavior notes:

- If `--address` is present, it is used directly.
- If `--address` is empty, addresses are derived from `--listen`.
- `--advertise=direct`: publish direct addresses only.
- `--advertise=relay`: publish relay-circuit addresses only.
- `--advertise=both`: publish direct and relay addresses.
- `--advertise=auto`: publish direct only when relay addresses are absent, otherwise publish both.
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
aqua serve \
  [--listen <multiaddr> ...] \
  [--relay <multiaddr> ...] \
  [--relay-mode auto|off|required] \
  [--dryrun] \
  [--json]
```

Flags:

- `--listen` (repeatable): listen multiaddrs.
  - default preferred: `/ip4/0.0.0.0/udp/6372/quic-v1`
  - default preferred: `/ip4/0.0.0.0/tcp/6372`
  - fallback on bind failure: random ports (`/udp/0`, `/tcp/0`, `/tcp/0/ws`)
- `--relay` (repeatable): relay endpoints to reserve.
- `--relay-mode`: `auto|off|required` (default `auto`).
- `--dryrun`: print derived advertise addresses and exit without starting listeners.
- `--json`: print ready/event output as JSON.

Behavior notes:

- For wildcard listen addresses (`0.0.0.0` or `::`), Aqua auto-expands printed addresses with detected local interface IPs (for example Tailscale).
- With configured `--relay`, Aqua attempts Circuit Relay v2 reservation at startup.
- `--json` includes relay events like `relay.reservation.ok`, `relay.path.selected`, and `relay.fallback`.
- With `--dryrun`, Aqua does not create libp2p listeners and prints address planning output only (useful for CLI/programmatic address discovery).

#### `aqua relay serve`

Usage:

```bash
aqua relay serve [--listen <multiaddr> ...] [--allow-peer <peer_id> ...] [--json]
```

Flags:

- `--listen` (repeatable): relay service listen addresses. Defaults `/ip4/0.0.0.0/tcp/6372` and `/ip4/0.0.0.0/udp/6372/quic-v1`.
- `--allow-peer` (repeatable): peer allowlist. Default empty means allow all peers.
- `--json`: print ready output as JSON.

Behavior notes:

- Uses libp2p Circuit Relay v2 service mode.
- When allowlist is set, both source and destination peers must be allowlisted for relayed connect.

### Dialing Commands

`hello`, `ping`, and `capabilities` share the same address behavior:

- `--address` is optional and repeatable.
- If omitted, Aqua uses addresses from the contact card for `<peer_id>`.
- `--relay-mode` controls direct/relay preference (`auto|off|required`, default `auto`).

#### `aqua hello`

Usage:

```bash
aqua hello <peer_id> [--address <multiaddr> ...] [--relay-mode auto|off|required] [--json]
```

#### `aqua ping`

Usage:

```bash
aqua ping <peer_id> [--address <multiaddr> ...] [--relay-mode auto|off|required] [--json]
```

#### `aqua capabilities`

Usage:

```bash
aqua capabilities <peer_id> [--address <multiaddr> ...] [--relay-mode auto|off|required] [--json]
```

### Send

#### `aqua send`

Usage:

```bash
aqua send <peer_id> \
  [message] \
  [--address <multiaddr> ...] \
  [--relay-mode auto|off|required] \
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
- `--content-type`: default `text/plain`.
- `--idempotency-key`: default derived from generated message ID.
- `--session-id`: optional UUIDv7. If omitted, Aqua auto-generates one.
- `--reply-to`: reply target message ID.
- `--notify`: send as JSON-RPC notification (no response expected).
- `--json`: print structured JSON output.

Behavior notes:

- If `--session-id` is provided, it must be UUIDv7.
- Message can be provided either as positional argument or via `--message` (not both).
- Dialogue topic (`chat.message`) requires a valid session ID; omitted session ID is auto-generated.
- Aqua sends `--message` content as payload bytes directly (no extra JSON envelope).

Topic list:

- Dialogue topic (session required): `chat.message`.
- Non-dialogue topics: any non-empty custom topic string (session optional).

### Mailboxes

#### `aqua inbox list`

Usage:

```bash
aqua inbox list [--from-peer-id <peer_id>] [--topic <topic>] [--limit <n>] [--unread] [--json]
```

Flags:

- `--from-peer-id`: filter by sender peer ID.
- `--topic`: filter by topic.
- `--limit`: max records. `<= 0` means all. Default `50`.
- `--unread`: list unread messages only.
- `--json`: print structured JSON output.

Behavior notes:

- `--unread` automatically marks the listed messages as read after listing.

#### `aqua inbox mark-read`

Usage:

```bash
aqua inbox mark-read <message_id>... [--json]
aqua inbox mark-read --all [--from-peer-id <peer_id>] [--topic <topic>] [--json]
```

Flags:

- `--all`: mark all matching unread messages as read.
- `--from-peer-id`: filter by sender peer ID (used with `--all`).
- `--topic`: filter by topic (used with `--all`).
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
