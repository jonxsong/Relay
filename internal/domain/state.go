package domain

import "fmt"

type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

type NodeStatus string

const (
	NodePending   NodeStatus = "pending"
	NodeReady     NodeStatus = "ready"
	NodeQueued    NodeStatus = "queued"
	NodeRunning   NodeStatus = "running"
	NodeSucceeded NodeStatus = "succeeded"
	NodeFailed    NodeStatus = "failed"
	NodeCancelled NodeStatus = "cancelled"
	NodeSkipped   NodeStatus = "skipped"
)

var allowedRunTransitions = map[RunStatus]map[RunStatus]struct{}{
	RunPending: {RunRunning: {}, RunFailed: {}, RunCancelled: {}},
	RunRunning: {RunSucceeded: {}, RunFailed: {}, RunCancelled: {}},
}

var allowedNodeTransitions = map[NodeStatus]map[NodeStatus]struct{}{
	NodePending: {NodeReady: {}, NodeCancelled: {}, NodeSkipped: {}},
	NodeReady:   {NodeQueued: {}, NodeCancelled: {}, NodeSkipped: {}},
	NodeQueued:  {NodeRunning: {}, NodeCancelled: {}},
	NodeRunning: {NodeReady: {}, NodeSucceeded: {}, NodeFailed: {}, NodeCancelled: {}},
}

func ValidateRunTransition(from, to RunStatus) error {
	if _, ok := allowedRunTransitions[from][to]; !ok {
		return fmt.Errorf("invalid run transition %q -> %q", from, to)
	}
	return nil
}

func ValidateNodeTransition(from, to NodeStatus) error {
	if _, ok := allowedNodeTransitions[from][to]; !ok {
		return fmt.Errorf("invalid node transition %q -> %q", from, to)
	}
	return nil
}

func (s RunStatus) Terminal() bool {
	return s == RunSucceeded || s == RunFailed || s == RunCancelled
}

func (s RunStatus) CanSchedule() bool {
	return s == RunPending || s == RunRunning
}

func (s NodeStatus) Terminal() bool {
	return s == NodeSucceeded || s == NodeFailed || s == NodeCancelled || s == NodeSkipped
}
