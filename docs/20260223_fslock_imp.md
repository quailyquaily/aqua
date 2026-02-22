# Aqua FS Lock Improvement Plan (2026-02-23)

Status: Phase 1 Implemented, Phase 2 Pending  
Owner: core runtime/fsstore  
Scope: `aqua/node.go`, `aqua/file_store.go`, `internal/fsstore/*`

## 1. Background

Current incoming message persistence path (`agent.data.push`) performs multiple store operations per message:

1. dedupe lookup
2. dedupe write
3. dedupe prune
4. inbox append

Most write paths share one fs lock key (`state.main`) and one process-local mutex in `FileStore`, so high message rate causes lock contention and queueing.

## 2. Problem Statement

Observed risks in current model:

1. High-frequency lock contention: one message may acquire lock multiple times.
2. Process-local serialization: reads and writes all queue behind one `sync.Mutex`.
3. Unbounded lock wait in some paths because store calls use `context.Background()`.

Out of scope for this doc:

- Improving disk durability guarantees.  
  Product decision for this optimization: process crash message loss is acceptable.

## 3. Goals

1. Reduce hot-path lock demand for incoming traffic.
2. Keep incoming RPC latency stable under burst load.
3. Bound lock wait and avoid indefinite blocking.
4. Isolate unrelated resources (inbox/outbox/contacts/identity) to reduce cross-impact.

## 4. Non-Goals

1. Exactly-once delivery across process restarts.
2. Full transactional semantics across multiple store files.
3. Protocol-level behavior changes.

## 5. Core Decisions

1. `accepted` means "accepted by runtime queue", not "fsync completed".
2. Incoming messages are first buffered in memory, then flushed in batches.
3. `dedupe prune` leaves hot path and becomes periodic background work.
4. Locking is split by resource instead of one global `state.main`.
5. Queue saturation returns `ERR_BUSY`.
6. `OnDataPush` fires after enqueue succeeds (not after flush).

## 6. Proposed Architecture

### 6.1 In-Memory Ingress Queue

- Add bounded queue for accepted incoming messages.
- RPC handler does:
  1. validate request
  2. check in-memory dedupe cache
  3. enqueue
  4. return success
- If queue is full: return `ERR_BUSY` immediately.
- Keep queue behavior fixed in runtime (not exposed as `NodeOptions`):
  - queue size `4096`
  - enqueue timeout `20ms`

### 6.2 Single Batch Writer Goroutine

- One writer goroutine drains queue and writes in batches:
  - flush when `batch_size` reached, or
  - flush interval elapsed
- Runtime fixed values:
  - flush max batch `256`
  - flush interval `200ms`

Batch writer responsibilities:

1. append inbox messages
2. log flush failures (no blocking retry in request path)
3. dedupe cleanup on periodic ticker

### 6.3 Hot-Path Dedupe in Memory

- Maintain in-memory TTL map keyed by:
  - `(from_peer_id, topic, idempotency_key)`
- Prevent duplicate enqueue during runtime.
- Background task periodically compacts cache by TTL and max entries.

Persistence of dedupe records becomes optional:

- Keep as best-effort snapshot (periodic), not per-message sync write.

### 6.4 Split fsstore Lock Keys

Replace single `state.main` lock with resource locks:

- `state.identity`
- `state.contacts`
- `state.inbox`
- `state.outbox`
- `state.dedupe`
- `state.readstate`

Rules:

1. Single-resource operation acquires only one resource lock.
2. Multi-resource operations must follow fixed lock order to avoid deadlock:
   - `identity -> contacts -> dedupe -> inbox -> outbox -> readstate`
3. Keep lock scope minimal; avoid loading unrelated files while holding a lock.

### 6.5 Context and Timeout Discipline

- Remove `context.Background()` usage from store calls in runtime hot path.
- Use bounded timeout wrappers for store operations.
- On timeout:
  - enqueue path returns `ERR_BUSY`
  - writer path logs failure and drops that flush batch (crash-loss accepted)

## 7. API/Code Changes

### 7.1 NodeOptions scope

- No new queue/flush tuning fields are exposed in `NodeOptions`.
- Keep user-facing API small; use internal constants for ingress buffering.

### 7.2 Store interface changes

- Add batch-oriented methods to reduce lock operations:

- `AppendInboxMessages(ctx, []InboxMessage) error`
- Keep `AppendInboxMessage` as compatibility wrapper over batch append.

## 8. Failure Semantics

Because crash-loss is accepted:

1. Messages in memory queue may be lost on process crash.
2. Messages already flushed to inbox remain durable as before.
3. Duplicate suppression across restart is best-effort unless dedupe persistence is enabled.

This behavior must be explicit in CLI/docs.

## 9. Rollout Plan

### Phase 1: Queue + Batch Writer (No lock split yet)

Status: completed.

1. Implemented bounded queue and batch writer.
2. Moved dedupe prune out of hot path.
3. Replaced hot-path unbounded store contexts with timeout wrappers.

Expected result:

- Largest latency/throughput gain with minimal risk.

### Phase 2: Split fsstore locks

1. Introduce per-resource lock keys.
2. Keep fixed lock order for multi-resource ops.
3. Add contention tests for concurrent inbox/outbox/contacts.

Expected result:

- Less interference between unrelated features.

### Phase 3: Tighten behavior and observability

1. Add metrics for queue depth, enqueue reject, flush batch size, flush latency.
2. Add structured logs for queue saturation and writer lag.
3. Evaluate whether dedupe file persistence should remain enabled by default.

## 10. Testing Strategy

1. Concurrency tests:
   - burst incoming push under high parallelism
   - parallel inbox/outbox/contacts operations
2. Property tests:
   - no duplicate enqueue for same dedupe key during runtime
3. Fault tests:
   - queue full behavior
   - writer flush error and retry behavior
4. Benchmark:
   - baseline vs batch mode p50/p95 latency and throughput

## 11. Open Questions

1. Keep current global fs lock in `FileStore` for Phase 2, or split lock keys first and then relax process-local mutex?
