package pr

import "testing"

// === ChecksSummary ===

func TestChecksSummary_Empty(t *testing.T) {
	pr := TrackedPR{}
	passing, total := pr.ChecksSummary()
	if passing != 0 || total != 0 {
		t.Errorf("empty checks: got (%d, %d), want (0, 0)", passing, total)
	}
}

func TestChecksSummary_AllPassing(t *testing.T) {
	pr := TrackedPR{
		Checks: []Check{
			{Name: "ci", Conclusion: "SUCCESS"},
			{Name: "lint", Conclusion: "SUCCESS"},
			{Name: "neutral-check", Conclusion: "NEUTRAL"},
		},
	}
	passing, total := pr.ChecksSummary()
	if passing != 3 || total != 3 {
		t.Errorf("all passing: got (%d, %d), want (3, 3)", passing, total)
	}
}

func TestChecksSummary_Mixed(t *testing.T) {
	pr := TrackedPR{
		Checks: []Check{
			{Name: "ci", Conclusion: "SUCCESS"},
			{Name: "lint", Conclusion: "FAILURE"},
			{Name: "neutral", Conclusion: "NEUTRAL"},
			{Name: "pending", Status: "IN_PROGRESS", Conclusion: ""},
		},
	}
	passing, total := pr.ChecksSummary()
	if passing != 2 || total != 4 {
		t.Errorf("mixed: got (%d, %d), want (2, 4)", passing, total)
	}
}

func TestChecksSummary_AllFailing(t *testing.T) {
	pr := TrackedPR{
		Checks: []Check{
			{Name: "ci", Conclusion: "FAILURE"},
			{Name: "lint", Conclusion: "FAILURE"},
		},
	}
	passing, total := pr.ChecksSummary()
	if passing != 0 || total != 2 {
		t.Errorf("all failing: got (%d, %d), want (0, 2)", passing, total)
	}
}

// === HasFailingChecks ===

func TestHasFailingChecks_NoChecks(t *testing.T) {
	pr := TrackedPR{}
	if pr.HasFailingChecks() {
		t.Error("no checks should not have failing checks")
	}
}

func TestHasFailingChecks_AllGreen(t *testing.T) {
	pr := TrackedPR{
		Checks: []Check{
			{Name: "ci", Conclusion: "SUCCESS"},
			{Name: "lint", Conclusion: "NEUTRAL"},
		},
	}
	if pr.HasFailingChecks() {
		t.Error("all green checks should not report failing")
	}
}

func TestHasFailingChecks_OneFailing(t *testing.T) {
	pr := TrackedPR{
		Checks: []Check{
			{Name: "ci", Conclusion: "SUCCESS"},
			{Name: "lint", Conclusion: "FAILURE"},
		},
	}
	if !pr.HasFailingChecks() {
		t.Error("one failing check should report failing")
	}
}

func TestHasFailingChecks_InProgress(t *testing.T) {
	pr := TrackedPR{
		Checks: []Check{
			{Name: "ci", Status: "IN_PROGRESS", Conclusion: ""},
		},
	}
	if pr.HasFailingChecks() {
		t.Error("in-progress checks should not count as failing")
	}
}

// === NeedsAttention ===

func TestNeedsAttention_Failing(t *testing.T) {
	pr := TrackedPR{State: StateChecksFailing}
	if !pr.NeedsAttention() {
		t.Error("failing PR should need attention")
	}
}

func TestNeedsAttention_Running(t *testing.T) {
	pr := TrackedPR{State: StateChecksRunning}
	if pr.NeedsAttention() {
		t.Error("running PR should not need attention")
	}
}

func TestNeedsAttention_Passing(t *testing.T) {
	pr := TrackedPR{State: StateChecksPassing}
	if pr.NeedsAttention() {
		t.Error("passing PR should not need attention")
	}
}

func TestNeedsAttention_Approved(t *testing.T) {
	pr := TrackedPR{State: StateApproved}
	if pr.NeedsAttention() {
		t.Error("approved PR should not need attention")
	}
}

func TestNeedsAttention_Merged(t *testing.T) {
	pr := TrackedPR{State: StateMerged}
	if pr.NeedsAttention() {
		t.Error("merged PR should not need attention")
	}
}

// === ShouldAutoMerge ===

func TestShouldAutoMerge_Off(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PROff,
		Mergeable:     "MERGEABLE",
		Checks:        []Check{{Conclusion: "SUCCESS", Status: "COMPLETED"}},
		Reviews:       []Review{{State: "APPROVED"}},
	}
	if pr.ShouldAutoMerge() {
		t.Error("OFF mode should never auto-merge")
	}
}

func TestShouldAutoMerge_AutoWithApprovalAndGreenChecks(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		Mergeable:     "MERGEABLE",
		MergeMethod:   "squash",
		Checks:        []Check{{Conclusion: "SUCCESS", Status: "COMPLETED"}},
		Reviews:       []Review{{State: "APPROVED"}},
	}
	if !pr.ShouldAutoMerge() {
		t.Error("AUTO with approval + green checks should auto-merge")
	}
}

func TestShouldAutoMerge_AutoWithoutApproval(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		Mergeable:     "MERGEABLE",
		Checks:        []Check{{Conclusion: "SUCCESS", Status: "COMPLETED"}},
		Reviews:       []Review{{State: "COMMENTED"}},
	}
	if pr.ShouldAutoMerge() {
		t.Error("AUTO without approval should not auto-merge")
	}
}

func TestShouldAutoMerge_AutoNoReviews(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		Mergeable:     "MERGEABLE",
		Checks:        []Check{{Conclusion: "SUCCESS", Status: "COMPLETED"}},
	}
	if pr.ShouldAutoMerge() {
		t.Error("AUTO with no reviews should not auto-merge")
	}
}

func TestShouldAutoMerge_YoloWithGreenChecks(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRYolo,
		Mergeable:     "MERGEABLE",
		MergeMethod:   "squash",
		Checks:        []Check{{Conclusion: "SUCCESS", Status: "COMPLETED"}},
		// No reviews — YOLO doesn't need them.
	}
	if !pr.ShouldAutoMerge() {
		t.Error("YOLO with green checks should auto-merge without approval")
	}
}

func TestShouldAutoMerge_YoloWithFailingCheck(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRYolo,
		Mergeable:     "MERGEABLE",
		Checks: []Check{
			{Conclusion: "SUCCESS", Status: "COMPLETED"},
			{Conclusion: "FAILURE", Status: "COMPLETED"},
		},
	}
	if pr.ShouldAutoMerge() {
		t.Error("YOLO with failing check should not auto-merge")
	}
}

func TestShouldAutoMerge_Conflicting(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		Mergeable:     "CONFLICTING",
		Checks:        []Check{{Conclusion: "SUCCESS", Status: "COMPLETED"}},
		Reviews:       []Review{{State: "APPROVED"}},
	}
	if pr.ShouldAutoMerge() {
		t.Error("conflicting PR should not auto-merge")
	}
}

func TestShouldAutoMerge_UnknownMergeable(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		Mergeable:     "UNKNOWN",
		Checks:        []Check{{Conclusion: "SUCCESS", Status: "COMPLETED"}},
		Reviews:       []Review{{State: "APPROVED"}},
	}
	if pr.ShouldAutoMerge() {
		t.Error("UNKNOWN mergeable should not auto-merge")
	}
}

func TestShouldAutoMerge_AlreadyMerged(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		State:         StateMerged,
		Mergeable:     "MERGEABLE",
		Checks:        []Check{{Conclusion: "SUCCESS", Status: "COMPLETED"}},
		Reviews:       []Review{{State: "APPROVED"}},
	}
	if pr.ShouldAutoMerge() {
		t.Error("merged PR should not auto-merge again")
	}
}

func TestShouldAutoMerge_Closed(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		State:         StateClosed,
		Mergeable:     "MERGEABLE",
		Checks:        []Check{{Conclusion: "SUCCESS", Status: "COMPLETED"}},
		Reviews:       []Review{{State: "APPROVED"}},
	}
	if pr.ShouldAutoMerge() {
		t.Error("closed PR should not auto-merge")
	}
}

func TestShouldAutoMerge_NoCompletedChecks(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		Mergeable:     "MERGEABLE",
		Checks:        []Check{{Status: "IN_PROGRESS", Conclusion: ""}},
		Reviews:       []Review{{State: "APPROVED"}},
	}
	if pr.ShouldAutoMerge() {
		t.Error("no completed checks should not auto-merge")
	}
}

func TestShouldAutoMerge_NoChecks(t *testing.T) {
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		Mergeable:     "MERGEABLE",
		Reviews:       []Review{{State: "APPROVED"}},
	}
	if pr.ShouldAutoMerge() {
		t.Error("no checks at all should not auto-merge (hasCompleted=false)")
	}
}

func TestShouldAutoMerge_MixedChecksAllGreenAndInProgress(t *testing.T) {
	// Some checks pass, some still running, none failing => should merge
	// (we only block on FAILURE, not on in-progress).
	pr := TrackedPR{
		AutopilotMode: PRAuto,
		Mergeable:     "MERGEABLE",
		MergeMethod:   "squash",
		Checks: []Check{
			{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "deploy", Status: "IN_PROGRESS", Conclusion: ""},
		},
		Reviews: []Review{{State: "APPROVED"}},
	}
	if !pr.ShouldAutoMerge() {
		t.Error("green + in-progress (no failures) with approval should auto-merge")
	}
}

// === ShouldHammer ===

func TestShouldHammer_Off(t *testing.T) {
	pr := TrackedPR{
		Hammer:        false,
		AutopilotMode: PRAuto,
		Checks:        []Check{{Conclusion: "FAILURE"}},
	}
	if pr.ShouldHammer() {
		t.Error("hammer=false should not hammer")
	}
}

func TestShouldHammer_AutopilotOff(t *testing.T) {
	pr := TrackedPR{
		Hammer:        true,
		AutopilotMode: PROff,
		Checks:        []Check{{Conclusion: "FAILURE"}},
	}
	if pr.ShouldHammer() {
		t.Error("autopilot=off should not hammer")
	}
}

func TestShouldHammer_MaxAttempts(t *testing.T) {
	pr := TrackedPR{
		Hammer:        true,
		AutopilotMode: PRAuto,
		HammerCount:   3,
		MaxHammer:     3,
		Checks:        []Check{{Conclusion: "FAILURE"}},
	}
	if pr.ShouldHammer() {
		t.Error("max attempts reached should not hammer")
	}
}

func TestShouldHammer_DefaultMaxIs3(t *testing.T) {
	pr := TrackedPR{
		Hammer:        true,
		AutopilotMode: PRAuto,
		HammerCount:   3,
		MaxHammer:     0, // default should be 3
		Checks:        []Check{{Conclusion: "FAILURE"}},
	}
	if pr.ShouldHammer() {
		t.Error("default max=3, at count=3 should not hammer")
	}
}

func TestShouldHammer_UnderMax(t *testing.T) {
	pr := TrackedPR{
		Hammer:        true,
		AutopilotMode: PRAuto,
		HammerCount:   1,
		MaxHammer:     3,
		Checks:        []Check{{Conclusion: "FAILURE"}},
	}
	if !pr.ShouldHammer() {
		t.Error("failing checks + hammer + under max should hammer")
	}
}

func TestShouldHammer_NoFailingChecks(t *testing.T) {
	pr := TrackedPR{
		Hammer:        true,
		AutopilotMode: PRAuto,
		HammerCount:   0,
		MaxHammer:     3,
		Checks:        []Check{{Conclusion: "SUCCESS"}},
	}
	if pr.ShouldHammer() {
		t.Error("no failing checks should not hammer")
	}
}

func TestShouldHammer_YoloMode(t *testing.T) {
	pr := TrackedPR{
		Hammer:        true,
		AutopilotMode: PRYolo,
		HammerCount:   0,
		MaxHammer:     3,
		Checks:        []Check{{Conclusion: "FAILURE"}},
	}
	if !pr.ShouldHammer() {
		t.Error("YOLO mode with failing checks should hammer")
	}
}

func TestShouldHammer_CustomMaxAttempts(t *testing.T) {
	pr := TrackedPR{
		Hammer:        true,
		AutopilotMode: PRAuto,
		HammerCount:   4,
		MaxHammer:     5,
		Checks:        []Check{{Conclusion: "FAILURE"}},
	}
	if !pr.ShouldHammer() {
		t.Error("under custom max=5, at count=4 should hammer")
	}
	pr.HammerCount = 5
	if pr.ShouldHammer() {
		t.Error("at custom max=5, count=5 should not hammer")
	}
}
