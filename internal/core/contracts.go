package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ContractValidator validates node input/output data against JSON Schema contracts.
type ContractValidator struct {
	compiler *jsonschema.Compiler
}

// NewContractValidator creates a new contract validator.
func NewContractValidator() *ContractValidator {
	c := jsonschema.NewCompiler()
	return &ContractValidator{compiler: c}
}

// ValidateInput validates data against a node's input schema.
func (cv *ContractValidator) ValidateInput(node *NodeDefinition, data map[string]any) error {
	return cv.validate(node.ID, "input", node.InputSchema, data)
}

// ValidateOutput validates data against a node's output schema.
func (cv *ContractValidator) ValidateOutput(node *NodeDefinition, data map[string]any) error {
	return cv.validate(node.ID, "output", node.OutputSchema, data)
}

// validate performs JSON Schema validation on the given data.
func (cv *ContractValidator) validate(nodeID, direction string, schema json.RawMessage, data map[string]any) error {
	if len(schema) == 0 || string(schema) == "null" || string(schema) == "{}" {
		return nil // No schema defined, skip validation
	}

	schemaURI := fmt.Sprintf("genoma://nodes/%s/%s", nodeID, direction)

	// Unmarshal schema
	var schemaObj any
	if err := json.Unmarshal(schema, &schemaObj); err != nil {
		return &ErrContractViolation{
			NodeID:    nodeID,
			Direction: direction,
			Details:   fmt.Sprintf("invalid JSON Schema: %v", err),
		}
	}

	// Compile schema
	if err := cv.compiler.AddResource(schemaURI, schemaObj); err != nil {
		return &ErrContractViolation{
			NodeID:    nodeID,
			Direction: direction,
			Details:   fmt.Sprintf("failed to compile schema: %v", err),
		}
	}

	sch, err := cv.compiler.Compile(schemaURI)
	if err != nil {
		return &ErrContractViolation{
			NodeID:    nodeID,
			Direction: direction,
			Details:   fmt.Sprintf("failed to compile schema: %v", err),
		}
	}

	// Validate data
	if err := sch.Validate(data); err != nil {
		details := formatValidationError(err)
		return &ErrContractViolation{
			NodeID:    nodeID,
			Direction: direction,
			Details:   details,
		}
	}

	return nil
}

// ValidateSchema checks if a given JSON Schema is itself valid.
func (cv *ContractValidator) ValidateSchema(schema json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	var obj any
	if err := json.Unmarshal(schema, &obj); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := cv.compiler.AddResource("genoma://validate-schema", obj); err != nil {
		return fmt.Errorf("invalid JSON Schema: %w", err)
	}
	_, err := cv.compiler.Compile("genoma://validate-schema")
	return err
}

// formatValidationError formats a jsonschema validation error into a readable string.
func formatValidationError(err error) string {
	if err == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(err.Error())
	return sb.String()
}
