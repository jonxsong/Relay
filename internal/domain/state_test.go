package domain

import "testing"

func TestStateTransitions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		allowed bool
		runFrom RunStatus
		runTo   RunStatus
	}{
		{name: "start run", allowed: true, runFrom: RunPending, runTo: RunRunning},
		{name: "cancel run", allowed: true, runFrom: RunRunning, runTo: RunCancelled},
		{name: "cannot revive", allowed: false, runFrom: RunCancelled, runTo: RunRunning},
		{name: "cannot skip running", allowed: false, runFrom: RunRunning, runTo: RunPending},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRunTransition(test.runFrom, test.runTo)
			if (err == nil) != test.allowed {
				t.Fatalf("ValidateRunTransition(%q, %q) error = %v", test.runFrom, test.runTo, err)
			}
		})
	}

	if err := ValidateNodeTransition(NodeRunning, NodeReady); err != nil {
		t.Fatalf("retry transition rejected: %v", err)
	}
	if err := ValidateNodeTransition(NodeSucceeded, NodeRunning); err == nil {
		t.Fatal("terminal node revival accepted")
	}
}

func TestCancellationPreventsScheduling(t *testing.T) {
	t.Parallel()
	if RunCancelled.CanSchedule() {
		t.Fatal("cancelled run can schedule")
	}
	if !RunRunning.CanSchedule() {
		t.Fatal("running run cannot schedule")
	}
}
