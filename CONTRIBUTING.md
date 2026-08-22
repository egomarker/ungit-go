# Contributing to Ungit-Go

Thanks for helping improve Ungit-Go. The project is a Go port of Ungit with a smaller runtime memory footprint, built-in dark/light themes, and additional fixes while preserving the familiar browser workflow.

## Before opening a change

- Search existing issues first.
- Keep compatibility changes deliberate. Internal `ungit` names may be part of the inherited frontend/API contract.
- Do not add a Node.js runtime dependency to the server path.
- Do not replace system Git with a partial Git implementation for convenience.
- Third-party Ungit plugins and Electron are intentionally outside project scope.

## Backend development

Requirements:

- Go 1.22 or newer
- Git 2.34 or newer

Run:

```sh
go test ./...
go vet ./...
go test -race ./...
go build -trimpath -o ungit-go ./cmd/ungit-go
```

Tests create temporary repositories and cover read/write Git operations, authentication, credentials, realtime events, watcher lifecycle, parsing, route inventory, and concurrency behavior.

## Frontend development

The browser UI is inherited from Ungit and still uses its Node-based build toolchain. Node is required only when changing frontend source or generated assets.

```sh
npm install
npm run build:frontend
npm run lint:frontend
```

Commit the rebuilt browser assets when frontend source changes. Release binaries embed those generated files.

### Themes

Theme source lives in `public/source/theme.js`, `public/source/theme-color.js`, and the Less sources. Keep all UI changes usable in both dark and light modes, and verify `system` mode as well.

## Repository layout

- `cmd/ungit-go/` — executable entry point.
- `internal/config/` — config/CLI compatibility.
- `internal/git/` — Git runner, parsers, and write helpers.
- `internal/server/` — HTTP API, auth, credentials, realtime, watchers.
- `internal/browser/` — normal-browser launcher.
- `internal/credentials/` — Git credential-helper mode.
- `public/source/` — browser application source.
- `public/less/` — shared UI styling source.
- `public/` — committed generated/runtime browser assets.
- `components/` — built-in UI components and their generated assets.
- `scripts/build.js` — frontend asset build.

## Pull requests

A code change should include tests whenever practical. Before submitting:

```sh
go test ./...
go vet ./...
```

For concurrency-sensitive server changes also run:

```sh
go test -race ./...
```

For frontend changes, rebuild assets and run the frontend linter.

Update `CHANGELOG.md` for user-visible changes.

## Upstream attribution

Do not remove the original Ungit copyright/license notice from the repository. Code copied or adapted from upstream Ungit remains covered by its preserved MIT notice; original Ungit-Go contributions are covered by the Egomarker MIT notice. See `LICENSE.md` and `NOTICE.md`.
