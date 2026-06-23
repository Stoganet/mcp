package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iter"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/jsonstream"
	moby "github.com/moby/moby/client"

	"github.com/Stoganet/mcp/internal/tools"
)

type mockDockerClient struct {
	containerListFn    func(ctx context.Context, opts moby.ContainerListOptions) (moby.ContainerListResult, error)
	containerLogsFn    func(ctx context.Context, id string, opts moby.ContainerLogsOptions) (moby.ContainerLogsResult, error)
	containerRestartFn func(ctx context.Context, id string, opts moby.ContainerRestartOptions) (moby.ContainerRestartResult, error)
	containerInspectFn func(ctx context.Context, id string, opts moby.ContainerInspectOptions) (moby.ContainerInspectResult, error)
	imagePullFn        func(ctx context.Context, ref string, opts moby.ImagePullOptions) (moby.ImagePullResponse, error)
	imageInspectFn     func(ctx context.Context, id string, opts ...moby.ImageInspectOption) (moby.ImageInspectResult, error)
}

func (m *mockDockerClient) ContainerList(ctx context.Context, opts moby.ContainerListOptions) (moby.ContainerListResult, error) {
	return m.containerListFn(ctx, opts)
}
func (m *mockDockerClient) ContainerLogs(ctx context.Context, id string, opts moby.ContainerLogsOptions) (moby.ContainerLogsResult, error) {
	return m.containerLogsFn(ctx, id, opts)
}
func (m *mockDockerClient) ContainerRestart(ctx context.Context, id string, opts moby.ContainerRestartOptions) (moby.ContainerRestartResult, error) {
	return m.containerRestartFn(ctx, id, opts)
}
func (m *mockDockerClient) ContainerInspect(ctx context.Context, id string, opts moby.ContainerInspectOptions) (moby.ContainerInspectResult, error) {
	return m.containerInspectFn(ctx, id, opts)
}
func (m *mockDockerClient) ImagePull(ctx context.Context, ref string, opts moby.ImagePullOptions) (moby.ImagePullResponse, error) {
	return m.imagePullFn(ctx, ref, opts)
}
func (m *mockDockerClient) ImageInspect(ctx context.Context, id string, opts ...moby.ImageInspectOption) (moby.ImageInspectResult, error) {
	return m.imageInspectFn(ctx, id, opts...)
}

type fakeReadCloser struct{ *bytes.Reader }

func (f fakeReadCloser) Close() error { return nil }

type fakePullResponse struct{ io.ReadCloser }

func (f fakePullResponse) JSONMessages(_ context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(_ func(jsonstream.Message, error) bool) {}
}
func (f fakePullResponse) Wait(_ context.Context) error { return nil }

func assertTextResult(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result.IsError {
		tc, _ := result.Content[0].(mcp.TextContent)
		t.Fatalf("unexpected MCP error: %s", tc.Text)
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func makeListFn(project, service, id string) func(context.Context, moby.ContainerListOptions) (moby.ContainerListResult, error) {
	return func(_ context.Context, _ moby.ContainerListOptions) (moby.ContainerListResult, error) {
		return moby.ContainerListResult{
			Items: []containerapi.Summary{
				{
					ID:     id,
					Names:  []string{"/" + service},
					Image:  "test-image:latest",
					Status: "Up 5 minutes",
					State:  containerapi.ContainerState("running"),
					Labels: map[string]string{
						"com.docker.compose.project": project,
						"com.docker.compose.service": service,
					},
				},
			},
		}, nil
	}
}

func TestListContainers(t *testing.T) {
	mc := &mockDockerClient{
		containerListFn: func(_ context.Context, _ moby.ContainerListOptions) (moby.ContainerListResult, error) {
			return moby.ContainerListResult{
				Items: []containerapi.Summary{
					{
						ID: "abc123", Names: []string{"/jellyfin"}, Image: "jellyfin/jellyfin:latest",
						Status: "Up 2 hours", State: containerapi.ContainerState("running"),
						Labels: map[string]string{"com.docker.compose.service": "jellyfin"},
					},
					{
						ID: "def456", Names: []string{"/sonarr"}, Image: "linuxserver/sonarr:latest",
						Status: "Up 2 hours", State: containerapi.ContainerState("running"),
						Labels: map[string]string{"com.docker.compose.service": "sonarr"},
					},
				},
			}, nil
		},
	}

	_, handler := tools.ListContainers(mc, "services")
	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var entries []map[string]any
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
	if entries[0]["service"] != "jellyfin" {
		t.Errorf("entry[0].service = %q, want jellyfin", entries[0]["service"])
	}
	if entries[1]["service"] != "sonarr" {
		t.Errorf("entry[1].service = %q, want sonarr", entries[1]["service"])
	}
}

func TestGetLogs(t *testing.T) {
	const logText = "2026-01-01 info started\n2026-01-01 info ready\n"
	mc := &mockDockerClient{
		containerListFn: makeListFn("services", "jellyfin", "abc123"),
		containerLogsFn: func(_ context.Context, _ string, opts moby.ContainerLogsOptions) (moby.ContainerLogsResult, error) {
			if opts.Tail != "10" {
				return nil, nil
			}
			return fakeReadCloser{bytes.NewReader([]byte(logText))}, nil
		},
	}

	_, handler := tools.GetLogs(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"service": "jellyfin", "lines": float64(10)}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)
	if text != logText {
		t.Errorf("logs = %q, want %q", text, logText)
	}
}

func TestGetLogs_missingService(t *testing.T) {
	mc := &mockDockerClient{}
	_, handler := tools.GetLogs(mc, "services")
	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Error("expected MCP error for missing service argument")
	}
}

func TestRestartContainer(t *testing.T) {
	running := true
	mc := &mockDockerClient{
		containerListFn: makeListFn("services", "sonarr", "def456"),
		containerRestartFn: func(_ context.Context, _ string, _ moby.ContainerRestartOptions) (moby.ContainerRestartResult, error) {
			return moby.ContainerRestartResult{}, nil
		},
		containerInspectFn: func(_ context.Context, _ string, _ moby.ContainerInspectOptions) (moby.ContainerInspectResult, error) {
			return moby.ContainerInspectResult{
				Container: containerapi.InspectResponse{
					State: &containerapi.State{Running: running, Status: "running"},
				},
			}, nil
		},
	}

	_, handler := tools.RestartContainer(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"service": "sonarr"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		Service string `json:"service"`
		Running bool   `json:"running"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Service != "sonarr" {
		t.Errorf("service = %q, want sonarr", got.Service)
	}
	if !got.Running {
		t.Error("expected running=true")
	}
}

func TestRestartContainer_notInProject(t *testing.T) {
	mc := &mockDockerClient{
		containerListFn: func(_ context.Context, _ moby.ContainerListOptions) (moby.ContainerListResult, error) {
			return moby.ContainerListResult{Items: nil}, nil
		},
	}

	_, handler := tools.RestartContainer(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"service": "nonexistent"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Error("expected MCP error for container not in project")
	}
}

func TestPullImage(t *testing.T) {
	const (
		imageRef     = "jellyfin/jellyfin:latest"
		digestBefore = "jellyfin/jellyfin@sha256:aaa"
		digestAfter  = "jellyfin/jellyfin@sha256:bbb"
	)
	callCount := 0
	mc := &mockDockerClient{
		containerListFn: makeListFn("services", "jellyfin", "abc123"),
		containerInspectFn: func(_ context.Context, _ string, _ moby.ContainerInspectOptions) (moby.ContainerInspectResult, error) {
			return moby.ContainerInspectResult{
				Container: containerapi.InspectResponse{
					Config: &containerapi.Config{Image: imageRef},
				},
			}, nil
		},
		imageInspectFn: func(_ context.Context, _ string, _ ...moby.ImageInspectOption) (moby.ImageInspectResult, error) {
			callCount++
			digest := digestBefore
			if callCount > 1 {
				digest = digestAfter
			}
			r := moby.ImageInspectResult{}
			r.RepoDigests = []string{digest}
			return r, nil
		},
		imagePullFn: func(_ context.Context, _ string, _ moby.ImagePullOptions) (moby.ImagePullResponse, error) {
			return fakePullResponse{io.NopCloser(bytes.NewReader(nil))}, nil
		},
	}

	_, handler := tools.PullImage(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"service": "jellyfin"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		Service       string `json:"service"`
		DigestBefore  string `json:"digest_before"`
		DigestAfter   string `json:"digest_after"`
		DigestChanged bool   `json:"digest_changed"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.DigestChanged {
		t.Error("expected digest_changed=true")
	}
	if got.DigestBefore != digestBefore {
		t.Errorf("digest_before = %q, want %q", got.DigestBefore, digestBefore)
	}
	if got.DigestAfter != digestAfter {
		t.Errorf("digest_after = %q, want %q", got.DigestAfter, digestAfter)
	}
}
