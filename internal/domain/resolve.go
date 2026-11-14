package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type BindingContext struct {
	RunInput    map[string]any
	NodeOutputs map[string]any
}

func ResolveInputs(node NodeDefinition, context BindingContext) (map[string]any, error) {
	if err := ValidateBindingDependencies(node); err != nil {
		return nil, err
	}
	resolved := make(map[string]any, len(node.Inputs))
	for name, binding := range node.Inputs {
		if err := binding.Validate(); err != nil {
			return nil, fmt.Errorf("input %q: %w", name, err)
		}
		if binding.From == "" {
			resolved[name] = binding.Value
			continue
		}
		value, err := resolveReference(binding.From, context)
		if err != nil {
			return nil, fmt.Errorf("input %q: %w", name, err)
		}
		resolved[name] = value
	}
	return resolved, nil
}

func resolveReference(reference string, context BindingContext) (any, error) {
	match := inputReferencePattern.FindStringSubmatch(reference)
	if match == nil {
		return nil, errors.New("invalid restricted reference")
	}
	var current any
	var suffix string
	if strings.HasPrefix(reference, "$.input") {
		current = context.RunInput
		suffix = strings.TrimPrefix(reference, "$.input")
	} else {
		nodeID := match[1]
		var exists bool
		current, exists = context.NodeOutputs[nodeID]
		if !exists {
			return nil, fmt.Errorf("node %q has no available output", nodeID)
		}
		prefix := "$.nodes." + nodeID + ".output"
		suffix = strings.TrimPrefix(reference, prefix)
	}

	for suffix != "" {
		switch suffix[0] {
		case '.':
			suffix = suffix[1:]
			end := len(suffix)
			if dot := strings.IndexByte(suffix, '.'); dot >= 0 && dot < end {
				end = dot
			}
			if bracket := strings.IndexByte(suffix, '['); bracket >= 0 && bracket < end {
				end = bracket
			}
			key := suffix[:end]
			object, ok := current.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("cannot select field %q from %T", key, current)
			}
			var exists bool
			current, exists = object[key]
			if !exists {
				return nil, fmt.Errorf("field %q does not exist", key)
			}
			suffix = suffix[end:]
		case '[':
			closing := strings.IndexByte(suffix, ']')
			if closing < 2 {
				return nil, errors.New("invalid array index")
			}
			index, err := strconv.Atoi(suffix[1:closing])
			if err != nil {
				return nil, errors.New("invalid array index")
			}
			array, ok := current.([]any)
			if !ok {
				return nil, fmt.Errorf("cannot select index %d from %T", index, current)
			}
			if index < 0 || index >= len(array) {
				return nil, fmt.Errorf("array index %d is out of bounds", index)
			}
			current = array[index]
			suffix = suffix[closing+1:]
		default:
			return nil, errors.New("invalid reference suffix")
		}
	}
	return current, nil
}
