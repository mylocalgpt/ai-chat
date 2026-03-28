# ai-chat

Chat with AI agents on your machine through Telegram or a browser. Messages are routed to Claude, OpenCode, or Copilot running in tmux sessions, with everything stored locally.

## Quick Start

**Prerequisites:** Node.js 18+, tmux

```bash
npx @mylocalgpt/ai-chat@latest start
```

Create `~/.config/ai-chat/config.json` (or `config.json` in the current directory):

```json
{
  "telegram": {
    "bot_token": "your-bot-token",
    "allowed_users": [123456789]
  },
  "openrouter": {
    "api_key": "your-openrouter-api-key"
  }
}
```

Get the bot token from [@BotFather](https://t.me/BotFather) on Telegram. Find your user ID by messaging your bot and checking `from.id` via the [getUpdates](https://core.telegram.org/bots/api#getupdates) API. Get an API key from [OpenRouter](https://openrouter.ai/keys).

The web UI runs at `http://127.0.0.1:8080`.

## How It Works

You send a message via Telegram or the web UI. An orchestrator classifies it (using a cheap LLM via OpenRouter) and routes it to the right workspace and agent. The agent runs in a tmux session on your machine, and the response is sent back through the same channel.

Workspaces are project directories. Each gets its own agent sessions, so context stays isolated. Switch between them mid-conversation.

## MCP Server

Add to your MCP client config (Claude, Copilot, Cursor, etc.):

```json
{
  "mcpServers": {
    "ai-chat": {
      "command": "npx",
      "args": ["-y", "@mylocalgpt/ai-chat@latest", "stdio"]
    }
  }
}
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
