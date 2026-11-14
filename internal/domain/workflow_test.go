package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateWorkflowAcceptsParallelDAG(t *testing.T) {
	t.Parallel()
	workflow := WorkflowDefinition{Name: "research", Nodes: []NodeDefinition{
		{ID: "start", Type: NodeTypeTransform},
		{ID: "research_a", Type: NodeTypeLLM, DependsOn: []string{"start"}, Inputs: map[string]InputBinding{"topic": {From: "$.nodes.start.output.topic"}}},
		{ID: "research_b", Type: NodeTypeLLM, DependsOn: []string{"start"}},
		{ID: "join", Type: NodeTypeJoin, DependsOn: []string{"research_a", "research_b"}},
	}}
	if err := ValidateWorkflow(workflow, DefaultWorkflowLimits); err != nil {
		t.Fatalf("ValidateWorkflow() error = %v", err)
	}
}

func TestValidateWorkflowRejectsCycle(t *testing.T) {
	t.Parallel()
	workflow := WorkflowDefinition{Name: "cycle", Nodes: []NodeDefinition{
		{ID: "a", Type: NodeTypeLLM, DependsOn: []string{"c"}},
		{ID: "b", Type: NodeTypeLLM, DependsOn: []string{"a"}},
		{ID: "c", Type: NodeTypeLLM, DependsOn: []string{"b"}},
	}}
	err := ValidateWorkflow(workflow, DefaultWorkflowLimits)
	if err == nil || !strings.Contains(err.Error(), "contains cycle") {
		t.Fatalf("ValidateWorkflow() error = %v, want cycle error", err)
	}
}

func TestValidateWorkflowRejectsUnknownAndDuplicateDependencies(t *testing.T) {
	t.Parallel()
	workflow := WorkflowDefinition{Name: "invalid", Nodes: []NodeDefinition{
		{ID: "a", Type: NodeTypeLLM, DependsOn: []string{"missing", "missing"}},
	}}
	err := ValidateWorkflow(workflow, DefaultWorkflowLimits)
	var validationErrors ValidationErrors
	if !errors.As(err, &validationErrors) {
		t.Fatalf("ValidateWorkflow() error type = %T, want ValidationErrors", err)
	}
	if !strings.Contains(err.Error(), "unknown node") || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("ValidateWorkflow() error = %v", err)
	}
}

func TestValidateWorkflowEnforcesDepth(t *testing.T) {
	t.Parallel()
	workflow := WorkflowDefinition{Name: "deep", Nodes: []NodeDefinition{
		{ID: "a", Type: NodeTypeLLM},
		{ID: "b", Type: NodeTypeLLM, DependsOn: []string{"a"}},
		{ID: "c", Type: NodeTypeLLM, DependsOn: []string{"b"}},
	}}
	err := ValidateWorkflow(workflow, WorkflowLimits{MaxNodes: 10, MaxDepth: 2})
	if err == nil || !strings.Contains(err.Error(), "graph depth 3 exceeds limit 2") {
		t.Fatalf("ValidateWorkflow() error = %v, want depth error", err)
	}
}

func TestValidateWorkflowEnforcesNodeTimeoutBounds(t *testing.T) {
	t.Parallel()
	for _, timeout := range []int32{-1, 3601} {
		workflow := WorkflowDefinition{Name: "timeout", Nodes: []NodeDefinition{{ID: "work", Type: NodeTypeLLM, TimeoutSeconds: timeout}}}
		err := ValidateWorkflow(workflow, DefaultWorkflowLimits)
		if err == nil || !strings.Contains(err.Error(), "timeout_seconds") {
			t.Fatalf("ValidateWorkflow(timeout=%d) error = %v, want timeout validation error", timeout, err)
		}
	}
	workflow := WorkflowDefinition{Name: "timeout", Nodes: []NodeDefinition{{ID: "work", Type: NodeTypeLLM, TimeoutSeconds: 30}}}
	if err := ValidateWorkflow(workflow, DefaultWorkflowLimits); err != nil {
		t.Fatalf("ValidateWorkflow(valid timeout) error = %v", err)
	}
}

func TestBindingAllowsOnlyRestrictedReferences(t *testing.T) {
	t.Parallel()
	valid := []string{"$.input", "$.input.customer.id", "$.input.items[0].name", "$.nodes.fetch.output", "$.nodes.fetch_data.output.items[2]"}
	for _, reference := range valid {
		if err := (InputBinding{From: reference}).Validate(); err != nil {
			t.Errorf("reference %q rejected: %v", reference, err)
		}
	}
	invalid := []string{"$", "$.env.SECRET", "$.nodes.fetch.secret", "$.nodes['fetch'].output", "$.nodes.fetch.output..x", "$(shell)"}
	for _, reference := range invalid {
		if err := (InputBinding{From: reference}).Validate(); err == nil {
			t.Errorf("reference %q accepted, want error", reference)
		}
	}
}

func TestBindingMustReferenceDeclaredDependency(t *testing.T) {
	t.Parallel()
	node := NodeDefinition{ID: "summarize", Type: NodeTypeLLM, DependsOn: []string{"fetch"}, Inputs: map[string]InputBinding{
		"body": {From: "$.nodes.other.output.body"},
	}}
	if err := ValidateBindingDependencies(node); err == nil {
		t.Fatal("ValidateBindingDependencies() error = nil, want undeclared dependency error")
	}
	workflow := WorkflowDefinition{Name: "invalid binding", Nodes: []NodeDefinition{
		{ID: "fetch", Type: NodeTypeTool},
		node,
	}}
	if err := ValidateWorkflow(workflow, DefaultWorkflowLimits); err == nil {
		t.Fatal("ValidateWorkflow() error = nil, want undeclared input dependency error")
	}
}
