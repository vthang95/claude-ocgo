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
  run     Start the proxy server
  stop    Stop daemon
  status  Show daemon status
  logs    Tail daemon logs

Run flags:
  -p, --port           <port>   Listen port (default: 14242, env: PORT)
  -u, --upstream       <url>    Upstream base URL (env: OPENCODE_API_URL)
  -m, --model          <model>  Default model (default: qwen3.6-plus, env: DEFAULT_MODEL)
  -wf, --with-fallback            Enable automatic fallback to alternative models on failure
  -om, --overwrite-model          Always use default model, ignore Claude's model setting
  -d, --daemon                    Run server in background (daemon mode)

API key is read from OPENCODE_API_KEY environment variable.
```
```

## Usage with Claude Code

```sh
ANTHROPIC_BASE_URL=http://127.0.0.1:14242 \
ANTHROPIC_MODEL=qwen3.6-plus \
claude
```

## Supported models

- `qwen3.6-plus`
- `qwen3.5-plus`
- `minimax-m2.7`
- `minimax-m2.5`
