package llm

// Rediscover — the Repair/Rediscover surface (v1.3.6, spec §9/§8).
//
// Until now the Tier-2 system discovery pass (engdiscovery.FullScan)
// existed but had no production caller: QuickFind (Tier 0/1) ran on the
// boot path and nothing else. This is the missing production wiring:
//
//   - an explicit, user-triggerable API action (POST /api/engine/rediscover);
//   - it never runs on the startup path (spec §11: no startup disk crawl);
//   - it re-runs the discovery order BEFORE falling back to any download:
//     managed dir → persisted cache → Tier 1 → bounded Tier 2;
//   - a validated candidate is imported through the ONE provisioning
//     authority (updater.ImportCandidate — closure-restricted, §11/§12);
//   - the whole operation holds the lifecycle lock AND the cross-process
//     ownership lease (spec §15/§16);
//   - after a successful import the engine is restarted and verified —
//     an engine that does not start after rediscovery is an ERROR, and
//     the previous package state is honestly reported.
//
// The scan itself is bounded (workers/duration/depth/candidates, see
// engdiscovery.DefaultScanOptions) and access-denied safe.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/engdiscovery"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/englease"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// rediscoverScanTimeout bounds the Tier-2 walk for the explicit
// rediscovery operation. The default scan options already bound workers,
// depth and candidate count; this caps the wall clock.
const rediscoverScanTimeout = 3 * time.Minute

// Rediscover runs the full discovery ladder (Tier 0→1→2), imports a
// validated candidate when one exists, and restarts the engine. It is
// the user-facing Repair/Rediscover action.
func (s *LlamaServer) Rediscover() (string, error) {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()

	cfg := s.src.Load()

	lease, leaseErr := englease.Acquire(engineLeaseDir(cfg), englease.OwnerLease, "engine-owner")
	if leaseErr != nil {
		return "", fmt.Errorf("rediscover refused: %w", leaseErr)
	}

	defer lease.Release()

	// Expectations for the managed binary path.
	binPath := expectedEngineBinPath(cfg)

	wasRunning := s.IsRunning()

	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()

	if wasRunning {
		s.setState(StateUpdating)

		logging.Default().Info("engine", "rediscover: stopping engine for package re-import")

		if err := s.Stop(); err != nil {
			s.mu.Lock()
			s.stopping = false
			s.mu.Unlock()

			return "", fmt.Errorf("stop engine before rediscover: %w", err)
		}
	} else {
		s.setState(StateUpdating)
	}

	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()

	restartLastKnownGood := func() {
		if startErr := s.startLocked(); startErr != nil {
			logging.Default().Warn("engine", "rediscover: engine restart failed: %v", startErr)
		}
	}

	// 1) QUICK ladder first (Tier 0 managed dir + cache, Tier 1): when a
	// usable engine is already there, rediscovery is a no-op that
	// reports honestly instead of touching anything.
	if _, err := os.Stat(binPath); err == nil {
		if !wasRunning {
			restartLastKnownGood()
		}

		return fmt.Sprintf(
			"managed engine already present and usable at %s — nothing to rediscover",
			binPath,
		), nil
	}

	// 2) QuickFind (Tier 0b cache + Tier 1 cheap locations).
	if s.tryImportDiscoveredEngine(cfg) {
		if _, err := os.Stat(binPath); err == nil {
			restartLastKnownGood()

			return "rediscovered engine imported from the quick discovery tier (cache/PATH/known locations)", nil
		}
	}

	// 3) Tier-2 bounded background-grade scan — here run synchronously
	// because the user EXPLICITLY asked for rediscovery (never on the
	// startup path, spec §11).
	scanCtx, cancel := context.WithTimeout(context.Background(), rediscoverScanTimeout)
	defer cancel()

	s.logf("rediscover: running bounded full-system discovery scan (tier 2)")

	cands := engdiscovery.FullScan(scanCtx, cfg, llamaBinaryName(), engdiscovery.DefaultScanOptions())

	var best *engdiscovery.Candidate

	for i := range cands {
		if cands[i].Validated && cands[i].Kind == "llama-server" {
			if best == nil || cands[i].Tier < best.Tier {
				best = &cands[i]
			}
		}
	}

	if best == nil {
		// Honest failure: nothing usable exists locally. The caller can
		// now decide to download (online) or stay offline with a clear
		// error — rediscovery never downloads by itself.
		msg := "system discovery found no usable engine on this machine (Tier 0/1/2 exhausted)"

		if wasRunning {
			restartLastKnownGood()
		}

		return msg, errors.New(msg)
	}

	s.logf("rediscover: importing validated candidate %s (tier %d, sha256 %.12s…)",
		best.Path, best.Tier, best.SHA256)

	result, ierr := updater.ImportCandidate(cfg, filepath.Dir(best.Path), filepath.Base(best.Path))
	if ierr != nil {
		if wasRunning {
			restartLastKnownGood()
		}

		return "", fmt.Errorf("rediscovered candidate rejected: %w", ierr)
	}

	if best.Tag != "" {
		updater.RecordEngineTag(cfg, best.Tag)
	}

	restartLastKnownGood()

	logging.Default().Info("engine", "rediscover imported: %s", result.Outcome)

	return result.Outcome, nil
}
