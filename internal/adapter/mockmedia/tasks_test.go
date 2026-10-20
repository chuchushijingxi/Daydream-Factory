package mockmedia

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/task"
)

func TestFrozenMockExecutionDelayFailureAndLegacyRecovery(t *testing.T) {
	provider := TaskProvider{}
	ctx := context.Background()
	for _, id := range []string{md.MockImageModelID, md.MockVideoModelID} {
		for _, fail := range []bool{false, true} {
			raw, _ := json.Marshal(md.MockExecution{MockBehavior: md.MockBehavior{DelaySeconds: 2, FailurePercent: 50}, Fail: fail})
			binding := d.Binding{Protocol: md.MockProtocol, RequestModelID: id, ProviderConfig: raw}
			request := json.RawMessage(`{"prompt":"完整提示词 fail 不决定结果","aspectRatio":""}`)
			prepared, err := provider.Prepare(ctx, binding, request)
			if err != nil {
				t.Fatal(err)
			}
			before := time.Now()
			receipt, err := provider.Submit(ctx, binding, "submission_fixture", prepared)
			if err != nil || receipt.Remote == nil || receipt.Remote.ForceFail != fail || receipt.Remote.ReadyAt.Sub(before) < 2*time.Second || receipt.Remote.ReadyAt.Sub(before) > 3*time.Second {
				t.Fatal(receipt, err)
			}
			pending, err := provider.Query(ctx, binding, *receipt.Remote)
			if err != nil || pending.Outcome != "pending" {
				t.Fatal(pending, err)
			}
			// Model configuration and wall-clock sampling are absent from Query.
			remote := *receipt.Remote
			remote.ReadyAt = time.Now().Add(-time.Second)
			for i := 0; i < 3; i++ {
				got, err := provider.Query(ctx, binding, remote)
				want := "completed"
				if fail {
					want = "failed"
				}
				if err != nil || got.Outcome != want {
					t.Fatal(got, err)
				}
			}
			if _, err = provider.Prepare(ctx, binding, json.RawMessage(`{"prompt":"x","aspectRatio":"","delayMs":1,"forceFail":true}`)); err == nil {
				t.Fatal("new frozen task accepted input overrides")
			}
		}
	}
	// Already persisted jobs keep their immutable input. No public management route remains.
	old := d.Binding{Protocol: md.MockProtocol, RequestModelID: md.MockImageModelID}
	prepared, err := provider.Prepare(ctx, old, json.RawMessage(`{"prompt":"old","aspectRatio":"","delayMs":10,"forceFail":true}`))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := provider.Submit(ctx, old, "submission_old", prepared)
	if err != nil || !receipt.Remote.ForceFail {
		t.Fatal(receipt, err)
	}
}
