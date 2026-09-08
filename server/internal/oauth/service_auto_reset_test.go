package oauth

import (
	"testing"
	"time"
)

func TestOAuthWeeklyResetTransition(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	oldReset := now.Add(-time.Hour)
	futureReset := now.Add(time.Hour)

	tests := []struct {
		name              string
		previous, current weeklyQuotaSnapshot
		want              bool
	}{
		{name: "first sync", current: weeklyQuotaSnapshot{resetAt: futureReset, resetAtOK: true}},
		{name: "due reset moved forward", previous: weeklyQuotaSnapshot{resetAt: oldReset, resetAtOK: true}, current: weeklyQuotaSnapshot{resetAt: futureReset, resetAtOK: true}, want: true},
		{name: "early recovery moved reset and increased remaining", previous: weeklyQuotaSnapshot{resetAt: futureReset, remaining: 1, resetAtOK: true, remainingPercentOK: true}, current: weeklyQuotaSnapshot{resetAt: futureReset.AddDate(0, 0, 7), remaining: 100, resetAtOK: true, remainingPercentOK: true}, want: true},
		{name: "future reset drift without recovery", previous: weeklyQuotaSnapshot{resetAt: futureReset, remaining: 50, resetAtOK: true, remainingPercentOK: true}, current: weeklyQuotaSnapshot{resetAt: futureReset.Add(time.Minute), remaining: 50, resetAtOK: true, remainingPercentOK: true}},
		{name: "same reset is idempotent", previous: weeklyQuotaSnapshot{resetAt: oldReset, resetAtOK: true}, current: weeklyQuotaSnapshot{resetAt: oldReset, remaining: 100, resetAtOK: true, remainingPercentOK: true}},
		{name: "invalid previous reset", current: weeklyQuotaSnapshot{resetAt: futureReset, resetAtOK: true}},
		{name: "invalid current reset", previous: weeklyQuotaSnapshot{resetAt: oldReset, resetAtOK: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := weeklyResetOccurred(test.previous, test.current, now); got != test.want {
				t.Fatalf("weeklyResetOccurred() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOAuthQuotaResetAtParsesUnixSecondsAndRFC3339(t *testing.T) {
	want := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		value any
		valid bool
	}{
		{name: "unix seconds", value: want.Unix(), valid: true},
		{name: "rfc3339 string", value: want.Format(time.RFC3339), valid: true},
		{name: "invalid string", value: "not-a-time"},
		{name: "zero", value: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := oauthQuotaResetAt(test.value)
			if ok != test.valid || (ok && !got.Equal(want)) {
				t.Fatalf("oauthQuotaResetAt(%#v) = %s, %v; want %s, %v", test.value, got, ok, want, test.valid)
			}
		})
	}
}
