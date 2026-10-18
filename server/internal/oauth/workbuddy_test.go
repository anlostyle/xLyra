package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func workBuddyTestJWT(claims map[string]any) string {
	payload, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestWorkBuddyLoginFlow(t *testing.T) {
	polls := 0
	token := workBuddyTestJWT(map[string]any{"sub": "user-1", "email": "a@example.com"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"})
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"state": "st-1", "authUrl": "https://login.example/auth?state=st-1"}})
		case "/v2/plugin/auth/token":
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "abc" {
				t.Errorf("poll must reuse the cookie session from the start request")
			}
			if r.URL.Query().Get("state") != "st-1" {
				t.Errorf("unexpected state %q", r.URL.Query().Get("state"))
			}
			polls++
			if polls == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 11217, "msg": "login ing"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"accessToken": token, "refreshToken": "r-1", "expiresIn": 3600, "domain": "tenant.example",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	previous := workBuddyBaseURL
	workBuddyBaseURL = server.URL
	defer func() { workBuddyBaseURL = previous }()

	svc := &Service{httpClient: server.Client()}
	start, err := svc.StartWorkBuddyLogin(context.Background(), uuid.Nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if start.State != "st-1" || start.AuthURL == "" {
		t.Fatalf("unexpected start: %+v", start)
	}
	if _, err := svc.PollWorkBuddyLogin(context.Background(), uuid.Nil, start.State); !errors.Is(err, ErrWorkBuddyAuthorizationPending) {
		t.Fatalf("expected pending, got %v", err)
	}
	tokens, err := svc.PollWorkBuddyLogin(context.Background(), uuid.Nil, start.State)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if tokens.AccessToken != token || tokens.RefreshToken != "r-1" || tokens.Domain != "tenant.example" || tokens.Email != "a@example.com" || tokens.Subject != "user-1" {
		t.Fatalf("unexpected tokens: %+v", tokens)
	}
	if time.Until(tokens.ExpiresAt) < 55*time.Minute {
		t.Fatalf("unexpected expiry: %v", tokens.ExpiresAt)
	}
	if _, err := svc.PollWorkBuddyLogin(context.Background(), uuid.Nil, start.State); err == nil {
		t.Fatal("finished login session must be consumed")
	}
}

func TestWorkBuddyRefresh(t *testing.T) {
	status := http.StatusOK
	body := map[string]any{"code": 0, "data": map[string]any{"accessToken": "new-token", "refreshToken": "r-2", "expiresIn": 7200}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Refresh-Token") != "r-1" {
			t.Errorf("missing refresh token header")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()
	previous := workBuddyBaseURL
	workBuddyBaseURL = server.URL
	defer func() { workBuddyBaseURL = previous }()
	svc := &Service{httpClient: server.Client()}
	current := WorkBuddyCredentialBundle{AccessToken: "old", RefreshToken: "r-1", Domain: "tenant.example"}

	tokens, err := svc.RefreshWorkBuddyTokens(context.Background(), uuid.Nil, current)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if tokens.AccessToken != "new-token" || tokens.RefreshToken != "r-2" || tokens.Domain != "tenant.example" {
		t.Fatalf("unexpected tokens: %+v", tokens)
	}

	status, body = http.StatusUnauthorized, map[string]any{"code": 401, "msg": "invalid refresh token"}
	if _, err := svc.RefreshWorkBuddyTokens(context.Background(), uuid.Nil, current); !errors.Is(err, ErrWorkBuddyReauthorizationRequired) {
		t.Fatalf("expected reauthorization error, got %v", err)
	}
	status = http.StatusBadGateway
	if _, err := svc.RefreshWorkBuddyTokens(context.Background(), uuid.Nil, current); err == nil || errors.Is(err, ErrWorkBuddyReauthorizationRequired) {
		t.Fatalf("5xx must stay a transient error, got %v", err)
	}
}

func TestWorkBuddyBundleFresh(t *testing.T) {
	if !workBuddyBundleFresh(WorkBuddyCredentialBundle{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour).Unix()}) {
		t.Fatal("bundle with an hour left should be fresh")
	}
	if workBuddyBundleFresh(WorkBuddyCredentialBundle{AccessToken: "x", ExpiresAt: time.Now().Add(time.Minute).Unix()}) {
		t.Fatal("bundle inside the refresh lead window must be refreshed")
	}
	if workBuddyBundleFresh(WorkBuddyCredentialBundle{AccessToken: "x"}) {
		t.Fatal("unknown expiry must be refreshed")
	}
}
