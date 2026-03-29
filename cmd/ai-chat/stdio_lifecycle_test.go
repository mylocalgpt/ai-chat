package main

import "testing"

func TestShutdownStdioBackgroundCancelsBeforeWaitLifecycle(t *testing.T) {
	cancelled := make(chan struct{})
	shutdownStdioBackground(func() {
		close(cancelled)
	}, waitRecorder{
		wait: func() {
			select {
			case <-cancelled:
			default:
				t.Fatal("expected cancel before wait")
			}
		},
	})
}
