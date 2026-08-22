# Ungit-Go architecture

Ungit-Go keeps the established Ungit browser UI but replaces the Node.js runtime server with Go.

```text
Browser
  │
  │ REST + realtime event compatibility layer
  ▼
Ungit-Go executable
  ├─ embedded frontend and built-in components
  ├─ HTTP/auth/session handling
  ├─ repository watching
  ├─ Git credential bridge
  └─ Git command orchestration
       │
       ▼
    system Git
```

## Runtime design

The release artifact is a single executable. `assets.go` embeds `public/`, `components/`, and the package metadata at compile time. The only intentional external runtime dependency is Git.

Ungit-Go continues to execute the real Git CLI. This preserves behavior for credentials, SSH, GPG, merge/rebase semantics, hooks, configuration, submodules, worktrees, and other features that would be difficult to reproduce faithfully with a partial Git implementation.

## Browser compatibility

The inherited browser code still uses internal names such as `ungit`, `ungit-*` modules, `/api/*` routes, and the `.ungitrc` configuration filename. These names are intentionally retained where renaming would break compatibility for no user benefit.

Built-in component manifests are supported as an internal asset mechanism. Loading third-party Ungit plugins is outside the project scope.

## Realtime events

The frontend-facing `io().on()` / `io().emit()` shape is preserved through a lightweight compatibility client. Server-to-browser events use an SSE connection and browser-to-server events use HTTP requests. This avoids requiring a Socket.IO/Node runtime while preserving the UI contract.

## Repository watching

Linux uses native inotify when available, with a portable fallback. Other platforms currently use the portable watcher. Watchers are scoped to active realtime clients and are released when no longer needed.

## Credential handling

When interactive HTTPS credentials are required, Git can invoke the Ungit-Go executable in hidden `credential-helper` mode. The server forwards the request to the active browser session and returns the response using Git's credential-helper protocol.

## Themes

The frontend supports `system`, `dark`, and `light`. Theme-specific CSS is built from the same Less sources, and the browser stores an explicit selection locally. `system` follows `prefers-color-scheme`.

## Development boundaries

Go owns the runtime backend. Node/npm are development-only tools for compiling the inherited browser JavaScript, TypeScript, Less, and component bundles. Generated runtime assets remain committed so a Go build does not require npm.
