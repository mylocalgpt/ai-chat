import { useState } from "react";
import { useWebSocket } from "./hooks/useWebSocket";

function App() {
  const {
    messages,
    workspaces,
    activeWorkspace,
    isConnected,
    isReconnecting,
    isTyping,
    error,
    sendMessage,
    switchWorkspace,
  } = useWebSocket();

  const [input, setInput] = useState("");

  const handleSend = () => {
    const trimmed = input.trim();
    if (trimmed) {
      sendMessage(trimmed);
      setInput("");
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="flex min-h-screen flex-col bg-neutral-950 p-4 text-neutral-100">
      <div className="mb-4 flex items-center gap-4">
        <h1 className="text-xl font-semibold">ai-chat</h1>
        <span
          className={`text-sm ${isConnected ? "text-green-400" : "text-red-400"}`}
        >
          {isConnected
            ? "connected"
            : isReconnecting
              ? "reconnecting..."
              : "disconnected"}
        </span>
      </div>

      {error && (
        <div className="mb-2 rounded bg-red-900/50 px-3 py-1 text-sm text-red-200">
          {error}
        </div>
      )}

      {workspaces.length > 0 && (
        <div className="mb-4 flex gap-2">
          {workspaces.map((ws) => (
            <button
              key={ws.id}
              onClick={() => switchWorkspace(ws.name)}
              className={`rounded px-3 py-1 text-sm ${
                ws.name === activeWorkspace
                  ? "bg-blue-600 text-white"
                  : "bg-neutral-800 text-neutral-300 hover:bg-neutral-700"
              }`}
            >
              {ws.name}
            </button>
          ))}
        </div>
      )}

      <div className="mb-4 flex-1 space-y-2 overflow-y-auto">
        {messages.map((msg) => (
          <div
            key={msg.id}
            className={`rounded px-3 py-2 ${
              msg.direction === "inbound"
                ? "bg-neutral-800"
                : "bg-blue-900/50"
            }`}
          >
            <span className="text-xs text-neutral-500">
              {msg.direction === "inbound" ? "you" : "ai"}
            </span>
            <p className="whitespace-pre-wrap">{msg.content}</p>
          </div>
        ))}
        {isTyping && (
          <div className="text-sm text-neutral-500">typing...</div>
        )}
      </div>

      <div className="flex gap-2">
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Type a message..."
          className="flex-1 rounded bg-neutral-800 px-3 py-2 text-neutral-100 placeholder-neutral-500 outline-none focus:ring-1 focus:ring-blue-500"
        />
        <button
          onClick={handleSend}
          disabled={!isConnected || !input.trim()}
          className="rounded bg-blue-600 px-4 py-2 font-medium text-white disabled:opacity-50"
        >
          Send
        </button>
      </div>
    </div>
  );
}

export default App;
