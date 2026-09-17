package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAddTaskRejectsShortInterval(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	err := s.AddTask(Task{ID: "t1", Name: "too fast", Trigger: EventTimer, Interval: time.Minute, Prompt: "do"})
	if err == nil {
		t.Fatal("interval below the 5-minute floor must be rejected")
	}
}

func TestAddTaskAndPersist(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, nil, nil)

	if err := s.AddTask(Task{ID: "t1", Name: "daily", Trigger: EventTimer, Interval: 30 * time.Minute, Prompt: "check engines"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.AddTask(Task{ID: "t1", Name: "dup", Trigger: EventManual, Prompt: "x"}); err == nil {
		t.Fatal("duplicate id must be rejected")
	}

	s2 := New(dir, nil, nil)
	if err := s2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	tasks := s2.Tasks()
	if len(tasks) != 1 || tasks[0].ID != "t1" {
		t.Fatalf("persisted tasks: %+v", tasks)
	}
}

func TestFireRunsBoundedAndRecords(t *testing.T) {
	dir := t.TempDir()
	ran := false
	s := New(dir, func(ctx context.Context, task Task) (string, error) {
		ran = true
		return "task output", nil
	}, func(task Task, report Report) {
		if !report.OK {
			t.Fatalf("memory summary expects success: %+v", report)
		}
	})

	if err := s.AddTask(Task{ID: "m1", Name: "manual", Trigger: EventManual, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}

	report, err := s.Fire(context.Background(), "m1")
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !ran || !report.OK {
		t.Fatalf("report: %+v", report)
	}

	reports := s.Reports(10)
	if len(reports) != 1 || reports[0].Trigger != EventManual {
		t.Fatalf("persisted reports: %+v", reports)
	}
}

func TestFireFailureRecorded(t *testing.T) {
	s := New(t.TempDir(), func(ctx context.Context, task Task) (string, error) {
		return "", errors.New("engine exploded")
	}, nil)

	_ = s.AddTask(Task{ID: "m1", Name: "manual", Trigger: EventManual, Prompt: "hello"})
	report, err := s.Fire(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Err == "" {
		t.Fatalf("failure must be honest: %+v", report)
	}
}

func TestFireRefusesConcurrentRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	s := New(t.TempDir(), func(ctx context.Context, task Task) (string, error) {
		close(started)
		<-release
		return "done", nil
	}, nil)

	_ = s.AddTask(Task{ID: "m1", Name: "manual", Trigger: EventManual, Prompt: "hello"})

	// v1.2.5 repair: the first Fire() runs on a goroutine and its result is
	// collected through a channel. The test MUST wait for that completion
	// before returning: Fire's epilogue persists the report into the temp
	// dir, and an early return raced t.TempDir()'s RemoveAll cleanup
	// ("directory not empty"). Result ownership is deterministic — no sleep.
	type fireResult struct {
		report Report
		err    error
	}

	done := make(chan fireResult, 1)

	go func() {
		report, err := s.Fire(context.Background(), "m1")
		done <- fireResult{report: report, err: err}
	}()
	<-started

	if _, err := s.Fire(context.Background(), "m1"); err == nil {
		t.Fatal("concurrent fire of the same task must be refused")
	}
	close(release)

	first := <-done
	if first.err != nil {
		t.Fatalf("first fire: %v", first.err)
	}
	if !first.report.OK || first.report.Output != "done" {
		t.Fatalf("first fire report: %+v", first.report)
	}
}

func TestTimerTickFiresDueTasks(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, func(ctx context.Context, task Task) (string, error) {
		return "ran " + task.ID, nil
	}, nil)

	if err := s.AddTask(Task{ID: "tick1", Name: "timer", Trigger: EventTimer, Interval: 5 * time.Minute, Prompt: "p"}); err != nil {
		t.Fatal(err)
	}

	// Force the due time into the past.
	s.mu.Lock()
	for _, t := range s.tasks {
		if t.ID == "tick1" {
			t.NextDue = time.Now().Add(-time.Second)
		}
	}
	s.mu.Unlock()

	reports := s.Tick(context.Background())
	if len(reports) != 1 || !reports[0].OK {
		t.Fatalf("tick reports: %+v", reports)
	}
	if reports[0].Trigger != EventTimer {
		t.Fatalf("trigger = %s", reports[0].Trigger)
	}

	// The next due time must be pushed one interval forward.
	tasks := s.Tasks()
	if tasks[0].NextDue.Before(time.Now()) {
		t.Fatal("next due not advanced")
	}
}

func TestRemoveTaskCancels(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	_ = s.AddTask(Task{ID: "gone", Name: "x", Trigger: EventManual, Prompt: "p"})
	if err := s.RemoveTask("gone"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveTask("gone"); err == nil {
		t.Fatal("removing twice must fail")
	}
}
