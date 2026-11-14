package domain

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type NodeType string

const (
	NodeTypeLLM       NodeType = "llm"
	NodeTypeTool      NodeType = "tool"
	NodeTypeTransform NodeType = "transform"
	NodeTypeJoin      NodeType = "join"
)

type WorkflowLimits struct {
	MaxNodes int
	MaxDepth int
}

var DefaultWorkflowLimits = WorkflowLimits{MaxNodes: 250, MaxDepth: 64}

type WorkflowDefinition struct {
	Name  string           `json:"name"`
	Nodes []NodeDefinition `json:"nodes"`
}

type NodeDefinition struct {
	ID             string                  `json:"id"`
	Type           NodeType                `json:"type"`
	DependsOn      []string                `json:"depends_on,omitempty"`
	Inputs         map[string]InputBinding `json:"inputs,omitempty"`
	TimeoutSeconds int32                   `json:"timeout_seconds,omitempty"`
}

type InputBinding struct {
	Value any    `json:"value,omitempty"`
	From  string `json:"from,omitempty"`
}

type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Path + ": " + e.Message
}

type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	parts := make([]string, 0, len(e))
	for _, item := range e {
		parts = append(parts, item.Error())
	}
	return strings.Join(parts, "; ")
}

func (e ValidationErrors) Unwrap() []error {
	errs := make([]error, 0, len(e))
	for _, item := range e {
		errs = append(errs, item)
	}
	return errs
}

var nodeIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,127}$`)

func ValidateWorkflow(def WorkflowDefinition, limits WorkflowLimits) error {
	if limits.MaxNodes <= 0 || limits.MaxDepth <= 0 {
		return errors.New("workflow limits must be positive")
	}

	var problems ValidationErrors
	if strings.TrimSpace(def.Name) == "" {
		problems = append(problems, ValidationError{Path: "name", Message: "is required"})
	}
	if len(def.Nodes) == 0 {
		problems = append(problems, ValidationError{Path: "nodes", Message: "must contain at least one node"})
	}
	if len(def.Nodes) > limits.MaxNodes {
		problems = append(problems, ValidationError{Path: "nodes", Message: fmt.Sprintf("must contain at most %d nodes", limits.MaxNodes)})
	}

	byID := make(map[string]NodeDefinition, len(def.Nodes))
	for i, node := range def.Nodes {
		path := fmt.Sprintf("nodes[%d]", i)
		if !nodeIDPattern.MatchString(node.ID) {
			problems = append(problems, ValidationError{Path: path + ".id", Message: "must start with a letter and contain only letters, digits, underscores, or hyphens (max 128 characters)"})
		} else if _, exists := byID[node.ID]; exists {
			problems = append(problems, ValidationError{Path: path + ".id", Message: "must be unique"})
		} else {
			byID[node.ID] = node
		}

		switch node.Type {
		case NodeTypeLLM, NodeTypeTool, NodeTypeTransform, NodeTypeJoin:
		default:
			problems = append(problems, ValidationError{Path: path + ".type", Message: "must be one of llm, tool, transform, or join"})
		}
		if node.TimeoutSeconds < 0 || node.TimeoutSeconds > 3600 {
			problems = append(problems, ValidationError{Path: path + ".timeout_seconds", Message: "must be between 1 and 3600 when provided"})
		}

		seenDependency := make(map[string]struct{}, len(node.DependsOn))
		for j, dependency := range node.DependsOn {
			dependencyPath := fmt.Sprintf("%s.depends_on[%d]", path, j)
			if dependency == node.ID {
				problems = append(problems, ValidationError{Path: dependencyPath, Message: "cannot reference the node itself"})
			}
			if _, duplicate := seenDependency[dependency]; duplicate {
				problems = append(problems, ValidationError{Path: dependencyPath, Message: "must not be duplicated"})
			}
			seenDependency[dependency] = struct{}{}
		}

		inputNames := make([]string, 0, len(node.Inputs))
		for inputName := range node.Inputs {
			inputNames = append(inputNames, inputName)
		}
		sort.Strings(inputNames)
		for _, inputName := range inputNames {
			binding := node.Inputs[inputName]
			if err := binding.Validate(); err != nil {
				problems = append(problems, ValidationError{Path: path + ".inputs." + inputName, Message: err.Error()})
			}
		}
	}

	for i, node := range def.Nodes {
		for j, dependency := range node.DependsOn {
			if _, exists := byID[dependency]; !exists {
				problems = append(problems, ValidationError{Path: fmt.Sprintf("nodes[%d].depends_on[%d]", i, j), Message: "references an unknown node"})
			}
		}
		if err := ValidateBindingDependencies(node); err != nil {
			problems = append(problems, ValidationError{Path: fmt.Sprintf("nodes[%d].inputs", i), Message: err.Error()})
		}
	}

	if len(problems) > 0 {
		return problems
	}
	if err := validateAcyclicAndDepth(def.Nodes, limits.MaxDepth); err != nil {
		return err
	}
	return nil
}

func validateAcyclicAndDepth(nodes []NodeDefinition, maxDepth int) error {
	byID := make(map[string]NodeDefinition, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}

	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(nodes))
	depth := make(map[string]int, len(nodes))
	stack := make([]string, 0, len(nodes))

	var visit func(string) (int, error)
	visit = func(id string) (int, error) {
		switch state[id] {
		case visiting:
			cycleStart := 0
			for i, candidate := range stack {
				if candidate == id {
					cycleStart = i
					break
				}
			}
			cycle := append(append([]string{}, stack[cycleStart:]...), id)
			return 0, ValidationError{Path: "nodes", Message: "contains cycle: " + strings.Join(cycle, " -> ")}
		case visited:
			return depth[id], nil
		}

		state[id] = visiting
		stack = append(stack, id)
		longest := 1
		for _, dependency := range byID[id].DependsOn {
			dependencyDepth, err := visit(dependency)
			if err != nil {
				return 0, err
			}
			if dependencyDepth+1 > longest {
				longest = dependencyDepth + 1
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = visited
		depth[id] = longest
		if longest > maxDepth {
			return 0, ValidationError{Path: "nodes", Message: fmt.Sprintf("graph depth %d exceeds limit %d", longest, maxDepth)}
		}
		return longest, nil
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
