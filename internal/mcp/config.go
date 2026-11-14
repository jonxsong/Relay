package relaymcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	defaultTimeout        = 30 * time.Second
	defaultMaxResultBytes = int64(1 << 20)
)

var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

type ToolPolicy struct {
	RequiresApproval bool `json:"requires_approval"`
}

type Connection struct {
	Name                string
	Endpoint            string
	AllowedTools        map[string]ToolPolicy
	Headers             http.Header
	AllowPrivateNetwork bool
	Timeout             time.Duration
	MaxResultBytes      int64
}

type fileConfig struct {
	Connections []connectionFileConfig `json:"connections"`
}

type connectionFileConfig struct {
	Name                string                `json:"name"`
	Endpoint            string                `json:"endpoint"`
	AllowedTools        map[string]ToolPolicy `json:"allowed_tools"`
	HeaderEnv           map[string]string     `json:"header_env,omitempty"`
	AllowPrivateNetwork bool                  `json:"allow_private_network,omitempty"`
	Timeout             string                `json:"timeout,omitempty"`
	MaxResultBytes      int64                 `json:"max_result_bytes,omitempty"`
}

func LoadConfig(path string) ([]Connection, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read MCP config: %w", err)
	}
	var file fileConfig
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode MCP config: %w", err)
	}
	if len(file.Connections) == 0 {
		return nil, errors.New("MCP config must define at least one connection")
	}
	connections := make([]Connection, 0, len(file.Connections))
	seen := make(map[string]struct{}, len(file.Connections))
	for _, item := range file.Connections {
		connection, err := parseConnection(item, os.LookupEnv)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[connection.Name]; exists {
			return nil, fmt.Errorf("duplicate MCP connection %q", connection.Name)
		}
		seen[connection.Name] = struct{}{}
		connections = append(connections, connection)
	}
	return connections, nil
}

func parseConnection(item connectionFileConfig, lookupEnv func(string) (string, bool)) (Connection, error) {
	item.Name = strings.TrimSpace(item.Name)
	if !safeName.MatchString(item.Name) {
		return Connection{}, errors.New("MCP connection name must be 1-64 safe characters")
	}
	if _, err := ValidateEndpoint(item.Endpoint, item.AllowPrivateNetwork); err != nil {
		return Connection{}, fmt.Errorf("MCP connection %q: %w", item.Name, err)
	}
	if len(item.AllowedTools) == 0 {
		return Connection{}, fmt.Errorf("MCP connection %q must allow at least one tool", item.Name)
	}
	for toolName := range item.AllowedTools {
		if !safeName.MatchString(toolName) {
			return Connection{}, fmt.Errorf("MCP connection %q has invalid tool name", item.Name)
		}
	}
	timeout := defaultTimeout
	if item.Timeout != "" {
		parsed, err := time.ParseDuration(item.Timeout)
		if err != nil || parsed < time.Second || parsed > 5*time.Minute {
			return Connection{}, fmt.Errorf("MCP connection %q timeout must be between 1s and 5m", item.Name)
		}
		timeout = parsed
	}
	maxResultBytes := item.MaxResultBytes
	if maxResultBytes == 0 {
		maxResultBytes = defaultMaxResultBytes
	}
	if maxResultBytes < 1024 || maxResultBytes > 10<<20 {
		return Connection{}, fmt.Errorf("MCP connection %q max_result_bytes must be between 1024 and 10485760", item.Name)
	}
	headers := make(http.Header, len(item.HeaderEnv))
	for name, environmentVariable := range item.HeaderEnv {
		canonicalName := http.CanonicalHeaderKey(strings.TrimSpace(name))
		if canonicalName == "" || canonicalName == "Host" || canonicalName == "Content-Length" {
			return Connection{}, fmt.Errorf("MCP connection %q has forbidden secret header", item.Name)
		}
		environmentVariable = strings.TrimSpace(environmentVariable)
		value, ok := lookupEnv(environmentVariable)
		if environmentVariable == "" || !ok || strings.TrimSpace(value) == "" {
			return Connection{}, fmt.Errorf("MCP connection %q secret environment variable is missing", item.Name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return Connection{}, fmt.Errorf("MCP connection %q secret header contains a newline", item.Name)
		}
		headers.Set(canonicalName, value)
	}
	return Connection{
		Name: item.Name, Endpoint: item.Endpoint, AllowedTools: item.AllowedTools, Headers: headers,
		AllowPrivateNetwork: item.AllowPrivateNetwork, Timeout: timeout, MaxResultBytes: maxResultBytes,
	}, nil
}
