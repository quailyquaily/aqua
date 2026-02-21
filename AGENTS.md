# Repository Guidelines

## Project Structure & Module Organization
- `cmd/aqua/main.go`: CLI entrypoint (`aqua`) and command wiring.
- `maep/`: core MAEP domain logic (identity, contacts, RPC, node/session behavior).
- `internal/fsstore/`: file-backed storage primitives (atomic writes, JSON/JSONL helpers, locks, indexing).
- `docs/`: operator-facing docs (`architecture.md`, `cli.md`).
- Tests live next to implementation files and use `*_test.go`.

## Build, Test, and Development Commands
- `go build -o ./bin/aqua ./cmd/aqua`: build the CLI binary.
- `go run ./cmd/aqua --help`: run locally without a manual build.
- `go test ./...`: run all unit tests (baseline check before every PR).
- `go test -race ./...`: run race detector for concurrency/storage-sensitive changes.
- `go test ./maep -run TestExtractRPCIDForError`: run a focused test during iteration.

Tip: isolate local state during manual testing:
`AQUA_MAEP_DIR=$(mktemp -d) go run ./cmd/aqua init`

## Coding Style & Naming Conventions
- Language target is Go (`go.mod` uses Go 1.24.x); always format with `gofmt`.
- Follow Go naming: exported identifiers in `PascalCase`, internal helpers in `camelCase`.
- Keep package names short/lowercase (`maep`, `fsstore`) and files organized by concern (`*_test.go` mirrors production files).
- Prefer wrapped errors with context (`fmt.Errorf("...: %w", err)`), especially at storage/network boundaries.

## Testing Guidelines
- Use the standard `testing` package with clear `Test...` names describing behavior.
- Prefer table-driven tests plus `t.Run(...)` for variants.
- Use `t.Parallel()` where safe and `t.TempDir()` for filesystem isolation.
- Cover both success and failure paths (invalid input, decode errors, lock/path failures).

## Commit & Pull Request Guidelines
- This repository currently has no commit history; start with Conventional Commits (for example: `feat(maep): add protocol history lookup`, `fix(fsstore): reject invalid lock key`).
- Keep commits scoped and include related tests/docs in the same change.
- PRs should include: purpose, impacted paths, test commands run, and linked issue (if any).
- For CLI behavior changes, include sample command/output snippets in the PR description.

## Security & Configuration Tips
- Never commit real MAEP state or keys from `~/.aqua/maep`.
- Use `--dir` or `AQUA_MAEP_DIR` for disposable test data.
- Sanitize peer IDs, addresses, and card payloads before sharing logs or fixtures.
