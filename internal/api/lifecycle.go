package api

import "time"

// Close stops active agent runs and releases the resources owned by the
// shared runtime stack.
//
// The API server is the owner of the Stack instance created by New(), so
// shutting down the API must also shut down the runtime resources it owns.
func (s *Server) Close() {
	if s == nil {
		return
	}

	// Stop the scheduled engine-update loop FIRST and wait (bounded) for
	// its in-flight pass to finish.
	//
	// v1.2.0: the updater goroutine was previously never cancelled — it
	// outlived the server and, in the test suite, kept writing into the
	// temp data dir while t.TempDir() cleanup ran RemoveAll, producing
	// the observed "bin\.update-stage: The directory is not empty"
	// failures. Deterministic ownership: cancel → wait → then tear down.
	s.updateCancelIfNeeded()

	// Cancel every active HTTP-triggered run.
	//
	// Do not hold runsMu while calling into the runtime; cancellation can
	// cause callbacks/goroutines to touch the run registry.
	s.runsMu.Lock()
	cancels := make([]func(), 0, len(s.runs))

	for _, rs := range s.runs {
		if rs != nil && rs.cancel != nil {
			cancels = append(cancels, rs.cancel)
		}
	}

	s.runsMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	// The orchestrator itself needs no explicit abort: every run it
	// executes is bound to the request context canceled above, so
	// canceling the registered runs stops all orchestrator work.

	// Stop the engine event fan-out before tearing down the engine it
	// subscribes to.
	if s.engineStop != nil {
		select {
		case <-s.engineStop:
		default:
			close(s.engineStop)
		}
	}

	if s.engineDone != nil {
		select {
		case <-s.engineDone:
		case <-time.After(2 * time.Second):
		}
	}

	// Release the shared runtime resources.
	if s.stack != nil {
		s.stack.Close()
	}
}

// updateCancelIfNeeded cancels the scheduled engine-update loop and waits
// up to 3 seconds for its completion signal. Safe on servers that never
// started the loop.
func (s *Server) updateCancelIfNeeded() {
	if s.updateCancel != nil {
		s.updateCancel()
	}

	if s.updateDone != nil {
		select {
		case <-s.updateDone:
		case <-time.After(3 * time.Second):
		}
	}
}
