# ai-chat

Chat with & control AI Agents via telegram, chat UI & voice.

> **Note:** Claude Code will not be supported (first class support for OpenCode, Copilot, Codex intended via CLI)

## Running

- Start the Telegram bot with `go run ./cmd/ai-chat start --config ./config.json`
- Run the in-process regression suite with `go run ./cmd/ai-chat test`
- Run one named regression with `go run ./cmd/ai-chat test --scenario "Session switching regression"`
- Run the explicit Telegram acceptance path with `go run ./cmd/ai-chat test --scenario telegram-acceptance --config ./config.json`

## Validation

- Final refactor validation is `go test ./...`
- Then run `./scripts/pre-push`
- Then run one explicit `telegram-acceptance` pass against a real Telegram bot/chat configuration

## Telegram Acceptance

- `telegram-acceptance` is opt-in and is not part of normal `go test` or default `ai-chat test` runs
- By default it uses `telegram.bot_token`
- You can override the bot with `telegram.acceptance_bot_token`
- You should set `telegram.acceptance_chat_id` to the real chat to validate
- If you omit `telegram.acceptance_chat_id`, the command only falls back when `telegram.allowed_users` contains exactly one direct-chat user ID

## Runtime Notes

- The in-process test harness now boots the same store, router, session manager, watcher, and outbound response flow used by `start`
- Telegram remains a transport/rendering adapter on top of router and session-manager behavior
