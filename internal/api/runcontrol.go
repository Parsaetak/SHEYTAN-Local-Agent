// runcontrol.go — v1.7.4 backend-neutral PAUSE / EDIT / RESUME.
//
// ONE authoritative contract over the EXISTING run authorities:
//
//   - runLive (runstate.go) remains the ONE live lifecycle authority — the
//     pause state machine methods live there; nothing here duplicates it.
//   - the WS replay contract (run_snapshot at seq N + events > N) is
//     unchanged; a paused run stays registered with an open hub, so a
//     reconnect during PAUSED replays the draft and the revision.
//   - the durable settlement of a resumed run is the SAME
//     finishSuccessfulRun method a fresh run uses — one lifecycle, one
//     outcome path, no second run-state system.
//
// Paused-run checkpoints are BOUNDED durable records under the existing
// data root (<DataDir>/runs/paused/<runId>.json, atomic temp+rename) —
// never a new storage root, never unbounded prompt duplication.
//
// API surface (extends /api/run + /api/abort, one coherent contract):
//
//      POST /api/run/pause   {sessionId, runId}            → pause request
//      POST /api/run/edit    {sessionId, runId, revision, message?, draft?}
//      POST /api/run/resume  {sessionId, runId, revision}  → resume
//      GET  /api/run/paused  ?sessionId=                   → recoverable paused runs
//
// Pause is SEMANTIC CONTINUATION (honestly labeled): the stream stops at a
// safe boundary, the accepted prefix is checkpointed, and resume rebuilds
// the request from the persisted transcript + draft. It is not a claimed
// byte-identical KV continuation.
package api

import (
        "context"
        "encoding/json"
        "fmt"
        "net/http"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

// pausedRecordCap bounds the largest persisted draft — the same bound the
// live snapshot enforces, so a checkpoint can never outgrow what the live
// state would retain.
const pausedRecordCap = 256 << 10

// pausedRunRecord is the durable checkpoint of ONE paused run (spec §6:
// bounded durable run state — identities, the accepted prefix, the tool
// state needed to resume, generation settings, integrity metadata).
type pausedRunRecord struct {
        Version   int `json:"version"` // 1
        CreatedAt time.Time
        UpdatedAt time.Time

        RunID     string `json:"runId"`
        SessionID string `json:"sessionId"`
        Revision  int64  `json:"revision"`

        // Conversation identity: the ORIGINAL user message and, when the user
        // edited it while paused, the edited one. The session transcript stays
        // the authority; these fields make the checkpoint self-describing and
        // let a restart-recovered UI show what will be resumed.
        OriginalUserMessage string `json:"originalUserMessage,omitempty"`
        EditedUserMessage   string `json:"editedUserMessage,omitempty"`

        // The accepted generated assistant prefix (semantic continuation).
        AssistantDraft string `json:"assistantDraft,omitempty"`
        ReasoningDraft string `json:"reasoningDraft,omitempty"`

        // Tool state at the pause boundary — none / pending / committed, plus
        // the assembled-but-unexecuted calls (never blindly replayed: a resume
        // re-offers them to the model through the normal loop).
        ToolState        string         `json:"toolState,omitempty"`
        PendingToolCalls []llm.ToolCall `json:"pendingToolCalls,omitempty"`
        RoundTail        []llm.Message  `json:"roundTail,omitempty"`

        // Model/backend identity and the generation settings a resume must
        // reproduce for one coherent revision.
        Model     string   `json:"model,omitempty"`
        Backend   string   `json:"backend,omitempty"`
        Thinking  string   `json:"thinking,omitempty"`
        ToolMode  string   `json:"toolMode,omitempty"`
        ToolAllow []string `json:"toolAllow,omitempty"`
        NetSearch bool     `json:"netSearch,omitempty"`

        // ContinuationMode is honest about what resume does.
        ContinuationMode string `json:"continuationMode"` // "semantic"
}

// successRunParams carries the shared durable-settlement inputs.
type successRunParams struct {
        sess            *sessions.Session
        userMessage     string
        res             agent.RunResult
        resultOutcome   string
        terminalCaption string
        tl              *runTimeline
        settle          func(result string, caption string, persisted bool, replyText, reasonText string)
        publish         func(agent.Activity)
        stampRun        func(agent.Activity) agent.Activity
}

// finishSuccessfulRun is the ONE durable settlement path for a successful
// (non-error) run result — fresh or resumed: append the authoritative
// assistant reply exactly once, roll the session summary, write the
// mandatory agent.md handoff, index recall, roll continuum, then publish
// the terminal state. v1.7.4: extracted verbatim from the run goroutine so
// Pause/Resume cannot grow a divergent settlement.
func (s *Server) finishSuccessfulRun(p successRunParams) {
        sess := p.sess
        res := p.res
        resultOutcome := p.resultOutcome
        terminalCaption := p.terminalCaption

        // Append assistant reply — exactly once, by the one settlement path.
        if res.Text != "" {
                if _, err := s.store.AppendMessage(
                        sess.ID,
                        llm.Message{
                                Role:      "assistant",
                                Content:   res.Text,
                                Reasoning: res.Reasoning,
                        },
                ); err != nil {
                        // v1.1.4: a lost reply is a REAL failure the user must see,
                        // not a swallowed error.
                        p.publish(p.stampRun(agent.Activity{
                                Type:      "error",
                                Caption:   "The reply was generated but could not be saved to the session: " + err.Error(),
                                Timestamp: time.Now(),
                        }))

                        logging.Default().Error(
                                "api",
                                "append assistant reply: %v",
                                err,
                        )

                        p.settle("error", "reply persistence failed: "+err.Error(), false, res.Text, res.Reasoning)
                        return
                }
        }

        p.tl.persistedAt = time.Now()

        // v1.2.9 DURABLE COMPLETION ORDERING (see the original comment in the
        // run goroutine): every durable write happens BEFORE the terminal
        // publication; the post-terminal publishes are UI events only.

        // Durable step 1 — rolling session summary (best-effort).
        if res.Text != "" {
                s.updateSessionSummaryRolling(sess, p.userMessage, res.Text, res.ToolsUsed)
        }

        // Durable step 2 — mandatory agent.md handoff (Agent mode).
        handoffWriteErr := error(nil)
        handoffPath := ""

        if sess.Mode == sessions.ModeAgent && resultOutcome == "done" {
                handoffPath, handoffWriteErr = s.writeAgentHandoff(sess, res.Task, resultOutcome)
        }

        if handoffWriteErr != nil {
                resultOutcome = "error"
                terminalCaption = "agent.md handoff failed: " + handoffWriteErr.Error()
        }

        // Durable step 3 — index the completed exchange into persistent recall.
        if s.recall != nil && res.Text != "" {
                if err := s.recall.IndexTurn(
                        sess.ID,
                        sess.Title,
                        p.userMessage,
                        res.Text,
                        res.ToolsUsed,
                ); err != nil {
                        logging.Default().Warn(
                                "recall",
                                "index turn: %v",
                                err,
                        )
                }
        }

        // Durable step 4 — Continuum chapter rollover.
        var continuumChild *sessions.Session
        var continuumPrev string

        if s.continuum != nil {
                if fresh, err := s.store.Get(sess.ID); err == nil {
                        if s.continuum.ShouldRollover(fresh, s.src.Load()) {
                                if child, _, err := s.continuum.Rollover(fresh, s.src.Load()); err == nil {
                                        continuumChild = child
                                        continuumPrev = fresh.ID

                                        logging.Default().Info(
                                                "continuum",
                                                "rolled over session %s → chapter %d (%s)",
                                                fresh.ID,
                                                child.Chapter,
                                                child.ID,
                                        )
                                } else {
                                        logging.Default().Warn(
                                                "continuum",
                                                "rollover failed (conversation continues in current session): %v",
                                                err,
                                        )
                                }
                        }
                }
        }

        // Terminal publication — every required durable artifact above is
        // already on disk.
        p.settle(resultOutcome, terminalCaption, res.Text != "", res.Text, res.Reasoning)

        // Post-terminal UI events (non-durable).
        if handoffWriteErr != nil {
                p.publish(p.stampRun(agent.Activity{
                        Type:    "error",
                        Caption: "agent.md handoff failed: " + handoffWriteErr.Error(),
                        Detail: map[string]any{
                                "sessionId": sess.ID,
                        },
                        Timestamp: time.Now(),
                }))
        } else if handoffPath != "" {
                p.publish(p.stampRun(agent.Activity{
                        Type:    "handoff",
                        Caption: "agent.md handoff updated for the next agent",
                        Detail: map[string]any{
                                "path":      handoffPath,
                                "sessionId": sess.ID,
                        },
                        Timestamp: time.Now(),
                }))
        }

        if continuumChild != nil {
                p.publish(agent.Activity{
                        Type:    "session",
                        Caption: "Context threshold reached — conversation continued in chapter " + fmt.Sprint(continuumChild.Chapter),
                        Detail: map[string]any{
                                "sessionId": continuumChild.ID,
                                "threadId":  continuumChild.ThreadID,
                                "chapter":   continuumChild.Chapter,
                                "previous":  continuumPrev,
                        },
                        Timestamp: time.Now(),
                })
        }
}

// ---------------------------------------------------------------------------
// Durable paused-run checkpoint store (<DataDir>/runs/paused)
// ---------------------------------------------------------------------------

func (s *Server) pausedDir() string {
        return filepath.Join(s.src.Load().DataDir, "runs", "paused")
}

// savePausedRecord persists one checkpoint ATOMICALLY (temp + rename).
func (s *Server) savePausedRecord(rec *pausedRunRecord) error {
        dir := s.pausedDir()

        if err := os.MkdirAll(dir, 0o755); err != nil {
                return fmt.Errorf("paused runs dir: %w", err)
        }

        data, err := json.MarshalIndent(rec, "", "  ")
        if err != nil {
                return fmt.Errorf("marshal paused run: %w", err)
        }

        path := filepath.Join(dir, rec.RunID+".json")
        tmp := path + ".tmp"

        if err := os.WriteFile(tmp, data, 0o644); err != nil {
                return fmt.Errorf("write paused run: %w", err)
        }

        if err := os.Rename(tmp, path); err != nil {
                _ = os.Remove(tmp)
                return fmt.Errorf("commit paused run: %w", err)
        }

        return nil
}

// loadPausedRecord reads one checkpoint by run id.
func (s *Server) loadPausedRecord(runID string) (*pausedRunRecord, bool) {
        if runID == "" {
                return nil, false
        }

        data, err := os.ReadFile(filepath.Join(s.pausedDir(), runID+".json"))
        if err != nil || len(data) > 4*(1<<20) {
                return nil, false
        }

        var rec pausedRunRecord
        if err := json.Unmarshal(data, &rec); err != nil {
                return nil, false
        }

        return &rec, true
}

// deletePausedRecord removes a consumed or aborted checkpoint.
func (s *Server) deletePausedRecord(runID string) {
        if runID == "" {
                return
        }
        _ = os.Remove(filepath.Join(s.pausedDir(), runID+".json"))
}

// completeRoundTail makes a paused round tail wire-valid: every tool call
// in an assistant message must carry a result. Calls recorded as PENDING
// get an honest synthetic result ("never executed — run paused"); the
// model re-issues them through the normal loop on resume.
func completeRoundTail(tail []llm.Message, pending []llm.ToolCall) []llm.Message {
        if len(pending) == 0 {
                return tail
        }

        pendingIDs := make(map[string]bool, len(pending))
        for _, tc := range pending {
                if tc.ID != "" {
                        pendingIDs[tc.ID] = true
                }
        }

        out := make([]llm.Message, len(tail))
        copy(out, tail)

        for i := len(out) - 1; i >= 0; i-- {
                if out[i].Role != "assistant" || len(out[i].ToolCalls) == 0 {
                        continue
                }

                have := make(map[string]bool, len(out[i].ToolCalls))
                for _, tc := range out[i].ToolCalls {
                        have[tc.ID] = true
                }

                for _, tc := range out[i].ToolCalls {
                        if have[tc.ID] && tc.ID != "" && !pendingIDs[tc.ID] {
                                continue // a real result already follows
                        }
                        if !pendingIDs[tc.ID] {
                                continue
                        }

                        out = append(out, llm.Message{
                                Role:       "tool",
                                ToolCallID: tc.ID,
                                Content:    "Error: this tool call was never executed — the run was paused by the user before it started. Request it again if it is still needed.",
                        })

                        delete(pendingIDs, tc.ID)
                }
        }

        return out
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleRunPause requests a cooperative pause of the session's active run.
// Idempotent: a duplicate request returns the current state instead of an
// error. The orchestrator stops at the NEXT SAFE BOUNDARY; the response
// therefore reports phase "pausing", never an instant "paused".
func (s *Server) handleRunPause(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

        var body struct {
                SessionID string `json:"sessionId"`
                RunID     string `json:"runId"`
        }

        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("pause: %w", err))
                return
        }

        if body.SessionID == "" {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId required"))
                return
        }

        s.runsMu.Lock()
        rs, ok := s.runs[body.SessionID]
        s.runsMu.Unlock()

        if !ok {
                writeErr(w, http.StatusNotFound, fmt.Errorf("no active run for session %s", body.SessionID))
                return
        }

        if body.RunID != "" && body.RunID != rs.live.runID {
                writeErr(w, http.StatusConflict, fmt.Errorf(
                        "runId mismatch: active run is %s, request targeted %s", rs.live.runID, body.RunID))
                return
        }

        revision, phase, accepted := rs.live.requestPause()

        if accepted {
                // The orchestrator observes this flag at safe boundaries only.
                rs.ctrl.RequestPause()

                logging.Default().Info("run", "pause requested: runId=%s session=%s", rs.live.runID, body.SessionID)
        }

        writeJSON(w, map[string]any{
                "ok":       true,
                "runId":    rs.live.runID,
                "revision": revision,
                "phase":    phase,
        })
}

// handleRunEdit edits a PAUSED run: replace the user prompt and/or trim or
// replace the partial assistant draft. Every accepted edit creates a new
// monotonic revision; a stale revision is rejected with the current value.
// The discarded draft is never persisted as an assistant message.
func (s *Server) handleRunEdit(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

        var body struct {
                SessionID string `json:"sessionId"`
                RunID     string `json:"runId"`
                Revision  int64  `json:"revision"`
                Message   string `json:"message,omitempty"`
                Draft     string `json:"draft,omitempty"`
        }

        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("edit: %w", err))
                return
        }

        if body.SessionID == "" || body.RunID == "" {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId and runId required"))
                return
        }

        if body.Message == "" && body.Draft == "" {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("nothing to edit: provide message and/or draft"))
                return
        }

        s.runsMu.Lock()
        rs, ok := s.runs[body.SessionID]
        s.runsMu.Unlock()

        if !ok {
                writeErr(w, http.StatusNotFound, fmt.Errorf("no registered run %s for session %s", body.RunID, body.SessionID))
                return
        }

        if rs.live.runID != body.RunID {
                writeErr(w, http.StatusConflict, fmt.Errorf(
                        "runId mismatch: registered run is %s, request targeted %s", rs.live.runID, body.RunID))
                return
        }

        if !rs.live.isPausedSettled() {
                writeErr(w, http.StatusConflict, fmt.Errorf(
                        "run %s is not editable: phase is %q (editing is allowed only while paused)", body.RunID, rs.live.phaseLocked()))
                return
        }

        if _, changed := rs.live.bumpRevision(body.Revision); !changed {
                writeErr(w, http.StatusConflict, fmt.Errorf(
                        "stale revision: run %s is at revision %d, request acted on %d — reload the paused state and retry",
                        body.RunID, rs.live.currentRevision(), body.Revision))
                return
        }

        rec, found := s.loadPausedRecord(body.RunID)
        if !found {
                writeErr(w, http.StatusConflict, fmt.Errorf("paused checkpoint for %s vanished — the run can only be stopped", body.RunID))
                return
        }

        if body.Message != "" {
                rec.EditedUserMessage = body.Message

                // The session transcript stays the single authority: the ORIGINAL
                // user message (appended when the run started) is replaced in
                // place — never duplicated.
                if _, editErr := s.store.EditLastUserMessage(body.SessionID, body.Message); editErr != nil {
                        writeErr(w, http.StatusInternalServerError, fmt.Errorf("edit user message: %w", editErr))
                        return
                }
        }

        if body.Draft != "" {
                rec.AssistantDraft = body.Draft
        }

        rec.Revision = body.Revision + 1
        rec.UpdatedAt = time.Now()

        if err := s.savePausedRecord(rec); err != nil {
                writeErr(w, http.StatusInternalServerError, err)
                return
        }

        // The live state reflects the accepted edit AFTER the checkpoint is
        // durable — the phase that promises durability is published last.
        rs.live.confirmEdited(rec.AssistantDraft, rec.Revision)

        logging.Default().Info("run", "paused run edited: runId=%s revision=%d", body.RunID, rec.Revision)

        writeJSON(w, map[string]any{
                "ok":       true,
                "runId":    body.RunID,
                "revision": rec.Revision,
                "phase":    "paused",
        })
}

// handleRunResume resumes a PAUSED run from its durable checkpoint: the
// SAME runId, hub and live state continue (one authoritative run); the
// transcript + accepted draft are rebuilt into a semantic continuation
// request. The resumed settlement is the shared finishSuccessfulRun path.
func (s *Server) handleRunResume(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

        var body struct {
                SessionID string `json:"sessionId"`
                RunID     string `json:"runId"`
                Revision  int64  `json:"revision"`
        }

        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("resume: %w", err))
                return
        }

        if body.SessionID == "" || body.RunID == "" {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId and runId required"))
                return
        }

        s.runsMu.Lock()
        rs, ok := s.runs[body.SessionID]
        s.runsMu.Unlock()

        // Restart-recovery: a paused run whose registry entry is gone (process
        // restart) is re-registered from its durable checkpoint before the
        // state-machine checks run — the paused run remains resumable.
        if !ok {
                rec, found := s.loadPausedRecord(body.RunID)
                if !found || rec.SessionID != body.SessionID {
                        writeErr(w, http.StatusNotFound, fmt.Errorf("no resumable run %s for session %s", body.RunID, body.SessionID))
                        return
                }

                if rs = s.recoverPausedRun(rec); rs == nil {
                        writeErr(w, http.StatusInternalServerError, fmt.Errorf("failed to re-register paused run %s", body.RunID))
                        return
                }
        }

        if rs.live.runID != body.RunID {
                writeErr(w, http.StatusConflict, fmt.Errorf(
                        "runId mismatch: registered run is %s, request targeted %s", rs.live.runID, body.RunID))
                return
        }

        if _, ok := rs.live.requestResume(body.Revision); !ok {
                writeErr(w, http.StatusConflict, fmt.Errorf(
                        "cannot resume run %s: phase is %q, revision %d (request acted on %d)",
                        body.RunID, rs.live.phaseLocked(), rs.live.currentRevision(), body.Revision))
                return
        }

        rec, found := s.loadPausedRecord(body.RunID)
        if !found {
                // The state machine said paused but the checkpoint is gone —
                // honest failure, never a fabricated continuation.
                rs.live.confirmResumed()
                writeErr(w, http.StatusConflict, fmt.Errorf("paused checkpoint for %s is missing — cannot resume", body.RunID))
                return
        }

        logging.Default().Info("run", "resume: runId=%s session=%s revision=%d", body.RunID, body.SessionID, body.Revision)

        go s.runResumed(rs, rec)

        writeJSON(w, map[string]any{
                "ok":       true,
                "runId":    body.RunID,
                "revision": body.Revision,
                "phase":    "resuming",
        })
}

// handlePausedRuns lists recoverable paused checkpoints — the restart
// recovery surface: after a process restart a paused run is still paused.
func (s *Server) handlePausedRuns(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                return
        }

        sessionID := r.URL.Query().Get("sessionId")

        entries, err := os.ReadDir(s.pausedDir())
        if err != nil {
                writeJSON(w, []any{})
                return
        }

        out := make([]map[string]any, 0)

        for _, e := range entries {
                if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
                        continue
                }

                rec, ok := s.loadPausedRecord(e.Name()[:len(e.Name())-len(".json")])
                if !ok {
                        continue
                }

                if sessionID != "" && rec.SessionID != sessionID {
                        continue
                }

                draft := rec.AssistantDraft
                if rec.EditedUserMessage != "" {
                        out = append(out, map[string]any{
                                "runId":          rec.RunID,
                                "sessionId":      rec.SessionID,
                                "revision":       rec.Revision,
                                "userMessage":    rec.EditedUserMessage,
                                "assistantDraft": draft,
                                "updatedAt":      rec.UpdatedAt,
                        })
                        continue
                }

                out = append(out, map[string]any{
                        "runId":          rec.RunID,
                        "sessionId":      rec.SessionID,
                        "revision":       rec.Revision,
                        "userMessage":    rec.OriginalUserMessage,
                        "assistantDraft": draft,
                        "updatedAt":      rec.UpdatedAt,
                })
        }

        writeJSON(w, out)
}

// recoverPausedRun re-registers a checkpointed paused run after a process
// restart: same runId, same session, phase PAUSED, revision from the
// checkpoint. The hub is fresh (the old one died with the process).
func (s *Server) recoverPausedRun(rec *pausedRunRecord) *runState {
        if _, err := s.store.Get(rec.SessionID); err != nil {
                return nil // the session vanished — nothing to attach to
        }

        live := newRunLive(rec.RunID, rec.SessionID, rec.CreatedAt)
        live.confirmPaused(rec.AssistantDraft, "recovered from restart")

        // A recovered paused run owns a cancellation token only so the
        // one-run-per-session replacement keeps working; the resume runner
        // derives its own context.
        _, cancel := context.WithCancel(context.Background())
        hub := newActivityHub()

        rs := &runState{
                cancel: cancel,
                hub:    hub,
                live:   live,
                ctrl:   agent.NewRunControl(),
        }

        s.runsMu.Lock()

        if old, ok := s.runs[rec.SessionID]; ok {
                old.cancel()
                old.hub.close()
        }

        s.runs[rec.SessionID] = rs
        s.runsMu.Unlock()

        return rs
}

// runResumed is the resume half of the ONE run lifecycle: it reuses the
// registered runState (same runId, hub, live, ctrl), rebuilds the
// continuation request from the durable checkpoint and the CURRENT session
// transcript, and settles through the shared finishSuccessfulRun path.
func (s *Server) runResumed(rs *runState, rec *pausedRunRecord) {
        sess, err := s.store.Get(rec.SessionID)
        if err != nil {
                rs.live.settleTerminal("error", "resume failed: session unavailable", false, "", "")
                return
        }

        publish := func(a agent.Activity) {
                a.RunID = rs.live.runID
                a.Seq = rs.live.nextSeq()
                rs.live.observe(a)
                rs.hub.publish(a)
        }

        stampRun := func(a agent.Activity) agent.Activity {
                a.RunID = rs.live.runID
                return a
        }

        var settledOnce sync.Once

        settle := func(result, caption string, persisted bool, replyText, reasonText string) {
                settledOnce.Do(func() {
                        if s.outcomes != nil {
                                s.outcomes.record(sess.ID, runOutcome{
                                        RunID:       rs.live.runID,
                                        StartedAt:   rs.live.startedAt,
                                        EndedAt:     time.Now(),
                                        Outcome:     result,
                                        Caption:     caption,
                                        Persisted:   persisted,
                                        ReplyChars:  len(replyText),
                                        ReasonChars: len(reasonText),
                                })
                        }

                        rs.live.settleTerminal(result, caption, persisted, replyText, reasonText)

                        logging.Default().Info("run", "runId=%s session=%s outcome=%s (resumed)", rs.live.runID, sess.ID, result)
                })
        }

        // Fresh budget + cancel for the resumed generation; the pause was NOT
        // an abort, so nothing from the old context carries over.
        runCtx := context.Background()

        if budget := s.src.Load().EffectiveRunTimeout(); budget > 0 {
                var budgetCtx context.Context
                budgetCtx, budgetCancel := context.WithTimeout(runCtx, budget)
                runCtx = budgetCtx
                defer budgetCancel()
        }

        ctx, cancel := context.WithCancel(runCtx)
        defer cancel()

        // Release the registry entry when the RESUMED run settles (the pause
        // path left it in place on purpose).
        defer func() {
                rs.live.mu.Lock()
                terminal := rs.live.terminalOutcome != ""
                rs.live.mu.Unlock()

                if terminal {
                        s.runsMu.Lock()
                        if current, ok := s.runs[sess.ID]; ok && current == rs {
                                delete(s.runs, sess.ID)
                        }
                        s.runsMu.Unlock()

                        rs.hub.close()
                        s.deletePausedRecord(rs.live.runID)
                }
        }()

        // The user message is ALREADY in the transcript (appended when the run
        // started); the edit path replaced it in place. Resume never appends a
        // second user message.
        messages := make([]llm.Message, 0, len(sess.Messages))
        messages = append(messages, sess.Messages...)

        // Wire-valid round tail: real results stay, pending calls get honest
        // synthetic results so the conversation contract holds.
        tail := completeRoundTail(rec.RoundTail, rec.PendingToolCalls)
        messages = append(messages, tail...)

        // One authoritative revision: the accepted draft rides the request;
        // the final assistant message is draft + continuation, appended once
        // by finishSuccessfulRun.
        resumeDraft := rec.AssistantDraft

        // Engine gate — identical posture to a fresh run.
        if err := s.stack.EnsureLLMContext(ctx); err != nil {
                settle("error", fmt.Sprintf("Engine unavailable: %v", err), false, "", "")
                publish(stampRun(agent.Activity{
                        Type:      "error",
                        Caption:   fmt.Sprintf("Engine unavailable: %v", err),
                        Timestamp: time.Now(),
                }))
                return
        }

        // Back to generating: the run control is reset so the SAME ctrl can be
        // reused for a future pause of the resumed run.
        rs.ctrl.Clear()
        rs.live.confirmResumed()

        res, err := s.orch.RunDetailed(
                ctx,
                messages,
                func(a agent.Activity) { publish(a) },
                agent.WithThinkingMode(rec.Thinking),
                agent.WithToolPolicy(rec.ToolMode, rec.ToolAllow),
                agent.WithNetSearch(rec.NetSearch),
                agent.WithRunIdentity(sess.ID, sess.ThreadID, rs.live.runID),
                agent.WithRunControl(rs.ctrl),
                agent.WithResumeContinuation(resumeDraft, rec.ReasoningDraft),
        )

        if err != nil {
                settle("error", err.Error(), false, res.Text, res.Reasoning)
                publish(stampRun(agent.Activity{
                        Type:      "error",
                        Caption:   err.Error(),
                        Timestamp: time.Now(),
                }))
                return
        }

        // A resumed run may itself be PAUSED again (pause → resume → pause):
        // that is the same contract, applied to the updated checkpoint.
        if res.Paused {
                s.pauseResumedRun(rs, rec, res, settle, publish, stampRun)
                return
        }

        // Join draft + continuation into ONE authoritative revision (never
        // duplicated: the draft was never persisted as an assistant message).
        if resumeDraft != "" && res.Text != "" {
                res.Text = joinDraftContinuation(resumeDraft, res.Text)
        } else if resumeDraft != "" && res.Text == "" {
                // The continuation produced nothing new — the draft IS the answer.
                res.Text = resumeDraft
        }

        resultOutcome := "done"
        if ctx.Err() != nil {
                resultOutcome = "aborted"
        }

        terminalCaption := "Completed"

        userMessage := rec.OriginalUserMessage
        if rec.EditedUserMessage != "" {
                userMessage = rec.EditedUserMessage
        }

        s.finishSuccessfulRun(successRunParams{
                sess:            sess,
                userMessage:     userMessage,
                res:             res,
                resultOutcome:   resultOutcome,
                terminalCaption: terminalCaption,
                tl:              &runTimeline{runID: rs.live.runID, sessionID: sess.ID, acceptedAt: rs.live.startedAt, registeredAt: rs.live.startedAt},
                settle:          settle,
                publish:         publish,
                stampRun:        stampRun,
        })
}

// pauseResumedRun checkpoints a pause that happened on a RESUMED run: the
// same state machine, a bumped history, an updated draft.
func (s *Server) pauseResumedRun(
        rs *runState,
        prev *pausedRunRecord,
        res agent.RunResult,
        settle func(string, string, bool, string, string),
        publish func(agent.Activity),
        stampRun func(agent.Activity) agent.Activity,
) {
        draft := res.Text
        if prev.AssistantDraft != "" && draft == "" {
                // A committed-boundary pause mid-continuation: the accepted prefix
                // is still the previous draft.
                draft = prev.AssistantDraft
        } else if prev.AssistantDraft != "" && draft != "" && res.PauseToolState == agent.ToolStateCommitted {
                draft = prev.AssistantDraft
        }

        rec := *prev
        rec.AssistantDraft = draft
        rec.ReasoningDraft = res.Reasoning
        rec.ToolState = string(res.PauseToolState)
        rec.PendingToolCalls = res.PendingToolCalls
        rec.RoundTail = completeRoundTail(res.RoundTail, res.PendingToolCalls)
        rec.UpdatedAt = time.Now()

        if err := s.savePausedRecord(&rec); err != nil {
                settle("error", "pause checkpoint failed: "+err.Error(), false, draft, res.Reasoning)
                publish(stampRun(agent.Activity{
                        Type:      "error",
                        Caption:   "The run could not be paused safely: " + err.Error(),
                        Timestamp: time.Now(),
                }))
                return
        }

        // Checkpoint is durable — only now does the state promise PAUSED.
        rs.live.confirmPaused(draft, "paused at a safe boundary")

        publish(stampRun(agent.Activity{
                Type:      "status",
                Caption:   "Paused",
                Detail:    map[string]any{"phase": "paused", "revision": rec.Revision},
                Timestamp: time.Now(),
        }))
}

// joinDraftContinuation merges the accepted prefix and the continuation
// into one authoritative revision. The join is byte-honest: no invented
// punctuation, one space when both sides need a separator.
func joinDraftContinuation(draft, continuation string) string {
        // The seam is decided from the ORIGINAL draft tail: a draft that
        // already ends with a newline needs no space (the newline IS the
        // separator). Deciding after the TrimRight below made the newline
        // check dead code and injected a stray space after every
        // newline-terminated draft — the "byte-honest" join regressed.
        seam := " "
        if strings.HasSuffix(draft, "\n") {
                seam = "\n"
        }

        draft = strings.TrimRight(draft, " \t\n\r")
        continuation = strings.TrimLeft(continuation, " \t")

        if draft == "" {
                return continuation
        }
        if continuation == "" {
                return draft
        }

        return draft + seam + continuation
}
