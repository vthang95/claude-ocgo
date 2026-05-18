# ocgo

Proxy server that translates Anthropic API requests to OpenCode Go upstream services.

## Build

```sh
make build
```

## Run

```sh
OPENCODE_API_KEY=<your-key> ./ocgo run
```

### Command line options

```bash
$ ocgo -h
ocgo - OpenCode Go proxy for Claude

Usage:
  ocgo <command> [flags]

Commands:
  run                Start the proxy server
  copilot auth       Authenticate with GitHub Copilot (OAuth device flow)
  copilot usage      Show Copilot usage/quota
  stop               Stop daemon
  status             Show daemon status
  logs               Tail daemon logs (--verbose for full details)

Run flags:
  -p, --port           <port>      Listen port (default: 14242, env: PORT)
  -u, --upstream       <url>       Upstream base URL (env: OPENCODE_API_URL)
  -m, --model          <model>     Default model (default: qwen3.6-plus, env: DEFAULT_MODEL)
  -pv, --provider     <provider>   Upstream provider: opencode-go or copilot (env: PROVIDER)
  -wf, --with-fallback             Enable automatic fallback to alternative models on failure
  -om, --overwrite-model           Always use default model, ignore Claude's model setting
  -d, --daemon                     Run server in background (daemon mode)

API key is read from OPENCODE_API_KEY environment variable.
For Copilot, run 'ocgo copilot auth' first to authenticate with GitHub.
```

## Usage with Claude Code

### opencode-go provider (default)

```sh
OPENCODE_API_KEY=<your-key> ./ocgo run
```

```sh
ANTHROPIC_BASE_URL=http://127.0.0.1:14242 \
ANTHROPIC_MODEL=qwen3.6-plus \
claude
```

### Copilot provider

```sh
# Authenticate with GitHub (one-time setup)
ocgo copilot auth

# Start the proxy with Copilot provider
ocgo run --provider copilot
```

```sh
ANTHROPIC_BASE_URL=http://127.0.0.1:14242 \
ANTHROPIC_MODEL=claude-sonnet-4-7 \
claude
```

### Check Copilot usage

```sh
ocgo copilot usage
```

## Supported models

- `qwen3.6-plus`
- `qwen3.5-plus`
- `minimax-m2.7`
- `minimax-m2.5`
- `kimi-k2.6`
- `glm-5`
- `glm-5.1`
