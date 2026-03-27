import { useCallback, useRef, useState } from "react";

interface InputBarProps {
  isConnected: boolean;
  onSend: (content: string) => void;
}

const MAX_HEIGHT = 120; // ~4 lines

export function InputBar({ isConnected, onSend }: InputBarProps) {
  const [value, setValue] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, MAX_HEIGHT) + "px";
  }, []);

  const handleSend = useCallback(() => {
    const trimmed = value.trim();
    if (!trimmed || !isConnected) return;
    onSend(trimmed);
    setValue("");
    // Reset height after clearing.
    requestAnimationFrame(() => {
      const el = textareaRef.current;
      if (el) {
        el.style.height = "auto";
      }
    });
  }, [value, isConnected, onSend]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  return (
    <div className="shrink-0 border-t border-border px-3 py-2 sm:px-4 sm:py-3">
      <div className="mx-auto flex max-w-3xl items-end gap-2">
        <textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            adjustHeight();
          }}
          onKeyDown={handleKeyDown}
          placeholder="Send a message..."
          disabled={!isConnected}
          rows={1}
          className="flex-1 resize-none rounded-lg bg-surface-2 px-3 py-2 text-sm text-text-primary placeholder-text-muted outline-none transition-colors focus:ring-1 focus:ring-accent disabled:cursor-not-allowed disabled:opacity-40"
        />
        <button
          onClick={handleSend}
          disabled={!isConnected || !value.trim()}
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent text-surface-0 transition-opacity hover:opacity-90 disabled:opacity-30 disabled:cursor-not-allowed"
          aria-label="Send message"
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 16 16"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M14 2L7 9" />
            <path d="M14 2L9.5 14L7 9L2 6.5L14 2Z" />
          </svg>
        </button>
      </div>
    </div>
  );
}
