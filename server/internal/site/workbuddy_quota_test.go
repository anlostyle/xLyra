package site

import (
	"database/sql"
	"testing"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestApplyWorkBuddySummaryToState(t *testing.T) {
	summary := adapter.APIKeySummary{
		Usage: map[string]any{"tier": "personal", "limit": int64(2000), "remaining": int64(1999), "used": int64(1), "unlimited": false, "unit": "credits"},
		Raw:   map[string]any{"model_count": 18},
	}
	params := store.UpsertSiteAPIKeyStateParams{}
	applyWorkBuddySummaryToState(&params, summary, "188****9521")
	if params.RemainQuota != int64(1999) || params.UsedQuota != int64(1) || params.GroupName != "personal" || params.Name != "188****9521" {
		t.Fatalf("unexpected params: %+v", params)
	}
	if len(params.Usage) == 0 || len(params.Raw) == 0 {
		t.Fatalf("usage/raw must be persisted: %+v", params)
	}
}

func TestKeepWorkBuddyStateQuotaSurvivesFailedRefresh(t *testing.T) {
	previous := store.SiteAPIKeyState{
		Name:        "acct",
		GroupName:   sql.NullString{String: "personal", Valid: true},
		RemainQuota: sql.NullInt64{Int64: 1999, Valid: true},
		UsedQuota:   sql.NullInt64{Int64: 1, Valid: true},
		Usage:       store.JSON(`{"limit":2000}`),
		Raw:         store.JSON(`{"a":1}`),
	}
	params := store.UpsertSiteAPIKeyStateParams{SyncStatus: "stale"}
	keepWorkBuddyStateQuota(&params, previous)
	if params.RemainQuota != int64(1999) || params.UsedQuota != int64(1) || params.GroupName != "personal" || string(params.Usage) != `{"limit":2000}` || params.SyncStatus != "stale" {
		t.Fatalf("quota must be carried over a failed refresh: %+v", params)
	}
}
