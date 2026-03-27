import { useCallback, useEffect, useRef, useState } from "react";

export interface Message {
  id: number;
  content: string;
  direction: "inbound" | "outbound";
  createdAt: string;
}

export interface WorkspaceInfo {
  id: number;
  name: string;
}

interface ServerMessage {
  type: "status" | "response" | "typing" | "error";
  content?: string;
  workspace?: string;
  message_id?: number;
  message?: string;
  workspaces?: WorkspaceInfo[];
  active_workspace?: string;
  messages?: Array<{
    id: number;
    content: string;
    direction: "inbound" | "outbound";
    created_at: string;
  }>;
}

export interface UseWebSocketReturn {
  messages: Message[];
  workspaces: WorkspaceInfo[];
  activeWorkspace: string;
  isConnected: boolean;
  isReconnecting: boolean;
  isTyping: boolean;
  error: string | null;
  sendMessage: (content: string) => void;
  switchWorkspace: (workspace: string) => void;
}

const BASE_DELAY = 1000;
const MAX_DELAY = 30000;

export function useWebSocket(): UseWebSocketReturn {
  const [messages, setMessages] = useState<Message[]>([]);
  const [workspaces, setWorkspaces] = useState<WorkspaceInfo[]>([]);
  const [activeWorkspace, setActiveWorkspace] = useState("");
  const [isConnected, setIsConnected] = useState(false);
  const [isReconnecting, setIsReconnecting] = useState(false);
  const [isTyping, setIsTyping] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const retryCount = useRef(0);
  const retryTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const connect = useCallback(() => {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/ws`;

    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => {
      setIsConnected(true);
      setIsReconnecting(false);
      setError(null);
      retryCount.current = 0;
    };

    ws.onclose = () => {
      setIsConnected(false);
      setIsTyping(false);
      wsRef.current = null;

      // Auto-reconnect with exponential backoff.
      const delay = Math.min(BASE_DELAY * 2 ** retryCount.current, MAX_DELAY);
      retryCount.current++;
      setIsReconnecting(true);
      retryTimer.current = setTimeout(connect, delay);
    };

    ws.onerror = () => {
      setError("Connection error");
    };

    ws.onmessage = (event) => {
      const msg: ServerMessage = JSON.parse(event.data);

      switch (msg.type) {
        case "status":
          if (msg.workspaces) setWorkspaces(msg.workspaces);
          if (msg.active_workspace !== undefined)
            setActiveWorkspace(msg.active_workspace);
          if (msg.messages) {
            setMessages(
              msg.messages.map((m) => ({
                id: m.id,
                content: m.content,
                direction: m.direction,
                createdAt: m.created_at,
              })),
            );
          } else {
            setMessages([]);
          }
          setIsTyping(false);
          break;

        case "response":
          setIsTyping(false);
          setMessages((prev) => [
            ...prev,
            {
              id: msg.message_id ?? Date.now(),
              content: msg.content ?? "",
              direction: "outbound",
              createdAt: new Date().toISOString(),
            },
          ]);
          break;

        case "typing":
          setIsTyping(true);
          break;

        case "error":
          setError(msg.message ?? "Unknown error");
          break;
      }
    };
  }, []);

  useEffect(() => {
    connect();
    return () => {
      if (retryTimer.current) clearTimeout(retryTimer.current);
      if (wsRef.current) {
        wsRef.current.onclose = null; // Prevent reconnect on unmount.
        wsRef.current.close();
      }
    };
  }, [connect]);

  const sendMessage = useCallback((content: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(
        JSON.stringify({ type: "message", content }),
      );
      // Optimistically add the outbound message.
      setMessages((prev) => [
        ...prev,
        {
          id: Date.now(),
          content,
          direction: "inbound" as const,
          createdAt: new Date().toISOString(),
        },
      ]);
    }
  }, []);

  const switchWorkspace = useCallback((workspace: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(
        JSON.stringify({ type: "switch_workspace", workspace }),
      );
    }
  }, []);

  return {
    messages,
    workspaces,
    activeWorkspace,
    isConnected,
    isReconnecting,
    isTyping,
    error,
    sendMessage,
    switchWorkspace,
  };
}
