package domain

import (
	"strings"
	"testing"
)

func TestResolveInputs(t *testing.T) {
	t.Parallel()
	node := NodeDefinition{
		ID: "summarize", Type: NodeTypeLLM, DependsOn: []string{"fetch"},
		Inputs: map[string]InputBinding{
			"topic": {From: "$.input.topic"},
			"title": {From: "$.nodes.fetch.output.items[1].title"},
			"limit": {Value: float64(3)},
		},
	}
	resolved, err := ResolveInputs(node, BindingContext{
		RunInput: map[string]any{"topic": "reliability"},
		NodeOutputs: map[string]any{"fetch": map[string]any{"items": []any{
			map[string]any{"title": "first"}, map[string]any{"title": "second"},
		}}},
	})
	if err != nil {
		t.Fatalf("ResolveInputs() error = %v", err)
	}
	if resolved["topic"] != "reliability" || resolved["title"] != "second" || resolved["limit"] != float64(3) {
		t.Fatalf("ResolveInputs() = %#v", resolved)
	}
}

func TestResolveInputsRejectsMissingAndOutOfBoundsValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		reference string
		want      string
	}{
		{name: "missing field", reference: "$.input.missing", want: "does not exist"},
		{name: "missing output", reference: "$.nodes.fetch.output.value", want: "no available output"},
		{name: "out of bounds", reference: "$.input.items[2]", want: "out of bounds"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			node := NodeDefinition{ID: "next", Type: NodeTypeLLM, DependsOn: []string{"fetch"}, Inputs: map[string]InputBinding{"value": {From: test.reference}}}
			_, err := ResolveInputs(node, BindingContext{RunInput: map[string]any{"items": []any{"only"}}, NodeOutputs: map[string]any{}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ResolveInputs() error = %v, want substring %q", err, test.want)
			}
		})
	}
}
