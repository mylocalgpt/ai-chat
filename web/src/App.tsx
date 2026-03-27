import { Header } from "./components/Header";
import { MessageList } from "./components/MessageList";
import { InputBar } from "./components/InputBar";
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

  return (
    <div className="flex h-screen flex-col bg-surface-0">
      <Header
        workspaces={workspaces}
        activeWorkspace={activeWorkspace}
        isConnected={isConnected}
        isReconnecting={isReconnecting}
        onSwitchWorkspace={switchWorkspace}
      />

      {error && (
        <div className="shrink-0 border-b border-error/20 bg-error/10 px-3 py-1.5 text-xs text-error">
          {error}
        </div>
      )}

      <MessageList messages={messages} isTyping={isTyping} />

      <InputBar isConnected={isConnected} onSend={sendMessage} />
    </div>
  );
}

export default App;
