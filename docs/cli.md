# Aqua CLI Guide

## Initialization

```bash
aqua init
aqua id
```

## Contact Cards

Export (explicit addresses):

```bash
aqua card export \
  --address "/ip4/1.2.3.4/tcp/4001/p2p/<your_peer_id>" \
  --out ./my.card.json
```

Import:

```bash
aqua contacts import ./peer.card.json
```

View and verify:

```bash
aqua contacts list
aqua contacts show <peer_id>
aqua contacts verify <peer_id>
```

## Run Node

```bash
aqua serve --listen /ip4/0.0.0.0/tcp/4001
```

JSON output:

```bash
aqua serve --json
```

## Connect and Negotiate

```bash
aqua hello <peer_id> --address /ip4/1.2.3.4/tcp/4001/p2p/<peer_id>
aqua ping <peer_id>
aqua capabilities <peer_id>
```

## Push Messages

```bash
aqua push <peer_id> \
  --topic chat.message \
  --text "hello"
```

Conversation topics (for example `dm.reply.v1`) must include `--session-id`, which must be UUIDv7:

```bash
aqua push <peer_id> \
  --topic dm.reply.v1 \
  --session-id 0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456 \
  --text "follow up"
```

## Audit and Mailboxes

```bash
aqua audit list --limit 100
aqua inbox list --limit 50
aqua outbox list --limit 50
```

## Directory and Environment Variables

- `--dir <path>`: set MAEP data directory
- `AQUA_MAEP_DIR`: override default data directory when `--dir` is not provided
