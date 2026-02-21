---
name: aqua-communication
description: Operate Aqua CLI for agent-to-agent communication. Use when an agent needs to initialize identity, start a node, add contacts, verify trust, handshake, send messages, inspect inbox/outbox, or troubleshoot peer connectivity.
---

# Aqua Communication Skill

## Goal

Use `aqua` to establish trusted peer communication and exchange messages reliably.

## Defaults

- Default state directory: `~/.aqua`
- Override with: `--dir <path>` or `AQUA_DIR`
- Common listen port in examples: `6371`

## Quick Workflow (Two Agents)

1. Initialize local identity:

```bash
aqua init
aqua id "<nickname>"
```

2. Start node:

```bash
aqua serve
```

3. Copy one printed address from `serve` output (must end with `/p2p/<peer_id>`).

4. Add peer contact:

```bash
aqua contacts add "<PEER_ADDR>" --verify
```

5. Handshake and send:

```bash
aqua send <PEER_ID> "hello"
```

## Message Operations

Send message:

```bash
aqua send <PEER_ID> "message content"
```

Use explicit topic/content type when needed:

```bash
aqua send <PEER_ID> "{\"event\":\"x\"}" \
  --topic custom.note.v1 \
  --content-type application/json
```

Reply threading metadata (optional):

```bash
aqua send <PEER_ID> "reply text" --reply-to <MESSAGE_ID>
```

Session behavior:

- For `chat.message`, session is expected.
- If `--session-id` is omitted, CLI auto-generates UUIDv7, but it will lose session semantics (treated as one-off message).

## Read Message History

Inbox (received):

```bash
aqua inbox list --limit 20
aqua inbox list --from-peer-id <PEER_ID> --limit 20
aqua inbox list --unread --limit 20
```

Note: `aqua inbox list --unread` auto-marks the listed messages as read.

Outbox (sent):

```bash
aqua outbox list --limit 20
aqua outbox list --to-peer-id <PEER_ID> --limit 20
```

Mark processed messages as read:

```bash
aqua inbox mark-read <MESSAGE_ID>
# or batch mark all unread from one peer/topic
aqua inbox mark-read --all --from-peer-id <PEER_ID> --topic chat.message
```

## JSON Mode (Agent-Friendly)

Prefer `--json` for machine consumption:

```bash
aqua id --json
aqua contacts list --json
aqua send <PEER_ID> "hello" --json
aqua inbox list --limit 10 --json
```

## Troubleshooting Checklist

- `contact not found`:
  - Run `aqua contacts list`
  - Re-add contact with `aqua contacts add "<PEER_ADDR>"`
- Cannot dial peer:
  - Confirm peer process is running: `aqua serve`
  - Re-check copied address and `/p2p/<peer_id>` suffix
  - Try explicit dial once: `aqua hello <PEER_ID> --address <PEER_ADDR>`
- Message not visible:
  - Check receiver terminal running `aqua serve`
  - Inspect receiver inbox: `aqua inbox list --limit 20`

## Trust Practice

Use `--verify` only after out-of-band fingerprint/identity confirmation.
