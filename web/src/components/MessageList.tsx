import Markdown from "react-markdown";
import type { Message } from "../hooks/useWebSocket";
import { useAutoScroll } from "../hooks/useAutoScroll";
import { TypingIndicator } from "./TypingIndicator";

interface MessageListProps {
  messages: Message[];
  isTyping: boolean;
  isOffline: boolean;
}

function formatTime(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  } catch {
    return "";
  }
}

function MessageBubble({ msg }: { msg: Message }) {
  const isUser = msg.direction === "inbound";

  return (
    <div className={`flex ${isUser ? "justify-end" : "justify-start"}`}>
      <div
        className={`max-w-[85%] sm:max-w-[75%] rounded-lg px-3 py-2 ${
          isUser
            ? "bg-msg-user text-text-primary"
            : "bg-msg-agent text-text-primary"
        } ${msg.pending ? "opacity-50" : ""}`}
      >
        {isUser ? (
          <p className="whitespace-pre-wrap text-sm">{msg.content}</p>
        ) : (
          <div className="prose-agent text-sm">
            <Markdown>{msg.content}</Markdown>
          </div>
        )}
        <div
          className={`mt-1 flex items-center gap-1 text-[10px] text-text-muted ${
            isUser ? "justify-end" : "justify-start"
          }`}
        >
          {msg.pending && (
            <svg
              width="10"
              height="10"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
            >
              <circle cx="8" cy="8" r="7" />
              <path d="M8 4v4l3 2" strokeLinecap="round" />
            </svg>
          )}
          {formatTime(msg.createdAt)}
        </div>
      </div>
    </div>
  );
}

export function MessageList({ messages, isTyping, isOffline }: MessageListProps) {
  const { containerRef, handleScroll } = useAutoScroll([
    messages.length,
    isTyping,
  ]);

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="flex flex-1 flex-col overflow-y-auto px-3 py-3 sm:px-4"
    >
      {isOffline && (
        <div className="mx-auto mb-2 max-w-3xl rounded bg-warning/10 px-3 py-1.5 text-center text-xs text-warning">
          No internet connection
        </div>
      )}

      {messages.length === 0 && !isTyping ? (
        <div className="flex flex-1 items-center justify-center">
          <p className="text-sm text-text-muted">No messages yet</p>
        </div>
      ) : (
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-2">
          {messages.map((msg) => (
            <MessageBubble key={msg.id} msg={msg} />
          ))}
          {isTyping && <TypingIndicator />}
        </div>
      )}
    </div>
  );
}
