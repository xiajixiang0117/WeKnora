package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestEmbedSessionHandleSignVerify(t *testing.T) {
	ch := &types.EmbedChannel{ID: "ch-1", PublishToken: "em_secret_token"}
	const sessionID = "11111111-2222-3333-4444-555555555555"

	sig := SignEmbedSessionHandle(ch, sessionID)
	if sig == "" {
		t.Fatal("expected a non-empty signature")
	}
	if !VerifyEmbedSessionHandle(ch, sessionID, sig) {
		t.Fatal("valid handle should verify")
	}

	// Tampered session id must not verify with the same signature.
	if VerifyEmbedSessionHandle(ch, "00000000-0000-0000-0000-000000000000", sig) {
		t.Fatal("signature must be bound to the session id")
	}

	// A different channel token (e.g. after rotation) invalidates the handle.
	rotated := &types.EmbedChannel{ID: "ch-1", PublishToken: "em_rotated_token"}
	if VerifyEmbedSessionHandle(rotated, sessionID, sig) {
		t.Fatal("rotating the channel token must invalidate old handles")
	}

	// A different channel id must not verify.
	other := &types.EmbedChannel{ID: "ch-2", PublishToken: "em_secret_token"}
	if VerifyEmbedSessionHandle(other, sessionID, sig) {
		t.Fatal("signature must be bound to the channel id")
	}

	// Empty / missing signature is rejected.
	if VerifyEmbedSessionHandle(ch, sessionID, "") {
		t.Fatal("empty signature must be rejected")
	}
	if SignEmbedSessionHandle(nil, sessionID) != "" {
		t.Fatal("nil channel must yield empty signature")
	}
	if SignEmbedSessionHandle(ch, "") != "" {
		t.Fatal("empty session id must yield empty signature")
	}
}

func TestIsEmbedSessionToken(t *testing.T) {
	if !IsEmbedSessionToken("ems_abc123") {
		t.Fatal("expected ems_ prefix to be session token")
	}
	if IsEmbedSessionToken("em_abc123") {
		t.Fatal("publish token must not be treated as session token")
	}
	if IsEmbedSessionToken("") {
		t.Fatal("empty token must not match")
	}
}

func TestIsEmbedPreviewSessionToken(t *testing.T) {
	if !IsEmbedPreviewSessionToken("ems_preview_abc123") {
		t.Fatal("expected ems_preview_ prefix to be preview session token")
	}
	if IsEmbedPreviewSessionToken("ems_abc123") {
		t.Fatal("ordinary session token must not be treated as preview token")
	}
	if IsEmbedPreviewSessionToken("em_abc123") {
		t.Fatal("publish token must not be treated as preview token")
	}
}

func TestIssuePreviewSessionUsesDistinctTokenPrefix(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	svc := &embedChannelService{
		repo: &stubEmbedChannelRepo{ch: &types.EmbedChannel{
			ID:       "ch-preview",
			TenantID: 42,
			Enabled:  true,
		}},
		redis: rdb,
	}
	token, expiresIn, err := svc.IssuePreviewSession(context.Background(), 42, "ch-preview")
	if err != nil {
		t.Fatalf("IssuePreviewSession() error = %v", err)
	}
	if !strings.HasPrefix(token, "ems_preview_") {
		t.Fatalf("preview token = %q, want ems_preview_ prefix", token)
	}
	if expiresIn != int(embedSessionTTL.Seconds()) {
		t.Fatalf("expiresIn = %d, want %d", expiresIn, int(embedSessionTTL.Seconds()))
	}
	resolved, err := svc.ResolveSessionToken(context.Background(), token)
	if err != nil || resolved != "ch-preview" {
		t.Fatalf("ResolveSessionToken() = %q, %v; want ch-preview, nil", resolved, err)
	}
}

func TestIssueSessionTokenWithoutRedis(t *testing.T) {
	svc := &embedChannelService{redis: nil}
	_, _, err := svc.IssueSessionToken(context.Background(), "channel-1")
	if err != ErrEmbedSessionUnavailable {
		t.Fatalf("expected ErrEmbedSessionUnavailable, got %v", err)
	}
}

func TestResolveSessionTokenWithoutRedis(t *testing.T) {
	svc := &embedChannelService{redis: nil}
	_, err := svc.ResolveSessionToken(context.Background(), "ems_test")
	if err != ErrEmbedSessionUnavailable {
		t.Fatalf("expected ErrEmbedSessionUnavailable, got %v", err)
	}
}

func TestResolveSessionTokenRejectsPublishToken(t *testing.T) {
	svc := &embedChannelService{redis: nil}
	_, err := svc.ResolveSessionToken(context.Background(), "em_publish_only")
	if err != ErrEmbedTokenInvalid {
		t.Fatalf("expected ErrEmbedTokenInvalid for publish token, got %v", err)
	}
}
