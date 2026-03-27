import type { WorkspaceInfo } from "../hooks/useWebSocket";

interface HeaderProps {
  workspaces: WorkspaceInfo[];
  activeWorkspace: string;
  isConnected: boolean;
  isReconnecting: boolean;
  onSwitchWorkspace: (workspace: string) => void;
}

export function Header({
  workspaces,
  activeWorkspace,
  isConnected,
  isReconnecting,
  onSwitchWorkspace,
}: HeaderProps) {
  const statusColor = isConnected
    ? "bg-success"
    : isReconnecting
      ? "bg-warning"
      : "bg-error";

  const statusLabel = isConnected
    ? "Connected"
    : isReconnecting
      ? "Reconnecting"
      : "Disconnected";

  return (
    <header className="flex h-12 shrink-0 items-center justify-between border-b border-border px-3 sm:px-4">
      <div className="flex items-center gap-3">
        {workspaces.length > 1 ? (
          <select
            value={activeWorkspace}
            onChange={(e) => onSwitchWorkspace(e.target.value)}
            className="rounded bg-surface-2 px-2 py-1 text-sm text-text-primary outline-none focus:ring-1 focus:ring-accent"
          >
            {workspaces.map((ws) => (
              <option key={ws.id} value={ws.name}>
                {ws.name}
              </option>
            ))}
          </select>
        ) : (
          <span className="text-sm font-medium text-text-primary">
            {activeWorkspace || "ai-chat"}
          </span>
        )}
      </div>

      <div className="flex items-center gap-3">
        {/* Voice button placeholder - wired in Phase 4 */}
        <button
          disabled
          className="flex h-8 w-8 items-center justify-center rounded text-text-muted opacity-40"
          title="Voice input (coming soon)"
          aria-label="Voice input"
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
            <rect x="5" y="1" width="6" height="9" rx="3" />
            <path d="M3 7a5 5 0 0 0 10 0" />
            <line x1="8" y1="12" x2="8" y2="15" />
            <line x1="5.5" y1="15" x2="10.5" y2="15" />
          </svg>
        </button>

        <div className="flex items-center gap-1.5" title={statusLabel}>
          <span className={`inline-block h-2 w-2 rounded-full ${statusColor}`} />
          <span className="hidden text-xs text-text-muted sm:inline">
            {statusLabel}
          </span>
        </div>
      </div>
    </header>
  );
}
