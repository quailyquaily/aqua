# Aqua Relay V2

Status: Implemented (MVP)  
Last updated: 2026-02-21

## 1. Overview

Aqua Relay V2 enables cross-network communication by using libp2p Circuit Relay v2.

Key points:

- Relay transport is Circuit Relay v2 (not a custom Aqua relay protocol).
- Relay server runs as a dedicated command: `aqua relay serve`.
- Edge nodes keep using `aqua serve` and can reserve relay slots via `--relay`.
- Dial behavior defaults to `--relay-mode auto` (direct first, then relay fallback).
- Existing Aqua hello/RPC methods stay unchanged above transport.

## 2. Commands

### 2.1 Relay server

Run relay service:

```bash
aqua relay serve \
  --listen /ip4/0.0.0.0/tcp/6371 \
  --listen /ip4/0.0.0.0/tcp/6372/ws
```

Allowlist mode:

```bash
aqua relay serve \
  --allow-peer <peer_id_a> \
  --allow-peer <peer_id_b>
```

Allowlist behavior:

- empty allowlist (default): allow all peers
- non-empty allowlist: only listed peers can reserve
- relayed connect requires both source and destination peers to be allowlisted

### 2.2 Edge node

Run edge node with relay reservation:

```bash
aqua serve \
  --relay /dns4/relay.example.com/tcp/6371/p2p/<relay_peer_id> \
  --relay-mode auto
```

`--relay-mode` values:

- `auto` (default): try direct addresses first, fallback to relay addresses
- `off`: direct addresses only
- `required`: relay addresses only

### 2.3 Dial commands

The following commands support `--relay-mode auto|off|required`:

- `aqua hello`
- `aqua ping`
- `aqua capabilities`
- `aqua send`

### 2.4 Contact card relay advertisement

Relay-aware export:

```bash
aqua card export \
  --listen /ip4/0.0.0.0/tcp/6371 \
  --relay /dns4/relay.example.com/tcp/6371/p2p/<relay_peer_id> \
  --advertise both
```

`--advertise` values:

- `direct`: direct addresses only
- `relay`: relay-circuit addresses only
- `both`: direct + relay addresses
- `auto` (default): direct only when relay addresses are absent, otherwise both

## 3. Relay Events (`aqua serve --json`)

Relay-related JSON events are emitted as separate records, with fields:

- `event`
- `path`
- `relay_peer_id`
- `target_peer_id`
- `reason`
- `timestamp`

Current event names:

- `relay.reservation.ok`
- `relay.reservation.failed`
- `relay.path.selected`
- `relay.fallback`

## 4. Data Path

For relay-assisted messaging:

1. Edge B reserves a relay slot on relay R.
2. B advertises relay-circuit address including B peer ID.
3. Edge A dials B.
4. In `auto` mode, A attempts direct first; on direct failure, A retries relay path.
5. Once connected, normal Aqua `hello` and JSON-RPC continue unchanged.

## 5. Compatibility

V2 is additive and keeps existing behavior by default:

- without relay flags, existing direct networking flow remains unchanged
- RPC and hello protocol IDs are unchanged
- existing contact cards remain valid; relay addresses are additive
- relay events are added to `serve --json` output without changing existing `agent.data.push` event fields

## 6. Current Limits

Current MVP behavior:

- relay reservation is attempted at startup when `--relay` is configured
- no persistent relay reservation state file is maintained yet
- no advanced relay policy/rate configuration flags are exposed yet (beyond allowlist)
