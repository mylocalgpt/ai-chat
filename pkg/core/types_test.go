package core

import (
	"context"
	"regexp"
	"testing"
)

func TestNewRequestID(t *testing.T) {
	re := regexp.MustCompile(`^req_[0-9a-f]{8}$`)

	id1 := NewRequestID()
	id2 := NewRequestID()

	if len(id1) != 12 {
		t.Errorf("expected length 12, got %d for %q", len(id1), id1)
	}
	if !re.MatchString(id1) {
		t.Errorf("id %q does not match expected format req_[0-9a-f]{8}", id1)
	}

	if len(id2) != 12 {
		t.Errorf("expected length 12, got %d for %q", len(id2), id2)
	}
	if !re.MatchString(id2) {
		t.Errorf("id %q does not match expected format req_[0-9a-f]{8}", id2)
	}

	if id1 == id2 {
		t.Errorf("successive calls produced identical IDs: %q", id1)
	}
}

func TestWithRequestID_RequestID_roundtrip(t *testing.T) {
	ctx := context.Background()
	id := "req_deadbeef"

	ctx = WithRequestID(ctx, id)
	got := RequestID(ctx)

	if got != id {
		t.Errorf("RequestID() = %q, want %q", got, id)
	}
}

func TestRequestID_bare_context(t *testing.T) {
	got := RequestID(context.Background())
	if got != "" {
		t.Errorf("RequestID(background) = %q, want empty string", got)
	}
}
