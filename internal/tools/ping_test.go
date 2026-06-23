package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Stoganet/mcp/internal/tools"
)

func TestPing(t *testing.T) {
	_, handler := tools.Ping("test-server", "1.2.3")

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success result, got error")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content, got empty")
	}

	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var got struct {
		Status  string `json:"status"`
		Server  string `json:"server"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Status != "ok" {
		t.Errorf("status = %q, want %q", got.Status, "ok")
	}
	if got.Server != "test-server" {
		t.Errorf("server = %q, want %q", got.Server, "test-server")
	}
	if got.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", got.Version, "1.2.3")
	}
}
