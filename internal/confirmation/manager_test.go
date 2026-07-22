package confirmation

import (
	"testing"
	"time"
)

func TestTokenIsOneTimeAndScoped(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	manager := New(Config{TTL: time.Minute, Now: func() time.Time { return now }})
	token, err := manager.Issue("terminate_workflow", "argo-ci", "build-123")
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	if manager.Consume(token, "terminate_workflow", "other", "build-123") {
		t.Fatal("token must not authorize another namespace")
	}
	if !manager.Consume(token, "terminate_workflow", "argo-ci", "build-123") {
		t.Fatal("expected correctly scoped token to be accepted")
	}
	if manager.Consume(token, "terminate_workflow", "argo-ci", "build-123") {
		t.Fatal("token must only be accepted once")
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	manager := New(Config{TTL: time.Minute, Now: func() time.Time { return now }})
	token, err := manager.Issue("terminate_workflow", "argo-ci", "build-123")
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	now = now.Add(2 * time.Minute)

	if manager.Consume(token, "terminate_workflow", "argo-ci", "build-123") {
		t.Fatal("expired token must be rejected")
	}
}
