package tools

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

type pingResult struct {
	Status  string `json:"status"`
	Server  string `json:"server"`
	Version string `json:"version"`
}

func Ping(serverName, version string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("ping",
		mcp.WithDescription("Check MCP server health"),
	)
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		b, err := json.Marshal(pingResult{
			Status:  "ok",
			Server:  serverName,
			Version: version,
		})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr // tool errors are returned as MCP results, not Go errors
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}
