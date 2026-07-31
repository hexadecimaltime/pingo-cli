# Pingo CLI

A Go TUI (Terminal User Interface) client for [Pingo](https://pingo.coactum.de) built with Charm Bubble Tea. It features a live countdown, WebSocket-based real-time updates, and interactive terminal inputs.

![Pingo CLI Showcase](demo/pingo-showcase.gif)

## Features

- Interactive Terminal UI using Charm Bubble Tea
- Join sessions instantly using a session code
- Real-time survey status and countdown updates via WebSockets (Faye)
- Support for multiple poll types: Single Choice, Multiple Choice, and Text/Number inputs
- Multi-language support (i18n)
- Cross-platform binaries (Windows, macOS, Linux)

## Usage

If you downloaded a release or cross-compiled the app, your executable name will include the target OS and architecture (e.g., `pingo-linux-amd64`, `pingo-darwin-arm64`, `pingo-windows-amd64.exe`).
Natively built binaries will simply be named `pingo` or `pingo.exe`. Substitute `pingo` with your exact binary name in the commands below.
You can also just rename the binary to `pingo` for simplicity.

You can launch the application by providing an optional session code and language flag. If no session code is provided, the UI will prompt you to enter one.

```bash
pingo [SESSION_CODE] [flags]
```

### Flags

- `-l`, `--lang <tag>`: Language tag (e.g., `en`, `de`). Defaults to the system locale.
- `-v`, `--version`: Print version and exit.
- `-h`, `--help`: Show help and usage instructions.

### Examples

```bash
pingo-linux-amd64 620610 --lang de
.\pingo-windows-amd64.exe --lang en 620610
pingo -l de
```

## Development and Building

This project uses [`just`](https://github.com/casey/just) as a command runner. The available commands are defined in the `justfile`.

- Run in development: `just run [SESSION_CODE]`
- Build for your current platform (outputs to `bin/`): `just build`
- Format and tidy code before committing: `just tidy`
- Build release binaries for all common platforms: `just release`
- Clean build artifacts: `just clean`

### Release Artifacts

When running `just release`, the following optimized binaries are automatically generated in the `bin/` directory:

- `pingo-linux-amd64`
- `pingo-linux-arm64`
- `pingo-darwin-amd64`
- `pingo-darwin-arm64`
- `pingo-windows-amd64.exe`
