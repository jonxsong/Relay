package schema

import (
	"encoding/json"
	"errors"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func Compile(rawSchema any) (*jsonschema.Schema, error) {
	if rawSchema == nil {
		return nil, errors.New("schema is required")
	}
	schemaBytes, err := json.Marshal(rawSchema)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(schemaBytes, &normalized); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("relay://schema.json", normalized); err != nil {
		return nil, err
	}
	return compiler.Compile("relay://schema.json")
}

func Validate(rawSchema, value any) error {
	compiled, err := Compile(rawSchema)
	if err != nil {
		return err
	}
	return compiled.Validate(value)
}
