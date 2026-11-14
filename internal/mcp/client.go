package relaymcp

import (
	"context"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Session interface {
	ListTools(context.Context, *sdk.ListToolsParams) (*sdk.ListToolsResult, error)
	CallTool(context.Context, *sdk.CallToolParams) (*sdk.CallToolResult, error)
	Close() error
}

type Connector interface {
	Connect(context.Context, Connection) (Session, error)
}

type SDKConnector struct{}

func (SDKConnector) Connect(ctx context.Context, connection Connection) (Session, error) {
	httpClient, err := safeHTTPClient(connection)
	if err != nil {
		return nil, err
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "relay-worker", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:             connection.Endpoint,
		HTTPClient:           httpClient,
		MaxRetries:           1,
		DisableStandaloneSSE: true,
		MaxEventSize:         int(connection.MaxResultBytes),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to MCP server: %w", err)
	}
	return session, nil
}
