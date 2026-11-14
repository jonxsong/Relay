package relaymcp

import (
	"testing"
	"time"
)

func TestParseConnectionResolvesSecretReferences(t *testing.T) {
	connection, err := parseConnection(connectionFileConfig{
		Name: "crm", Endpoint: "https://mcp.example.com/mcp",
		AllowedTools: map[string]ToolPolicy{"lookup": {}},
		HeaderEnv:    map[string]string{"Authorization": "CRM_TOKEN"},
		Timeout:      "12s",
	}, func(name string) (string, bool) {
		if name != "CRM_TOKEN" {
			t.Fatalf("unexpected secret name %q", name)
		}
		return "Bearer test-secret", true
	})
	if err != nil {
		t.Fatalf("parse connection: %v", err)
	}
	if connection.Headers.Get("Authorization") != "Bearer test-secret" || connection.Timeout != 12*time.Second {
		t.Fatalf("unexpected connection: %#v", connection)
	}
}

func TestParseConnectionRejectsMissingSecret(t *testing.T) {
	_, err := parseConnection(connectionFileConfig{
		Name: "crm", Endpoint: "https://mcp.example.com/mcp",
		AllowedTools: map[string]ToolPolicy{"lookup": {}},
		HeaderEnv:    map[string]string{"Authorization": "CRM_TOKEN"},
	}, func(string) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("expected missing secret error")
	}
}

func TestValidateEndpointRejectsSSRFPrimitives(t *testing.T) {
	tests := []string{
		"http://example.com/mcp",
		"https://localhost/mcp",
		"https://127.0.0.1/mcp",
		"https://169.254.169.254/latest/meta-data",
		"https://user:password@example.com/mcp",
		"https://example.com/mcp?target=http://localhost",
	}
	for _, endpoint := range tests {
		if _, err := ValidateEndpoint(endpoint, false); err == nil {
			t.Errorf("ValidateEndpoint(%q) error = nil", endpoint)
		}
	}
	if _, err := ValidateEndpoint("https://mcp.example.com/mcp", false); err != nil {
		t.Fatalf("public HTTPS endpoint rejected: %v", err)
	}
}

func TestValidateEndpointAllowsExplicitPrivateDevelopmentEndpoint(t *testing.T) {
	if _, err := ValidateEndpoint("http://127.0.0.1:8080/mcp", true); err != nil {
		t.Fatalf("explicit private endpoint rejected: %v", err)
	}
}
