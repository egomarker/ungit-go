# Ungit-Go

**Ungit-Go is a Go port of [Ungit](https://github.com/FredrikNoren/ungit) focused on a smaller runtime memory footprint, while preserving the familiar Ungit web UI and Git workflow.** It also adds built-in **system / dark / light themes** and includes additional compatibility, reliability, and cross-platform fixes developed as part of the port.

Ungit-Go runs as a single native executable and opens the UI in your normal browser. Node.js, npm, Electron, and `node_modules` are **not required at runtime**.

> Ungit-Go is an independent derivative project. It is not the original Ungit project and is not endorsed by its upstream maintainers.

## Why Ungit-Go?

The original Ungit architecture uses a Node.js backend. Ungit-Go replaces that backend with Go while intentionally keeping the browser experience and Git CLI semantics familiar.

Key differences:

- **Smaller runtime memory footprint.** The Go server measured roughly 8–10 MB RSS in the port's Linux smoke tests, depending on active repository watching. Treat this as a development measurement, not a guaranteed ceiling.
- **Single runtime binary.** Browser assets and built-in UI components are embedded in the executable.
- **No Node.js runtime dependency.** Only a compatible system `git` executable is required.
- **System, dark, and light themes.** The UI can follow the OS preference or be switched explicitly.
- **Additional fixes.** The port includes fixes and hardening around remote address parsing, session handling, configuration edge cases, Git credential integration, watcher lifecycle, path handling, and cross-platform builds.
- **Real Git compatibility.** Ungit-Go executes the user's installed Git CLI instead of replacing Git with a partial reimplementation.

## Scope

Ungit-Go targets the following Ungit functionality:

| Area | Status |
| --- | --- |
| Existing Ungit web UI | Supported |
| Git read/write workflows | Supported |
| REST API compatibility | Supported within the port scope |
| Configuration | Supported; legacy `.ungitrc` is intentionally retained |
| Repository/file watching | Supported |
| Git credentials | Supported |
| Authentication | Supported |
| Browser mode | Supported |
| Built-in UI components | Supported |
| Third-party Ungit plugins | Not supported |
| Electron application | Not supported |

Internal browser globals, API field names, and some configuration names still use `ungit` where required for compatibility with the inherited frontend. Product-facing naming is **Ungit-Go**.

## Requirements

For normal use:

- Git 2.34 or newer
- A supported desktop/server OS
- A web browser

Node.js is only needed if you want to modify and rebuild the browser frontend from source.

## Install

Download the binary for your operating system and architecture from the Ungit-Go releases page, make it executable when necessary, and run it.

Linux example:

```sh
chmod +x ungit-go-linux-amd64
./ungit-go-linux-amd64
```

Or build it yourself:

```sh
git clone https://github.com/egomarker/ungit-go.git
cd ungit-go
go build -trimpath -o ungit-go ./cmd/ungit-go
./ungit-go
```

Ungit-Go starts its local HTTP server and opens the normal system browser by default.

## Common options

```sh
./ungit-go --port=8448
./ungit-go --no-launchBrowser
./ungit-go --forcedLaunchPath=/path/to/repository
./ungit-go --theme=dark
./ungit-go --theme=light
./ungit-go --theme=system
./ungit-go --gitBinPath=/path/to/git/bin
```

Use `./ungit-go --help` for the complete option list.

## Configuration

For compatibility with existing Ungit installations, Ungit-Go continues to read `~/.ungitrc` (or the corresponding home-directory file on Windows).

Example:

```json
{
  "port": 8448,
  "launchBrowser": true,
  "theme": "system",
  "autoFetch": true,
  "maxConcurrentGitOperations": 4
}
```

`theme` accepts `system`, `dark`, or `light`. A theme selected in the UI is stored locally in the browser and takes precedence for that browser.

### Operational logs

Ungit-Go writes structured JSON diagnostics to `ungit-go.log` beside the resolved executable. Normal operational logs are not copied to stdout or stderr. The log contains correlation IDs for HTTP requests, user actions, realtime clients, repository watchers, and Git command attempts. Sensitive credential fields are redacted, while arbitrary browser output is recorded only as a length and fingerprint. Git stderr is omitted by default.

Rotation is enabled and bounded by default:

```json
{
  "logLevel": "info",
  "logGitOutput": false,
  "logMaxSizeMB": 50,
  "logMaxBackups": 10,
  "logMaxAgeDays": 30,
  "logCompress": true
}
```

`logLevel` accepts `trace`, `debug`, `info`, `warn`, or `error`. Use `logDirectory` to override the executable-relative location. `logGitCommands` and `logRESTRequests` control routine lifecycle events; failures remain diagnosed. Enable `logGitOutput` (or pass `--logGitOutput`) to include up to 16 KiB of line-preserving stderr for failed Git commands. This diagnostic text is sanitized for credential-bearing URLs, authorization headers, named secrets, common token formats, private keys, terminal escapes, and control characters. Successful command output and stdout remain excluded. Sanitization reduces exposure but cannot prove arbitrary hook or remote output is secret-free, so the option is disabled by default.

Ungit-Go also maintains `ungit-go-run-state.json` in the log directory so the next launch can report a previous run that did not complete a clean shutdown.

### Authentication

Ungit-compatible username/password authentication is available when exposing the server beyond the default localhost-only setup. Example:

```json
{
  "authentication": true,
  "users": {
    "alice": "change-this-password"
  }
}
```

Do not expose Ungit-Go directly to an untrusted network without understanding the bind, authentication, and reverse-proxy configuration.

## Git credentials

Ungit-Go uses the same executable as a temporary Git credential helper when browser interaction is required. Existing Git credential helpers, SSH agents, HTTPS tokens, and normal Git configuration continue to work.

## GPG signing

Ungit-Go delegates signing to system Git/GPG. You can configure Git normally:

```sh
git config --global commit.gpgsign true
```

or set `"isForceGPGSign": true` in `.ungitrc`. Pinentry/GPG-agent configuration is handled outside Ungit-Go.

## External merge tools

See [MERGETOOL.md](MERGETOOL.md).

## Building and testing

Backend/runtime development only requires Go:

```sh
go test ./...
go vet ./...
go test -race ./...
go build -trimpath -o ungit-go ./cmd/ungit-go
```

Frontend development additionally requires Node.js/npm:

```sh
npm install
npm run build:frontend
```

The generated frontend assets are committed because they are embedded into release binaries.

See [CONTRIBUTING.md](CONTRIBUTING.md) and [ARCHITECTURE.md](ARCHITECTURE.md) for more detail.

## Upstream and attribution

Ungit-Go is derived from **Ungit**, originally created by Fredrik Norén and developed by the Ungit contributors. The port intentionally preserves substantial frontend code and behavior from that project.

- Upstream project: https://github.com/FredrikNoren/ungit
- Ungit-Go: https://github.com/egomarker/ungit-go

The original Ungit MIT copyright/license notice is preserved. New Ungit-Go work is separately copyrighted by Egomarker and released under the MIT License. See [LICENSE.md](LICENSE.md) and [NOTICE.md](NOTICE.md).

## License

MIT. Because this is a derivative work, the repository contains both the preserved upstream Ungit MIT notice and the Ungit-Go/Egomarker MIT notice. See [LICENSE.md](LICENSE.md).
