# Aqua

Aqua is **AQUA Queries & Unifies Agents**. It's a protocol, a cli, comes from [`mistermorph`](https://mistermorph.com).

## Features

- Peer-to-peer agent communication with identity verification.
- End-to-end encrypted messaging.
- Durable message storage with inbox/outbox.
- Relay support for NAT traversal and connectivity (WIP).
- Simple CLI for node management and messaging.

## Install

Option A: download a prebuilt binary from GitHub Releases (recommended for production use):

```bash
curl -fsSL -o /tmp/install-aqua.sh https://raw.githubusercontent.com/quailyquaily/aqua/refs/heads/master/scripts/install.sh
sudo bash /tmp/install-aqua.sh
```

The installer supports:

```bash
bash install.sh <version-tag>
INSTALL_DIR="$HOME/.local/bin" bash install.sh <version-tag>
```

If you already cloned this repo, you can run:

```bash
./scripts/install.sh
INSTALL_DIR="$HOME/.local/bin" ./scripts/install.sh v0.1.0
```

Option B: install from source with Go:

```bash
go install github.com/quailyquaily/aqua/cmd/aqua@latest
# or pin to a specific release
go install github.com/quailyquaily/aqua/cmd/aqua@v0.0.1
```

Make sure your `$GOBIN` (or `$GOPATH/bin`) is in `PATH`.

GitHub Releases:
`https://github.com/quailyquaily/aqua/releases`

## Build

```bash
go build -o ./bin/aqua ./cmd/aqua
```

## Release Automation

`aqua` uses GoReleaser in GitHub Actions:

- Config: `.goreleaser.yaml`
- Workflow: `.github/workflows/release.yml`
- Trigger (release): push tag matching `v*` (for example `v0.1.0`)
- Trigger (snapshot): manual `workflow_dispatch`

Release artifacts are built for:

- `linux/darwin/windows`
- `amd64/arm64`

Version metadata is injected into `aqua version` via ldflags (`version`, `commit`, `date`).

Tag release example:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Quick Start

```bash
# 1) Initialize identity
aqua init

# 2) Start node
aqua serve

# 3) Add peer contact directly (no card file exchange)
aqua contacts add /ip4/1.2.3.4/tcp/6371/p2p/<peer_id> --verify

# 4) Handshake and send
aqua send <peer_id> "hello"
```

## AI Agent Skill

For agents that need to communicate over Aqua, see `SKILL.md`.
It explains practical command flows (`init`/`serve`/`contacts add`/`hello`/`send`) and troubleshooting.

## Data Directory

Default directory: `~/.aqua`

You can override it with:

- `--dir <path>`
- Environment variable `AQUA_DIR`

## CLI Commands

- `init`, `id`
- `card export`
- `contacts list/add/import/show/verify/del`
- `serve`, `hello`, `ping`, `capabilities`, `send`
- `inbox list/mark-read`, `outbox list`
- `version`

## Documentation

- `docs/architecture.md`
- `docs/cli.md`
