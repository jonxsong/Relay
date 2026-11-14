package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var inputReferencePattern = regexp.MustCompile(`^\$(?:\.input(?:\.[A-Za-z_][A-Za-z0-9_]*|\[[0-9]+\])*|\.nodes\.([A-Za-z][A-Za-z0-9_-]{0,127})\.output(?:\.[A-Za-z_][A-Za-z0-9_]*|\[[0-9]+\])*)$`)

func (b InputBinding) Validate() error {
	hasFrom := strings.TrimSpace(b.From) != ""
	hasValue := b.Value != nil
	if hasFrom == hasValue {
		return errors.New("must set exactly one of value or from")
	}
	if !hasFrom {
		return nil
	}
	if len(b.From) > 512 {
		return errors.New("reference must not exceed 512 characters")
	}
	if !inputReferencePattern.MatchString(b.From) {
		return errors.New("from must be a restricted reference rooted at $.input or $.nodes.<id>.output")
	}
	return nil
}

func (b InputBinding) ReferencedNode() (string, bool) {
	match := inputReferencePattern.FindStringSubmatch(b.From)
	if len(match) != 2 || match[1] == "" {
		return "", false
	}
	return match[1], true
}

func ValidateBindingDependencies(node NodeDefinition) error {
	dependencies := make(map[string]struct{}, len(node.DependsOn))
	for _, id := range node.DependsOn {
		dependencies[id] = struct{}{}
	}
	for name, binding := range node.Inputs {
		id, referencesNode := binding.ReferencedNode()
		if !referencesNode {
			continue
		}
		if _, allowed := dependencies[id]; !allowed {
			return fmt.Errorf("input %q references node %q which is not a declared dependency", name, id)
		}
	}
	return nil
}
