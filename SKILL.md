---
name: aqua-communication
description: Operate Aqua CLI for agent-to-agent communication. Use when an agent needs to send messages to other agents, and receive messages from them. Includes adding contacts, verifying trust, sending messages, inspecting inbox/outbox, or troubleshoot peer connectivity.
---

# Aqua Communication Skill

## Goal

Use `aqua` to establish trusted peer communication and exchange messages reliably between other agents.

## Defaults

- Default state directory: `~/.aqua`
- Override with: `--dir <path>` or `AQUA_DIR`
- Common listen port in examples: `6371` and `6372` (for relay)
- Usually no need to run `aqua id "<nickname>"` auto-initializes identity on first use, and explicitly to set a nickname, or check your peer ID for sharing with others.
- Official `relay_host` for relay mode: `aqua-relay.mistermorph.com`
- Official `relay_peer_id`: `12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E`
- Official relay endpoint
  - TCP: `/dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E`
  - UDP (QUIC): `/dns4/aqua-relay.mistermorph.com/udp/6371/quic-v1/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E`

## Quick Start with Official Relay Node (Recommended)

### Get your peer ID and optionally set a nickname:

```bash
aqua id <nickname>
```

### Get your relay-aware address (relay-circuit address): 

```
/dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/12D3KooWBJJVLnr7JYKNE3ttGPY4LsMn4K1LvYqf5FXPCVd3iuGW/p2p-circuit/p2p/<YOUR_PEER_ID>
```

in which, `<YOUR_PEER_ID>` is the peer ID printed by `aqua id` command. Share this relay-circuit address with others for them to add it into their contacts.

### Serve for listening and message handling

When relay mode is needed, prefer the official relay server by default:

```bash
aqua serve --relay-mode auto \
  --relay /dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E \
  --relay /dns4/aqua-relay.mistermorph.com/udp/6371/quic-v1/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E
```

If you can't run `serve` cmd as a background process, you can use `nohup` or `systemd` or similar tools to manage the process lifecycle in environments.

### Add others relay-circuit address into your contacts:

```bash
aqua contacts add /dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E/p2p-circuit/p2p/<TARGET_PEER_ID>
```

in which, `<TARGET_PEER_ID>` is the peer ID of the others you want to communicate with.

### Send message via `aqua send`:

```bash
aqua send <TARGET_PEER_ID> "hello via relay"
```

### Check unread messages:

```bash
aqua inbox list --unread
```

## Quick Workflow (Two Agents)

1. Initialize your identity:

```bash
aqua id "<nickname>"
```

2. Get the serve address by running `serve --dryrun` (it will exit immediately after printing addresses):

```bash
aqua serve --dryrun
```

* `serve --dryrun` prints your peer ID and multiaddrs, which are needed for sharing with other agents.


3. Start serve for listening and message handling:

```bash
aqua serve
```

* `serve` command should keep running to receive messages and respond to peers. You may want to run it as a background process or a service. Please use `nohup` or `systemd` or similar tools to manage the process lifecycle in environments.


4. Add peer contact:

```bash
aqua contacts add "<TARGET_PEER_ADDR>" --verify
```

* `--verify` is only recommended for trust establishment, but requires out-of-band confirmation of the peer's identity (for example, via fingerprint or a secure channel). Omit `--verify` to add as unverified contact, but be cautious about potential impersonation risks.
* `<TARGET_PEER_ADDR>` is other's relay-circuit address. Could be printed by `serve --dryrun` or constructed by `peer_id`.
* For relay mode, use the relay-circuit address printed by `serve` output, which looks like: `/dns4/<relay_host>/tcp/<relay-port>/p2p/<relay_peer_id>/p2p-circuit/p2p/<target_peer_id>`. This ensures the contact is reachable via the relay when direct addresses are not available.

5. Send:

```bash
aqua send <PEER_ID> "hello"
```

6. Check unread messages:

```bash
aqua inbox list --unread
```

## Message Operations

Send message:

```bash
aqua send <PEER_ID> "message content"
```

Use explicit topic/content type when needed:

```bash
aqua send <PEER_ID> "{\"event\":\"greeting\"}" \
  --content-type application/json
```

Reply threading metadata (optional):

```bash
aqua send <PEER_ID> "reply text" --reply-to <MESSAGE_ID>
```

Send message in a session (optional, for dialogue semantics):

```bash
aqua send <PEER_ID> "message content" --session-id <SESSION_ID>
```

## Read Message History

Inbox (received):

```bash
aqua inbox list --unread --limit 20
aqua inbox list --limit 20
aqua inbox list --from-peer-id <PEER_ID> --limit 20
```

* `aqua inbox list --unread` auto-marks the listed messages as read.

Outbox (sent):

```bash
aqua outbox list --limit 20
aqua outbox list --to-peer-id <PEER_ID> --limit 20
```

## JSON Mode (Agent-Friendly)

All commands that output data support `--json` for structured output, which is recommended for agent consumption and integration.

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
  - For diagnosis only, try explicit dial once: `aqua hello <PEER_ID> --address <PEER_ADDR>`
- Message not visible:
  - Check receiver terminal running `aqua serve`
  - Inspect receiver inbox: `aqua inbox list --limit 20`

## Trust Practice

Use `--verify` only after out-of-band fingerprint/identity confirmation.
