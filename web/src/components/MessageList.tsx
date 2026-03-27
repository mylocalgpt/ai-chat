import Markdown from "react-markdown";
import type { Message } from "../hooks/useWebSocket";
import { useAutoScroll } from "../hooks/useAutoScroll";
import { TypingIndicator } from "./TypingIndicator";

interface MessageListProps {
  messages: Message[];
  isTyping: boolean;
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
    <div
      className={`flex ${isUser ? "justify-end" : "justify-start"}`}
    >
      <div
        className={`max-w-[85%] sm:max-w-[75%] rounded-lg px-3 py-2 ${
          isUser
            ? "bg-msg-user text-text-primary"
            : "bg-msg-agent text-text-primary"
        }`}
      >
        {isUser ? (
          <p className="whitespace-pre-wrap text-sm">{msg.content}</p>
        ) : (
          <div className="prose-agent text-sm">
            <Markdown>{msg.content}</Markdown>
          </div>
        )}
        <div
          className={`mt-1 text-[10px] text-text-muted ${
            isUser ? "text-right" : "text-left"
          }`}
        >
          {formatTime(msg.createdAt)}
        </div>
      </div>
    </div>
  );
}

export function MessageList({ messages, isTyping }: MessageListProps) {
  const { containerRef, handleScroll } = useAutoScroll([
    messages.length,
    isTyping,
  ]);

  if (messages.length === 0 && !isTyping) {
    return (
      <div
        ref={containerRef}
        className="flex flex-1 items-center justify-center"
      >
        <p className="text-sm text-text-muted">No messages yet</p>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="flex-1 overflow-y-auto px-3 py-3 sm:px-4"
    >
      <div className="mx-auto flex max-w-3xl flex-col gap-2">
        {messages.map((msg) => (
          <MessageBubble key={msg.id} msg={msg} />
        ))}
        {isTyping && <TypingIndicator />}
      </div>
    </div>
  );
}
