import { useCallback } from "react";
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
    error: wsError,
    sendMessage,
    switchWorkspace,
  } = useWebSocket();

  const handleSpeechEnd = useCallback(
    (transcript: string) => {
      if (transcript.trim() && isConnected) {
        // Brief delay so the user can see the final transcript.
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

  const displayError = voiceError || wsError;

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

      <MessageList messages={messages} isTyping={isTyping} />

      <InputBar
        isConnected={isConnected}
        isListening={voiceListening}
        voiceTranscript={voiceTranscript}
        onSend={sendMessage}
      />
    </div>
  );
}

export default App;
