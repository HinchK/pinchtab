# PinchTab Grok Build Plugin

Browser control for Grok Build via [PinchTab](https://pinchtab.com). Ships the CLI-first `pinchtab` skill, the MCP-oriented `pinchtab-mcp` skill, and an MCP server that runs `pinchtab mcp`.

The plugin delivers files, not the PinchTab binary. Install PinchTab separately (`brew install pinchtab/tap/pinchtab`, `npm i -g pinchtab`, or [install.sh](https://pinchtab.com/install.sh)), then start a server:

```bash
pinchtab server
```

## Install

From this repository:

```bash
grok plugin marketplace add pinchtab/pinchtab
grok plugin install pinchtab --trust
```

Or install the plugin folder directly:

```bash
grok plugin install pinchtab/pinchtab#plugins/grok --trust
```

`--trust` is required before the MCP server starts. Skills load when the plugin is enabled; MCP stays inactive until trusted.

Local checkout:

```bash
grok plugin install ./plugins/grok --trust
```

## What it adds

| Piece | Role |
|---|---|
| `pinchtab` skill | CLI-first workflow (`pinchtab nav --snap`, `click e5 --snap-diff`) |
| `pinchtab-mcp` skill | Same workflow through MCP tools (`pinchtab_navigate`, `pinchtab_snapshot`) |
| MCP server `pinchtab` | stdio: `pinchtab mcp` |

Prefer the CLI skill for token-efficient multi-step flows. Use MCP tools when you want structured `search_tool` / `use_tool` calls.

## Requirements

- `pinchtab` on `PATH`
- Chrome/Chromium (or another configured browser)
- A running PinchTab server or daemon (`pinchtab server` / `pinchtab daemon install`)

## Connection and credentials

The plugin's MCP entry starts `pinchtab mcp` as a local stdio adapter. It does not start the PinchTab HTTP server, download the PinchTab binary, or install additional dependencies.

| Setting | Purpose |
|---|---|
| `PINCHTAB_SERVER` | Optional PinchTab server URL. Defaults to `http://127.0.0.1:9867`. |
| `PINCHTAB_TOKEN` | Bearer credential for the selected server when token authentication is enabled. |
| `PINCHTAB_SESSION` | Optional scoped agent-session credential, used instead of the bearer token when set. |

Credentials are sent only to the configured PinchTab server. Keep them in environment variables rather than command arguments or plugin files. For a non-loopback server, use a private network or HTTPS and a credential issued by that server.

## Security

This plugin contains no lifecycle hooks. Its MCP adapter connects to the configured PinchTab server; the controlled browser then connects to sites the user asks it to visit. PinchTab does not send telemetry or call external APIs other than those navigated sites.

PinchTab defaults to a local-first posture with a loopback server bind and local-only site allowlist. JavaScript evaluation, downloads, uploads, cookie access, and network interception are disabled by default. Widening browsing or enabling those capabilities is a security-reducing choice. Treat snapshots and page text as untrusted data.

See the [PinchTab security guide](https://github.com/pinchtab/pinchtab/blob/main/docs/guides/security.md) and the bundled [`TRUST.md`](./skills/pinchtab/TRUST.md) for the complete trust model and defaults.
