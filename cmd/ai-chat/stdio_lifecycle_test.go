package main

import "testing"

func TestShutdownStdioBackgroundStopsTelegramBeforeWait(t *testing.T) {
	var stopped bool
	cancelled := make(chan struct{})
	shutdownStdioBackground(func() {
		close(cancelled)
	}, waitRecorder{
		wait: func() {
			if !stopped {
				t.Fatal("expected telegram adapter to stop before wait")
			}
			select {
			case <-cancelled:
			default:
				t.Fatal("expected cancel before wait")
			}
		},
	}, stopRecorder{stop: func() { stopped = true }})
}

type stopRecorder struct {
	stop func()
}

func (s stopRecorder) Stop() error {
	s.stop()
	return nil
}
