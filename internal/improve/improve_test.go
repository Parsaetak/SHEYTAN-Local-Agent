package improve

import (
	"testing"
)

func TestProposeRequiresCompleteDefinition(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.Propose(Tactic{ID: "t1", Condition: "build fails", Action: "run tests first"}); err == nil {
		t.Fatal("tactic without a prediction must be rejected")
	}

	err := store.Propose(Tactic{
		ID:         "t1",
		Condition:  "build fails after an edit",
		Action:     "re-run the smallest verification command before diagnosing",
		Prediction: "the failing test is identified within one iteration",
	})
	if err != nil {
		t.Fatalf("valid proposal: %v", err)
	}
}

// TestUnverifiedAttemptsNeverPromote: the model's own success claims can
// never make a tactic active — only objective verification can.
func TestUnverifiedAttemptsNeverPromote(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Propose(Tactic{
		ID: "t1", Condition: "c", Action: "a", Prediction: "p",
	})

	for i := 0; i < 5; i++ {
		state, err := store.RecordAttempt(Attempt{
			TacticID:     "t1",
			Predicted:    "p",
			Observed:     "p",
			Matched:      true,
			Verification: "not_verified", // model prose, no objective evidence
		})
		if err != nil {
			t.Fatal(err)
		}
		if state == StateActive {
			t.Fatal("unverified attempts must never activate a tactic")
		}
	}

	active := store.Active()
	if len(active) != 0 {
		t.Fatalf("active tactics = %d, want 0", len(active))
	}
}

// TestVerifiedPredictionsPromote: two independently verified matching
// predictions promote the tactic; one verified contradiction retires it.
func TestVerifiedPredictionsPromoteThenRetire(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Propose(Tactic{
		ID: "t2", Condition: "c", Action: "a", Prediction: "p",
	})

	state, err := store.RecordAttempt(Attempt{
		TacticID: "t2", Predicted: "p", Observed: "p",
		Matched: true, Verification: "verified",
	})
	if err != nil || state == StateActive {
		t.Fatalf("first verified success must leave the candidate: state=%s err=%v", state, err)
	}

	state, err = store.RecordAttempt(Attempt{
		TacticID: "t2", Predicted: "p", Observed: "p",
		Matched: true, Verification: "verified",
	})
	if err != nil || state != StateActive {
		t.Fatalf("second verified success must activate: state=%s err=%v", state, err)
	}

	active := store.Active()
	if len(active) != 1 {
		t.Fatalf("active = %d, want 1", len(active))
	}

	// A verified contradiction retires the tactic.
	state, err = store.RecordAttempt(Attempt{
		TacticID: "t2", Predicted: "p", Observed: "something else",
		Matched: false, Verification: "verified",
	})
	if err != nil || state != StateRetired {
		t.Fatalf("verified contradiction must retire: state=%s err=%v", state, err)
	}
	if len(store.Active()) != 0 {
		t.Fatal("retired tactic must not stay active")
	}
}

func TestRenderActive(t *testing.T) {
	out := RenderActive([]Tactic{{
		ID: "t", Condition: "the build breaks", Action: "run the smallest check",
		Prediction: "root cause in one step", Successes: 2, Uses: 2,
	}}, 2000)
	if out == "" {
		t.Fatal("rendered block empty")
	}
	if RenderActive(nil, 2000) != "" {
		t.Fatal("empty tactics must render nothing")
	}
}
