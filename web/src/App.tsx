import { useCallback, useEffect, useRef } from "react";
import { Header } from "./components/Header";
import { MessageList } from "./components/MessageList";
import { InputBar } from "./components/InputBar";
import { useWebSocket } from "./hooks/useWebSocket";
import { useSpeechRecognition } from "./hooks/useSpeechRecognition";

function App() {
  const {
    messages,
    workspaces,
    activeWorkspace,
    isConnected,
    isReconnecting,
    isTyping,
    isOffline,
    hasLoaded,
    error: wsError,
    sendMessage,
    switchWorkspace,
  } = useWebSocket();

  const handleSpeechEnd = useCallback(
    (transcript: string) => {
      if (transcript.trim() && isConnected) {
        setTimeout(() => {
          sendMessage(transcript.trim());
        }, 200);
      }
    },
    [isConnected, sendMessage],
  );

  const {
    isSupported: voiceSupported,
    isListening: voiceListening,
    transcript: voiceTranscript,
    error: voiceError,
    startListening,
    stopListening,
  } = useSpeechRecognition(handleSpeechEnd);

  const handleVoiceToggle = useCallback(() => {
    if (voiceListening) {
      stopListening();
    } else {
      startListening();
    }
  }, [voiceListening, startListening, stopListening]);

  // Keyboard shortcuts.
  const inputRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Ignore when modifier keys are held.
      if (e.ctrlKey || e.metaKey || e.altKey) return;

      if (e.key === "/" && document.activeElement !== inputRef.current) {
        e.preventDefault();
        inputRef.current?.focus();
      }

      if (e.key === "Escape" && document.activeElement === inputRef.current) {
        inputRef.current?.blur();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, []);

  const displayError = voiceError || wsError;

  // Loading state before first status message.
  if (!hasLoaded) {
    return (
      <div className="flex h-screen items-center justify-center bg-surface-0">
        <div className="text-center">
          <div className="mb-3 inline-block h-5 w-5 animate-spin rounded-full border-2 border-text-muted border-t-accent" />
          <p className="text-sm text-text-muted">Connecting...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-screen flex-col bg-surface-0">
      <Header
        workspaces={workspaces}
        activeWorkspace={activeWorkspace}
        isConnected={isConnected}
        isReconnecting={isReconnecting}
        onSwitchWorkspace={switchWorkspace}
        voiceSupported={voiceSupported}
        voiceListening={voiceListening}
        onVoiceToggle={handleVoiceToggle}
      />

      {displayError && (
        <div className="shrink-0 border-b border-error/20 bg-error/10 px-3 py-1.5 text-xs text-error">
          {displayError}
        </div>
      )}

      <MessageList
        messages={messages}
        isTyping={isTyping}
        isOffline={isOffline}
      />

      <InputBar
        ref={inputRef}
        isConnected={isConnected && !isOffline}
        isListening={voiceListening}
        voiceTranscript={voiceTranscript}
        onSend={sendMessage}
      />
    </div>
  );
}

export default App;
