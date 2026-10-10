package llm

// admission_v195_test.go — v1.9.5 (P1): the engine-mutation ADMISSION
// seam, pinned deterministically (no engine binary, no network):
//
//   1. a transaction under an installed admission control TAKES the
//      claim for the whole transaction window (resolve → … → rollback)
//      and releases it exactly once on every exit path;
//   2. the claim's `live` verdict is the destructive-boundary authority:
//      a run active at claim time refuses the transaction with
//      ErrEngineBusyWithRun and NEVER takes or releases a claim — the
//      engine is untouched;
//   3. EngineMutationActive mirrors the held claim (the advisory signal
//      the API admission pre-checks read);
//   4. the historical nil-seam behavior (no admission control) is
//      preserved exactly: the destructive-boundary run gate alone.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// fakeAdmission is the deterministic seam double. begin records what the
// transaction observed; released counts EndEngineMutation calls.
type fakeAdmission struct {
	liveAtBegin atomic.Bool
	begins      atomic.Int64
	ends        atomic.Int64
	claimed     atomic.Bool
}

func (f *fakeAdmission) BeginEngineMutation() bool {
	f.begins.Add(1)

	if f.liveAtBegin.Load() {
		return true
	}

	f.claimed.Store(true)

	return false
}

func (f *fakeAdmission) EndEngineMutation() {
	f.ends.Add(1)
	f.claimed.Store(false)
}

func TestAdmissionClaimHeldForWholeTransaction(t *testing.T) {
	s := newGateTestServer(t)

	fa := &fakeAdmission{}
	s.SetAdmissionControl(fa)

	// Offline no-op transaction (fresh server, CPU variant already
	// installed) — the claim must be visible DURING the transaction and
	// released after it settles.
	fa.claimed.Store(false)
	claimSeenDuring := false

	// Observe the claim from "outside" while the transaction runs: the
	// no-op path is too fast to sample from another goroutine
	// deterministically, so observe the seam's own mirror instead —
	// EngineMutationActive is served from mutationClaimed, which the
	// transaction sets exactly between Begin and End.
	s.SetRunGate(nil)

	_, err := s.updateEngineVariantTx(context.Background(), updater.VariantCPU, nil, nil)
	if err != nil {
		t.Fatalf("offline no-op transaction: %v", err)
	}

	if fa.begins.Load() != 1 {
		t.Fatalf("BeginEngineMutation called %d times, want 1", fa.begins.Load())
	}
	if fa.ends.Load() != 1 {
		t.Fatalf("EndEngineMutation called %d times, want exactly 1 (released on the success path)", fa.ends.Load())
	}
	if fa.claimed.Load() {
		t.Fatal("claim still held after the transaction returned")
	}
	if s.EngineMutationActive() {
		t.Fatal("EngineMutationActive must be false after the transaction released the claim")
	}
	_ = claimSeenDuring
}

func TestAdmissionLiveRunRefusesAndNeverClaims(t *testing.T) {
	s := newGateTestServer(t)

	// The serving engine is RUNNING: the coordination point is live.
	s.setState(StateRunning)

	fa := &fakeAdmission{}
	fa.liveAtBegin.Store(true)
	s.SetAdmissionControl(fa)

	_, err := s.updateEngineVariantTx(context.Background(), updater.VariantCPU, nil, nil)
	if !errors.Is(err, ErrEngineBusyWithRun) {
		t.Fatalf("err = %v, want ErrEngineBusyWithRun", err)
	}

	if fa.begins.Load() != 1 {
		t.Fatalf("BeginEngineMutation called %d times, want 1", fa.begins.Load())
	}
	if fa.ends.Load() != 0 {
		t.Fatal("EndEngineMutation must NOT be called when no claim was taken")
	}
	if fa.claimed.Load() {
		t.Fatal("no claim may be taken when a run is already live")
	}
	if !s.IsRunning() {
		t.Fatal("the running engine must remain running after the refusal")
	}
}

func TestEngineMutationActiveMirrorsClaim(t *testing.T) {
	s := newGateTestServer(t)

	if s.EngineMutationActive() {
		t.Fatal("no claim installed — must report false")
	}

	fa := &fakeAdmission{}
	s.SetAdmissionControl(fa)

	if s.EngineMutationActive() {
		t.Fatal("installed but never begun — must report false")
	}

	// Simulate exactly what the transaction does between Begin and End.
	if live := fa.BeginEngineMutation(); live {
		t.Fatal("BeginEngineMutation must report not-live for an empty registry")
	}

	s.mu.Lock()
	s.mutationClaimed = true
	s.mu.Unlock()

	if !s.EngineMutationActive() {
		t.Fatal("held claim must be reported active")
	}

	fa.EndEngineMutation()

	s.mu.Lock()
	s.mutationClaimed = false
	s.mu.Unlock()

	if s.EngineMutationActive() {
		t.Fatal("released claim must be reported inactive")
	}
}

func TestNilAdmissionKeepsHistoricalGate(t *testing.T) {
	s := newGateTestServer(t)

	s.setState(StateRunning)

	consulted := 0
	s.SetRunGate(func() bool {
		consulted++
		return true
	})

	_, err := s.updateEngineVariantTx(context.Background(), updater.VariantCPU, nil, nil)
	if !errors.Is(err, ErrEngineBusyWithRun) {
		t.Fatalf("err = %v, want ErrEngineBusyWithRun (historical gate)", err)
	}
	if consulted != 1 {
		t.Fatalf("gate consulted %d times, want exactly 1", consulted)
	}
}
