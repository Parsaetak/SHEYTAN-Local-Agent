// Package ctxtelemetry implements Phase 7 context-effectiveness telemetry.
//
// The small-model strategy lives or dies on ONE question: which context
// actually improved the task? This package records the measurable answer
// per turn — tokens added and removed, retrieval latency and relevance,
// reused context, compression ratio, pressure, and whether retrieval was
// followed by successful tool work and task success — into a bounded JSONL
// store that diagnostics can aggregate. It observes; it never decides.
package ctxtelemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TurnRecord is one turn's context-effectiveness measurement.
type TurnRecord struct {
	// Turn identifies the measurement (session + sequence).
	SessionID string    `json:"sessionId"`
	Turn      int       `json:"turn"`
	At        time.Time `json:"at"`

	// Model and budget context.
	Model        string  `json:"model,omitempty"`
	BudgetTokens int     `json:"budgetTokens,omitempty"`
	UsedTokens   int     `json:"usedTokens,omitempty"`
	Pressure     float64 `json:"pressure,omitempty"`

	// Context deltas.
	TokensAdded   int `json:"tokensAdded,omitempty"`   // injected blocks (recall, attachments, skills)
	TokensRemoved int `json:"tokensRemoved,omitempty"` // windowed/elided history
	ReusedTokens  int `json:"reusedTokens,omitempty"`  // stable prefix served from the engine prompt cache

	// Compression ratio: removed / (added+removed) — 0 when nothing was
	// compressed; approaches 1 when almost everything was elided.
	CompressionRatio float64 `json:"compressionRatio,omitempty"`

	// Retrieval measurements.
	RetrievalLatencyMs int64   `json:"retrievalLatencyMs,omitempty"`
	RetrievalHits      int     `json:"retrievalHits,omitempty"`
	RetrievalRelevance float64 `json:"retrievalRelevance,omitempty"` // mean recall score of injected digests

	// Outcomes observed AFTER the context was assembled.
	ToolCalls       int     `json:"toolCalls,omitempty"`
	ToolSuccesses   int     `json:"toolSuccesses,omitempty"`
	ToolSuccessRate float64 `json:"toolSuccessRate,omitempty"`
	TaskSuccess     bool    `json:"taskSuccess,omitempty"`
	Verified        string  `json:"verified,omitempty"` // objective verification verdict
}

// Store is the bounded JSONL telemetry store.
type Store struct {
	mu      sync.Mutex
	path    string
	maxRecs int
}

// Option configures a store.
type Option func(*Store)

// WithMaxRecords overrides the compaction bound (also useful for tests).
func WithMaxRecords(n int) Option {
	return func(s *Store) { s.maxRecs = n }
}

// NewStore creates the store under dir (file: ctxtelemetry.jsonl).
func NewStore(dir string, opts ...Option) *Store {
	s := &Store{
		path:    filepath.Join(dir, "ctxtelemetry.jsonl"),
		maxRecs: 4096,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Record appends one turn measurement; oversized stores are compacted to
// the newest half. Errors are swallowed by design: telemetry must never
// break a turn.
func (s *Store) Record(rec TurnRecord) {
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}

	existing := s.readLocked()
	existing = append(existing, rec)
	if len(existing) > s.maxRecs {
		existing = append([]TurnRecord(nil), existing[len(existing)-s.maxRecs/2:]...)
	}

	var b []byte
	for _, r := range existing {
		line, err := json.Marshal(r)
		if err != nil {
			continue
		}
		b = append(b, line...)
		b = append(b, '\n')
	}

	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

func (s *Store) readLocked() []TurnRecord {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	var out []TurnRecord
	for _, line := range splitLines(data) {
		var r TurnRecord
		if json.Unmarshal(line, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// Recent returns the newest n records.
func (s *Store) Recent(n int) []TurnRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.readLocked()
	if n <= 0 || len(all) <= n {
		return all
	}
	return append([]TurnRecord(nil), all[len(all)-n:]...)
}

// Summary aggregates the store: means over the recorded window.
type Summary struct {
	Turns             int     `json:"turns"`
	AvgPressure       float64 `json:"avgPressure"`
	AvgCompression    float64 `json:"avgCompression"`
	AvgRetrievalMs    float64 `json:"avgRetrievalMs"`
	AvgRelevance      float64 `json:"avgRelevance"`
	ToolSuccessRate   float64 `json:"toolSuccessRate"`
	RetrievedTurnRate float64 `json:"retrievedTurnRate"` // share of turns that injected recall
}

// Summarize aggregates all records.
func (s *Store) Summarize() Summary {
	s.mu.Lock()
	recs := s.readLocked()
	s.mu.Unlock()

	var sum Summary
	sum.Turns = len(recs)
	if sum.Turns == 0 {
		return sum
	}

	var press, comp, retMs, rel, tRate float64
	var retrieved, toolTurns int
	for _, r := range recs {
		press += r.Pressure
		comp += r.CompressionRatio
		retMs += float64(r.RetrievalLatencyMs)
		rel += r.RetrievalRelevance
		if r.RetrievalHits > 0 {
			retrieved++
		}
		if r.ToolCalls > 0 {
			toolTurns++
			tRate += float64(r.ToolSuccesses) / float64(r.ToolCalls)
		}
	}

	n := float64(sum.Turns)
	sum.AvgPressure = press / n
	sum.AvgCompression = comp / n
	sum.AvgRetrievalMs = retMs / n
	sum.AvgRelevance = rel / n
	if toolTurns > 0 {
		sum.ToolSuccessRate = tRate / float64(toolTurns)
	}
	sum.RetrievedTurnRate = float64(retrieved) / n
	return sum
}

func splitLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range data {
		if c == '\n' {
			if i > start {
				out = append(out, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}
