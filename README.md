# ai-chat

A local-first AI chat system that routes messages from Telegram and a browser UI to AI agents (Claude, OpenCode, Copilot) running in tmux sessions on your machine.

It keeps everything on your hardware: SQLite for state, JSONL for audit logs, and direct tmux sessions for agent interaction.

## Quick Start

**Prerequisites:** Go 1.23+, Node.js 20+, tmux

1. Clone and build:
   ```bash
   git clone <repo-url> && cd ai-chat
   ./scripts/build.sh
   ```

2. Create a config file (`config.json`):
   ```json
   {
     "telegram": {
       "bot_token": "your-bot-token",
       "allowed_users": [123456789]
     },
     "openrouter": {
       "api_key": "your-openrouter-api-key"
     },
     "db_path": "~/.ai-chat/state.db",
     "log_dir": "~/.ai-chat/logs/",
     "http_addr": "127.0.0.1:8080"
   }
   ```

   Get the bot token from [@BotFather](https://t.me/BotFather), your user ID from [@userinfobot](https://t.me/userinfobot), and an API key from [OpenRouter](https://openrouter.ai/keys).

3. Run:
   ```bash
   ./ai-chat start -config config.json
   ```

   The web UI is at `http://127.0.0.1:8080`. Your Telegram bot is live.

## How It Works

Messages flow through four layers:

```
Channels ──> Orchestrator ──> Executor ──> Agents
(Telegram,    (routes to       (manages      (Claude, OpenCode,
 Browser)      workspace)       tmux)         Copilot)
```

**Channels** receive messages from Telegram (long polling) or the browser (WebSocket) and normalize them into a common format.

**Orchestrator** decides which workspace and agent should handle a message. It uses OpenRouter for intent classification and maintains per-user context so conversations resume where you left off.

**Executor** manages the agent lifecycle. Each agent runs in its own tmux session tied to a workspace directory. The executor spawns sessions on demand, captures output, detects crashes, and handles reconnection.

**Agents** are the AI tools doing the actual work. Each has a harness that knows how to start it, send input, and read output from tmux. Claude runs via `claude -p`, OpenCode via `opencode`, and Copilot via GitHub Copilot CLI.

### Workspaces

A workspace is a project directory on your machine. You can create multiple workspaces and switch between them mid-conversation. Each workspace gets its own agent sessions, so context stays isolated.

### MCP Server

ai-chat can also run as an [MCP](https://modelcontextprotocol.io/) server for integration with other tools:

```bash
./ai-chat stdio
```

This exposes tools for managing workspaces, sessions, health checks, and model configuration over stdio.

### Audit

All activity is logged to daily-rotated JSONL files. Check for anomalies or review usage:

```bash
./ai-chat audit check -days 1
./ai-chat audit usage -workspace myproject -days 7
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
