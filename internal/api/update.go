// v1.2.0 — the application update surface: check against the authoritative
// GitHub release manifest, and stage a SHA-256-verified artifact. This file
// NEVER executes installers and never deletes user data; the staged
// artifact is applied by the user / OS installer, which is the honest
// recovery-friendly flow for this release.
package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// handleUpdateStatus serves GET /api/update/status — the last-known update
// state (up-to-date | update-available | ready | …). A stale/absent state
// reports honestly as unknown; use /api/update/check for a live check.
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	st := AppUpdateStatusInternal{
		State:   updater.AppUnknown,
		Version: config.AppVersion,
		Message: "not checked yet — run Check now",
	}

	if cached, ok := s.lastAppUpdate.Load().(updater.AppUpdateStatus); ok {
		st.State = cached.State
		st.Latest = cached.Latest
		st.Channel = cached.Channel
		st.Notes = cached.Notes
		st.PublishedAt = cached.PublishedAt
		st.StagedPath = cached.StagedPath
		st.StagedSHA256 = cached.StagedSHA256
		st.CheckedAt = cached.CheckedAt
		st.Message = cached.Message
	}

	writeJSON(w, st)
}

// handleUpdateCheck serves POST /api/update/check — live manifest check.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	st, err := updater.CheckAppUpdate(r.Context(), config.AppVersion, updater.AppManifestURL)
	s.lastAppUpdate.Store(st)

	if err != nil {
		// A failed check is a REPORTED state, not a 5xx: the UI shows the
		// honest check-failed message.
		writeJSON(w, st)
		return
	}
	writeJSON(w, st)
}

// handleUpdateDownload serves POST /api/update/download — stage the
// platform artifact with SHA-256 verification. Requires a successful
// check first.
func (s *Server) handleUpdateDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	cfg := s.src.Load()
	cached, ok := s.lastAppUpdate.Load().(updater.AppUpdateStatus)
	if !ok || cached.Latest == "" || cached.State != updater.AppUpdateAvailable {
		writeErr(w, http.StatusConflict, errors.New("run a successful update check first"))
		return
	}

	manifest := &updater.AppManifest{
		Version:     cached.Latest,
		Channel:     cached.Channel,
		Notes:       cached.Notes,
		PublishedAt: cached.PublishedAt,
	}
	// Re-fetch the manifest so the digest we verify against is the one the
	// release CURRENTLY publishes (never a stale copy).
	fresh, err := updater.FetchAppManifest(r.Context(), updater.AppManifestURL)
	if err == nil && fresh.Version == cached.Latest {
		manifest = fresh
	} else if err != nil {
		cached.State = updater.AppFailed
		cached.Message = "manifest re-fetch failed: " + err.Error()
		cached.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		s.lastAppUpdate.Store(cached)
		writeJSON(w, cached)
		return
	}

	path, sha, err := updater.StageAppUpdate(r.Context(), cfg.DataDir, manifest, updater.PlatformID())

	cached.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		cached.State = updater.AppFailed
		cached.Message = err.Error()
		s.lastAppUpdate.Store(cached)
		writeJSON(w, cached)
		return
	}

	cached.State = updater.AppReady
	cached.StagedPath = path
	cached.StagedSHA256 = sha
	cached.Message = "verified and staged — run the installer to apply"
	s.lastAppUpdate.Store(cached)
	writeJSON(w, cached)
}

// AppUpdateStatusInternal is the wire shape (mirrors
// updater.AppUpdateStatus with stable JSON names).
type AppUpdateStatusInternal = updater.AppUpdateStatus
