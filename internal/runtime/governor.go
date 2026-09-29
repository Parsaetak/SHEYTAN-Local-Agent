// governor.go — v1.8.0 Runtime Governor wiring on the Stack.
//
// ONE closed loop, built entirely on existing authorities:
//
//   - the EXISTING preflight.LiveMonitor keeps its sampler, cadence,
//     hysteresis and the critical-protection path (cooperative
//     cancellation of active runs) — untouched;
//   - the SAME poll loop now also feeds the accepted sample to the
//     Governor, which folds it into the resource state and recomputes
//     the envelope;
//   - the Governor exposes POLICY (envelope, admission, self-model);
//     it executes nothing.
//
// Seams injected here:
//
//   - CPU load: governor.CPULoadPlatform (measured where the platform
//     can measure; unknown elsewhere — never guessed);
//   - engine facts: the EXISTING engine lifecycle owner (llm.Backend
//     Metrics — pid/model/RSS), read-only;
//   - active runs: the EXISTING Stack run counter.
package runtime

import (
        "context"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/governor"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
)

// StartGovernor builds the ONE runtime policy authority over the existing
// live monitor's poll path. It is idempotent and safe to call before or
// after StartLiveMonitor — without a monitor the Governor still runs, fed
// at whatever cadence the wiring observes.
func (s *Stack) StartGovernor() {
        if s.governor != nil {
                return // already started
        }

        g := governor.New(
                governor.DefaultThresholds(),
                governor.CPULoadPlatform,
                s.governorEngineFacts,
                nil,
        )
        g.SetActiveRunsSource(s.ActiveRuns)

        s.governorMu.Lock()
        if s.governor != nil { // lost the race — keep the existing one
                s.governorMu.Unlock()
                return
        }
        s.governor = g
        s.governorMu.Unlock()

        logging.Default().Info("resources", "runtime governor active: policy authority wired to the live monitor")
}

// Governor exposes the ONE runtime policy authority (nil before
// StartGovernor — callers must be nil-safe).
func (s *Stack) Governor() *governor.Governor {
        s.governorMu.Lock()
        defer s.governorMu.Unlock()
        return s.governor
}

// governorEngineFacts reads the engine lifecycle owner's published facts.
// Read-only: the Governor never starts, stops or probes engine processes.
func (s *Stack) governorEngineFacts() governor.EngineFacts {
        eng := s.Engine()
        if eng == nil {
                return governor.EngineFacts{}
        }

        m, err := eng.Metrics(context.Background())
        if err != nil {
                // A metrics read failure is an honest unknown, not a fake
                // "engine stopped": the engine may be perfectly alive.
                return governor.EngineFacts{}
        }

        facts := governor.EngineFacts{
                Running: m.Pid != 0,
                Model:   m.Model,
        }

        if m.ProcessRSSBytes > 0 {
                facts.RSSBytes = int64(m.ProcessRSSBytes)
                facts.Known = true
        }

        return facts
}

// observeGovernor folds one accepted monitor sample into the Governor.
// Called from the SAME poll loop that drives the monitor — one cadence,
// one thread, no second scheduler.
func (s *Stack) observeGovernor(sample preflight.Sample) {
        g := s.Governor()
        if g == nil {
                return
        }

        g.Observe(sample)
}
