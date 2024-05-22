// Package servicedef contains a definition struct for
// operations available in a particular service.
package servicedef

import (
	"github.com/invopop/jsonschema"
)

type Definitions struct {
	Services []Service `json:"services"`
}

type Service struct {
	ID          string      `json:"id"`
	CLIName     string      `json:"cliName"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Operations  []Operation `json:"operations"`
}

type Operation struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	CLIName     string      `json:"cliName"`
	Description string      `json:"description"`
	RoutingRule RoutingRule `json:"routingRule"`

	RequestBody *RootSchema `json:"requestBody"`

	// ResponseBody maps the HTTP response status codes
	// to the expected body schema.
	ResponseBody map[string]jsonschema.Schema `json:"responses"`
}

type RoutingRule struct {
	Type   string `json:"type"`
	Path   string `json:"path"`
	Method string `json:"method"`

	// Target is used for AWS routing rules
	Target string `json:"target,omitempty"`
}

type RootSchema struct {
	Schema jsonschema.Schema `json:"schema"`
}
