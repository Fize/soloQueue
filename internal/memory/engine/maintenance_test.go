package engine

import "github.com/xiaobaitu/soloqueue/internal/testfixtures"
import (
	"context"
	"testing"
)

func TestLegacyCleanupIsConservativeAndReversible(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()
	_, _, _ = engine.Save(ctx, testfixtures.Chinese("maintenance_test_001"), "2026-07-01", "", "2026-07-01")
	_, _, _ = engine.Save(ctx, testfixtures.Chinese("maintenance_test_002"), "2026-07-02", "auto-compact,memory", "2026-07-02")
	_, _, _ = engine.Save(ctx, testfixtures.Chinese("maintenance_test_003"), "2026-07-03", "", "2026-07-03")

	manifest, err := engine.PlanLegacyCleanup(ctx, "/work/soloQueue")
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	for _, decision := range manifest.Decisions {
		counts[decision.Action]++
	}
	if counts[StatusActive] != 1 || counts[StatusArchived] != 1 || counts[StatusQuarantined] != 1 {
		t.Fatalf("unexpected cleanup decisions: %+v", counts)
	}
	if err := engine.ApplyLegacyCleanup(ctx, manifest); err != nil {
		t.Fatal(err)
	}

	report, err := engine.Audit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 3 || report.ByStatus[StatusActive] != 1 {
		t.Fatalf("unexpected audit after cleanup: %+v", report)
	}
}
