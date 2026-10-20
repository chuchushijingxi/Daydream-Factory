package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	u "drama-agent/internal/application/modelusage"
	"drama-agent/internal/domain/identity"
)

func TestUsageMediaProjectionPaginationAndUnknownCosts(t *testing.T) {
	store, conn := identityDB(t)
	ctx := context.Background()
	owner := identity.New("usage-owner")
	email := owner + "@example.invalid"
	_, err := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,'同名创作者','user','fixture'),($3,$4,'同名创作者','user','fixture')`, owner, email, identity.New("unrelated-owner"), identity.New("unrelated-email")+"@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	taskID := identity.New("usage-media")
	requestID, submissionID := identity.New("usage-request"), identity.New("usage-submission")
	at := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	payload := map[string]any{"operation": "video.generate", "modelId": "fixture-video", "binding": map[string]any{"connectionId": "fixture-connection"}, "endedAt": at.Add(10 * time.Second), "private": "PRIVATE_TASK_BODY_SENTINEL", "result": map[string]any{"files": []any{map[string]any{"contentType": "image/png", "objectKey": "PRIVATE_OBJECT"}, map[string]any{"contentType": "video/mp4", "durationSeconds": 2.5, "objectKey": "PRIVATE_OBJECT"}}}, "submissions": []any{
		map[string]any{"id": submissionID, "startedAt": at.Add(time.Second), "disposition": "accepted", "receipt": map[string]any{"remote": map[string]any{"id": requestID}, "billing": map[string]any{"status": "known", "knownMicroCny": 123}, "body": "PRIVATE_PROVIDER_BODY"}},
		map[string]any{"id": "unknown-submission", "startedAt": at.Add(2 * time.Second), "disposition": "may_have_sent", "receipt": map[string]any{"billing": map[string]any{"status": "unknown"}, "errorCode": "PRIVATE_PROVIDER_ERROR"}},
		map[string]any{"id": "not-sent-submission", "startedAt": at, "disposition": "not_sent", "receipt": map[string]any{"billing": map[string]any{"status": "known", "knownMicroCny": 999}}},
	}}
	raw, _ := json.Marshal(payload)
	_, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.tasks(id,scope_id,command_id,status,revision,input_snapshot,payload,created_at,owner_id) VALUES($1,$1,$1,'succeeded',1,'{}',$2,$3,$4)`, taskID, raw, at, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"tasks", "calls"} {
		filter := u.Filter{View: view, UserID: owner, Sort: "asc", Page: 1, PageSize: 1}
		result, err := store.ModelUsage(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		if result.Stats.KnownCostMicroCNY != 123 || !result.Stats.UnknownCost || result.Stats.CallCount != 2 || len(result.Items) != 1 {
			t.Fatal("media statistics changed", result.Stats, err)
		}
		item := result.Items[0]
		if item.UserID != owner || item.Username == nil || *item.Username != "同名创作者" || item.Email == nil || *item.Email != email {
			t.Fatal("account display metadata was not joined by stable user ID")
		}
		if view == "tasks" {
			if result.Total != 1 || result.Items[0].DurationMS == nil || *result.Items[0].DurationMS != 10000 {
				t.Fatal("task grouping or duration")
			}
			var media struct {
				ImageCount   int     `json:"imageCount"`
				VideoSeconds float64 `json:"videoSeconds"`
			}
			if json.Unmarshal(result.Items[0].MediaUsage, &media) != nil || media.ImageCount != 1 || media.VideoSeconds != 2.5 {
				t.Fatal("actual media units changed")
			}
		} else {
			if result.Total != 2 || result.Items[0].RequestID == nil || *result.Items[0].RequestID != requestID || result.Items[0].InputTokens != nil {
				t.Fatal("call projection")
			}
			filter.Page = 2
			second, err := store.ModelUsage(ctx, filter)
			if err != nil || second.Total != result.Total || second.Stats != result.Stats || len(second.Items) != 1 || !second.Items[0].UnknownCost || second.Items[0].ErrorCode == nil || *second.Items[0].ErrorCode != "MODEL_CALL_FAILED" {
				t.Fatal("call page/unknown evidence", err)
			}
			filter.RequestID = requestID
			filter.Page = 1
			match, err := store.ModelUsage(ctx, filter)
			if err != nil || match.Total != 1 || match.Stats.UnknownCost {
				t.Fatal("request filter", err)
			}
			filter.RequestID = ""
		}
		encoded, _ := json.Marshal(result)
		for _, private := range []string{"PRIVATE_", "objectKey", "input_snapshot", "password"} {
			if strings.Contains(string(encoded), private) {
				t.Fatal("projection leaked body or credentials")
			}
		}
		filter.Page = 100
		empty, err := store.ModelUsage(ctx, filter)
		if err != nil || len(empty.Items) != 0 || empty.Stats != result.Stats || empty.Total != result.Total {
			t.Fatal("out-of-range page lost stats", err)
		}
		filter.UserID = "absent-" + owner
		zero, err := store.ModelUsage(ctx, filter)
		if err != nil || zero.Total != 0 || len(zero.Items) != 0 || zero.Stats.UnknownCost || zero.Stats.KnownCostMicroCNY != 0 {
			t.Fatal("empty filter stats", err)
		}
	}
	for _, view := range []string{"tasks", "calls"} {
		for _, search := range []string{owner, taskID, requestID, submissionID} {
			filter := u.Filter{View: view, Search: search, Sort: "asc", Page: 1, PageSize: 1}
			result, err := store.ModelUsage(ctx, filter)
			want := int64(1)
			if view == "calls" && (search == owner || search == taskID) {
				want = 2
			}
			if err != nil || result.Total != want || result.Stats.Count != want || len(result.Items) != 1 || result.Items[0].TaskID != taskID || result.Stats.KnownCostMicroCNY != 123 {
				t.Fatalf("unified %s search %q: total/stats/items disagree: %+v, %v", view, search, result, err)
			}
			if view == "tasks" && (result.Stats.CallCount != 2 || !result.Stats.UnknownCost) {
				t.Fatal("request lookup hid the other calls or their unknown cost")
			}
			filter.Page = 3
			empty, err := store.ModelUsage(ctx, filter)
			if err != nil || len(empty.Items) != 0 || empty.Stats != result.Stats || empty.Total != result.Total {
				t.Fatal("unified search pagination changed full-filter statistics", err)
			}
			filter.ModelID = "unrelated-model"
			zero, err := store.ModelUsage(ctx, filter)
			if err != nil || zero.Total != 0 || zero.Stats.CallCount != 0 {
				t.Fatal("unified search bypassed another filter", err)
			}
		}
		zero, err := store.ModelUsage(ctx, u.Filter{View: view, Search: "' OR 1=1 --", Sort: "asc", Page: 1, PageSize: 20})
		if err != nil || zero.Total != 0 {
			t.Fatal("unified search was interpreted as SQL", err)
		}
	}
	orphanID, missingOwner := identity.New("orphan-media"), identity.New("missing-owner")
	_, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.tasks(id,scope_id,command_id,status,revision,input_snapshot,payload,created_at,owner_id) VALUES($1,$1,$1,'succeeded',1,'{}',$2,$3,$4)`, orphanID, raw, at, missingOwner)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"tasks", "calls"} {
		result, err := store.ModelUsage(ctx, u.Filter{View: view, UserID: missingOwner, Sort: "asc", Page: 1, PageSize: 20})
		if err != nil || len(result.Items) == 0 {
			t.Fatal("missing account dropped historical records", err)
		}
		for _, item := range result.Items {
			if item.UserID != missingOwner || item.Username != nil || item.Email != nil {
				t.Fatal("missing account metadata was invented")
			}
		}
	}
	t.Log("Real PG media call grouping, unsent exclusion, nullable usage, precise media facts, unknown costs, pagination/filter statistics and privacy verified")
}
