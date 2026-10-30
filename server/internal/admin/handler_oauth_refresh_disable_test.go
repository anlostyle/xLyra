package admin

import (
	"testing"

	"xlyra/server/internal/site"
)

func TestOAuthRefreshAuthFailureClassificationKeepsRateLimitedSitesEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		provider string
		message  string
		want     bool
	}{
		{
			name:     "claude code usage rate limited",
			provider: "claude_code",
			message:  `all 1 enabled api keys failed: claude code usage returned 429: {   "error": {     "type": "rate_limit_error",     "message": "Rate limited. Please try again later."   } }`,
			want:     false,
		},
		{
			name:     "upstream bad gateway",
			provider: "claude_code",
			message:  "all 1 enabled api keys failed: claude code usage returned 502: bad gateway",
			want:     false,
		},
		{
			name:     "claude code unauthorized",
			provider: "claude_code",
			message:  "all 1 enabled api keys failed: claude code usage returned 401: invalid token",
			want:     true,
		},
		{
			name:     "codex invalid grant",
			provider: "codex",
			message:  "refresh failed: invalid_grant",
			want:     true,
		},
	}
	for _, tc := range cases {
		if got := site.OAuthPermanentAuthRefreshError(tc.provider, tc.message); got != tc.want {
			t.Fatalf("%s: OAuthPermanentAuthRefreshError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMarkOAuthConnectionUnavailableOnRefreshAuthFailureWithoutOAuthServiceIsNoop(t *testing.T) {
	t.Parallel()

	(Handler{}).markOAuthConnectionUnavailableOnRefreshAuthFailure(t.Context(), [16]byte{}, "claude_code", assertionError("claude code usage returned 401"))
}
