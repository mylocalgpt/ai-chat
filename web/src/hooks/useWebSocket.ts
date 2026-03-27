import { useCallback, useEffect, useRef, useState } from "react";

export interface Message {
  id: number;
  content: string;
  direction: "inbound" | "outbound";
  createdAt: string;
  pending?: boolean;
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
  isOffline: boolean;
  hasLoaded: boolean;
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
  const [isOffline, setIsOffline] = useState(!navigator.onLine);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const retryCount = useRef(0);
  const retryTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const lastMessageTime = useRef<number>(0);
  const pendingQueue = useRef<string[]>([]);

  // Offline detection.
  useEffect(() => {
    const onOnline = () => setIsOffline(false);
    const onOffline = () => setIsOffline(true);
    window.addEventListener("online", onOnline);
    window.addEventListener("offline", onOffline);
    return () => {
      window.removeEventListener("online", onOnline);
      window.removeEventListener("offline", onOffline);
    };
  }, []);

  const connect = useCallback(() => {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    let url = `${protocol}//${window.location.host}/ws`;

    // On reconnect, include since parameter to replay missed messages.
    if (lastMessageTime.current > 0) {
      url += `?since=${lastMessageTime.current}`;
    }

    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => {
      setIsConnected(true);
      setIsReconnecting(false);
      setError(null);
      retryCount.current = 0;

      // Resend queued messages.
      const queued = pendingQueue.current.splice(0);
      for (const content of queued) {
        ws.send(JSON.stringify({ type: "message", content }));
      }
      // Clear pending flag on queued messages.
      if (queued.length > 0) {
        setMessages((prev) =>
          prev.map((m) => (m.pending ? { ...m, pending: false } : m)),
        );
      }
    };

    ws.onclose = () => {
      setIsConnected(false);
      setIsTyping(false);
      wsRef.current = null;

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
        case "status": {
          if (msg.workspaces) setWorkspaces(msg.workspaces);
          if (msg.active_workspace !== undefined)
            setActiveWorkspace(msg.active_workspace);

          const serverMsgs = (msg.messages ?? []).map((m) => ({
            id: m.id,
            content: m.content,
            direction: m.direction,
            createdAt: m.created_at,
          }));

          if (lastMessageTime.current > 0 && serverMsgs.length > 0) {
            // Reconnect: merge with local messages, deduplicate by ID.
            setMessages((prev) => {
              const idSet = new Set(prev.map((m) => m.id));
              const newMsgs = serverMsgs.filter((m) => !idSet.has(m.id));
              return [...prev, ...newMsgs];
            });
          } else {
            setMessages(serverMsgs);
          }

          // Track latest message timestamp.
          if (serverMsgs.length > 0) {
            const latest = serverMsgs[serverMsgs.length - 1];
            lastMessageTime.current = new Date(latest.createdAt).getTime();
          }

          setHasLoaded(true);
          setIsTyping(false);
          break;
        }

        case "response":
          setIsTyping(false);
          lastMessageTime.current = Date.now();
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
        wsRef.current.onclose = null;
        wsRef.current.close();
      }
    };
  }, [connect]);

  const sendMessage = useCallback((content: string) => {
    const now = Date.now();
    lastMessageTime.current = now;

    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: "message", content }));
      setMessages((prev) => [
        ...prev,
        {
          id: now,
          content,
          direction: "inbound" as const,
          createdAt: new Date().toISOString(),
        },
      ]);
    } else {
      // Queue for resend on reconnect.
      pendingQueue.current.push(content);
      setMessages((prev) => [
        ...prev,
        {
          id: now,
          content,
          direction: "inbound" as const,
          createdAt: new Date().toISOString(),
          pending: true,
        },
      ]);
    }
  }, []);

  const switchWorkspace = useCallback((workspace: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      // Reset since on workspace switch to get full history.
      lastMessageTime.current = 0;
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
    isOffline,
    hasLoaded,
    error,
    sendMessage,
    switchWorkspace,
  };
}
