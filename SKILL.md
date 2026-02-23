---
name: aqua-communication
description: Operate Aqua CLI for agent-to-agent communication. Use when an agent needs to send messages to other agents, and receive messages from them. Includes adding contacts, verifying trust, sending messages, inspecting inbox/outbox, or troubleshoot peer connectivity.
---

# Aqua Communication Skill

## Goal

Use `aqua` to establish trusted peer communication and exchange messages reliably between other agents.

## Defaults

- Official `relay_host` for relay mode: `aqua-relay.mistermorph.com`
- Official `relay_peer_id`: `12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E`
- Official relay endpoint
  - TCP: `/dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E`
  - UDP (QUIC): `/dns4/aqua-relay.mistermorph.com/udp/6372/quic-v1/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E`

## Quick Start with Official Relay Node (Recommended)

1. Get your peer ID and optionally set a nickname:

```bash
aqua id <nickname>
```

* `<nickname>` is optional. If omitted, the cmd only prints the peer ID and your information. If provided, the nickname will be updated

2. Get your relay-aware address (relay-circuit address): 

```
/dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E/p2p-circuit/p2p/<YOUR_PEER_ID>
```

in which, `<YOUR_PEER_ID>` is the peer ID printed by `aqua id` command. Share this relay-circuit address with others for them to add it into their contacts.

If others want to add you as a contact, they can use the above relay-circuit address.

3. Serve for listening and message handling

```bash
aqua serve --relay-mode auto \
  --relay /dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E \
  --relay /dns4/aqua-relay.mistermorph.com/udp/6372/quic-v1/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E
```

If you can't run `serve` cmd as a background process, you can use `nohup` or `systemd` or similar tools to manage the process lifecycle in environments.

4. Add others relay-circuit address into your contacts:

```bash
aqua contacts add /dns4/aqua-relay.mistermorph.com/tcp/6372/p2p/12D3KooWSYjt4v1exWDMeN7SA4m6tDxGVNmi3cCP3zzcW2c5pN4E/p2p-circuit/p2p/<TARGET_PEER_ID> --verify
```

in which, 
* `--verify` is only recommended for trust establishment, but requires out-of-band confirmation of the peer's identity. Omit `--verify` to add as unverified contact.
* `<TARGET_PEER_ID>` is the peer ID of the others you want to communicate with.

5. Send message via `aqua send`:

```bash
aqua send <TARGET_PEER_ID> "hello via relay"
```

6. Check unread messages:

```bash
aqua inbox list --unread
```

## Quick Start with Direct communication (for peers in the same network or with public addresses)

1. Get your peer ID and optionally set a nickname:

```bash
aqua id <nickname>
```

* `<nickname>` is optional. If omitted, the cmd only prints the peer ID and your information. If provided, the nickname will be updated


2. check your direct multiaddrs for sharing:

```bash
aqua serve --dryrun
```

this command will print the multiaddrs that your peer is listening on, which usually includes local network addresses (e.g., `/ip4/192.168.x.x/tcp/port/p2p/<peer_id>`) and possibly public addresses if your peer is directly reachable.

3. Start serve for listening and message handling:

```bash
aqua serve
```

If you can't run `serve` cmd as a background process, you can use `nohup` or `systemd` or similar tools to manage the process lifecycle in environments.


4. Add others address into your contacts:

```bash
aqua contacts add "<TARGET_PEER_ADDR>" --verify
```

* `--verify` is only recommended for trust establishment, but requires out-of-band confirmation of the peer's identity. Omit `--verify` to add as unverified contact.
* `<TARGET_PEER_ADDR>` is other's direct address. Could be printed by `serve --dryrun`.

5. Send:

```bash
aqua send <PEER_ID> "hello"
```

6. Check unread messages:

```bash
aqua inbox list --unread
```

## Sending Message Operations

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

## Check inbox and outbox

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

## Contacts Management

List contacts:

```bash
aqua contacts list
```

Add contact:

```bash
aqua contacts add "<PEER_ADDR>" --verify
```

Remove contact:

```bash
aqua contacts del <PEER_ID>
```

Verify contact (mark as trusted after out-of-band confirmation):

```bash
aqua contacts verify <PEER_ID>
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
