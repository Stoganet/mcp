package tools_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iter"
	"net"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/jsonstream"
	networkapi "github.com/moby/moby/api/types/network"
	moby "github.com/moby/moby/client"

	"github.com/Stoganet/mcp/internal/tools"
)

type mockDockerClient struct {
	containerListFn    func(ctx context.Context, opts moby.ContainerListOptions) (moby.ContainerListResult, error)
	containerStatsFn   func(ctx context.Context, id string, opts moby.ContainerStatsOptions) (moby.ContainerStatsResult, error)
	containerLogsFn    func(ctx context.Context, id string, opts moby.ContainerLogsOptions) (moby.ContainerLogsResult, error)
	containerInspectFn func(ctx context.Context, id string, opts moby.ContainerInspectOptions) (moby.ContainerInspectResult, error)
	containerRestartFn func(ctx context.Context, id string, opts moby.ContainerRestartOptions) (moby.ContainerRestartResult, error)
	containerTopFn     func(ctx context.Context, id string, opts moby.ContainerTopOptions) (moby.ContainerTopResult, error)
	execCreateFn       func(ctx context.Context, id string, opts moby.ExecCreateOptions) (moby.ExecCreateResult, error)
	execAttachFn       func(ctx context.Context, execID string, opts moby.ExecAttachOptions) (moby.ExecAttachResult, error)
	execInspectFn      func(ctx context.Context, execID string, opts moby.ExecInspectOptions) (moby.ExecInspectResult, error)
	imagePullFn        func(ctx context.Context, ref string, opts moby.ImagePullOptions) (moby.ImagePullResponse, error)
	imageInspectFn     func(ctx context.Context, id string, opts ...moby.ImageInspectOption) (moby.ImageInspectResult, error)
}

func (m *mockDockerClient) ContainerList(ctx context.Context, opts moby.ContainerListOptions) (moby.ContainerListResult, error) {
	return m.containerListFn(ctx, opts)
}
func (m *mockDockerClient) ContainerStats(ctx context.Context, id string, opts moby.ContainerStatsOptions) (moby.ContainerStatsResult, error) {
	return m.containerStatsFn(ctx, id, opts)
}
func (m *mockDockerClient) ContainerLogs(ctx context.Context, id string, opts moby.ContainerLogsOptions) (moby.ContainerLogsResult, error) {
	return m.containerLogsFn(ctx, id, opts)
}
func (m *mockDockerClient) ContainerInspect(ctx context.Context, id string, opts moby.ContainerInspectOptions) (moby.ContainerInspectResult, error) {
	return m.containerInspectFn(ctx, id, opts)
}
func (m *mockDockerClient) ContainerRestart(ctx context.Context, id string, opts moby.ContainerRestartOptions) (moby.ContainerRestartResult, error) {
	return m.containerRestartFn(ctx, id, opts)
}
func (m *mockDockerClient) ContainerTop(ctx context.Context, id string, opts moby.ContainerTopOptions) (moby.ContainerTopResult, error) {
	return m.containerTopFn(ctx, id, opts)
}
func (m *mockDockerClient) ExecCreate(ctx context.Context, id string, opts moby.ExecCreateOptions) (moby.ExecCreateResult, error) {
	return m.execCreateFn(ctx, id, opts)
}
func (m *mockDockerClient) ExecAttach(ctx context.Context, execID string, opts moby.ExecAttachOptions) (moby.ExecAttachResult, error) {
	return m.execAttachFn(ctx, execID, opts)
}
func (m *mockDockerClient) ExecInspect(ctx context.Context, execID string, opts moby.ExecInspectOptions) (moby.ExecInspectResult, error) {
	return m.execInspectFn(ctx, execID, opts)
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

type fakeConn struct{}

func (fakeConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (fakeConn) Write([]byte) (int, error)        { return 0, nil }
func (fakeConn) Close() error                     { return nil }
func (fakeConn) LocalAddr() net.Addr              { return nil }
func (fakeConn) RemoteAddr() net.Addr             { return nil }
func (fakeConn) SetDeadline(time.Time) error      { return nil }
func (fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (fakeConn) SetWriteDeadline(time.Time) error { return nil }

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

func makeListFn(service, id string) func(context.Context, moby.ContainerListOptions) (moby.ContainerListResult, error) {
	return func(_ context.Context, _ moby.ContainerListOptions) (moby.ContainerListResult, error) {
		return moby.ContainerListResult{
			Items: []containerapi.Summary{{
				ID:     id,
				Names:  []string{"/" + service},
				Image:  "test-image:latest",
				Status: "Up 5 minutes",
				State:  containerapi.ContainerState("running"),
				Labels: map[string]string{
					"com.docker.compose.project": "services",
					"com.docker.compose.service": service,
				},
			}},
		}, nil
	}
}

func noStats(_ context.Context, _ string, _ moby.ContainerStatsOptions) (moby.ContainerStatsResult, error) {
	body, _ := json.Marshal(containerapi.StatsResponse{})
	return moby.ContainerStatsResult{Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func TestDockerPS(t *testing.T) {
	mc := &mockDockerClient{
		containerListFn: func(_ context.Context, _ moby.ContainerListOptions) (moby.ContainerListResult, error) {
			return moby.ContainerListResult{
				Items: []containerapi.Summary{
					{
						ID: "abc123456789", Names: []string{"/jellyfin"}, Image: "jellyfin/jellyfin:latest",
						Status: "Up 2 hours", State: containerapi.ContainerState("running"),
						Labels: map[string]string{"com.docker.compose.service": "jellyfin"},
					},
					{
						ID: "def456789012", Names: []string{"/sonarr"}, Image: "linuxserver/sonarr:latest",
						Status: "Up 2 hours", State: containerapi.ContainerState("running"),
						Labels: map[string]string{"com.docker.compose.service": "sonarr"},
					},
				},
			}, nil
		},
		containerStatsFn: noStats,
	}

	_, handler := tools.DockerPS(mc, "services")
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
}

func TestDockerLogs(t *testing.T) {
	const logText = "2026-01-01 info started\n2026-01-01 info ready\n"
	mc := &mockDockerClient{
		containerListFn: makeListFn("jellyfin", "abc123"),
		containerLogsFn: func(_ context.Context, _ string, _ moby.ContainerLogsOptions) (moby.ContainerLogsResult, error) {
			return fakeReadCloser{bytes.NewReader([]byte(logText))}, nil
		},
	}

	_, handler := tools.DockerLogs(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "jellyfin", "tail": float64(10)}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		Container string   `json:"container"`
		Lines     []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Container != "jellyfin" {
		t.Errorf("container = %q, want jellyfin", got.Container)
	}
	if len(got.Lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(got.Lines))
	}
}

func TestDockerLogs_grep(t *testing.T) {
	logData := "2026-01-01 info started\n2026-01-01 error failed\n2026-01-01 info ready\n"
	mc := &mockDockerClient{
		containerListFn: makeListFn("sonarr", "def456"),
		containerLogsFn: func(_ context.Context, _ string, _ moby.ContainerLogsOptions) (moby.ContainerLogsResult, error) {
			return fakeReadCloser{bytes.NewReader([]byte(logData))}, nil
		},
	}

	_, handler := tools.DockerLogs(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "sonarr", "grep": "error"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Lines) != 1 {
		t.Errorf("expected 1 line after grep, got %d: %v", len(got.Lines), got.Lines)
	}
}

func TestDockerInspect(t *testing.T) {
	mc := &mockDockerClient{
		containerListFn: makeListFn("radarr", "ghi789"),
		containerInspectFn: func(_ context.Context, _ string, _ moby.ContainerInspectOptions) (moby.ContainerInspectResult, error) {
			return moby.ContainerInspectResult{
				Container: containerapi.InspectResponse{
					Name:   "/radarr",
					Config: &containerapi.Config{Image: "linuxserver/radarr:latest"},
					NetworkSettings: &containerapi.NetworkSettings{
						Networks: map[string]*networkapi.EndpointSettings{
							"services_internal": {},
						},
					},
					HostConfig: &containerapi.HostConfig{
						Resources: containerapi.Resources{Memory: 384 * 1024 * 1024},
					},
				},
			}, nil
		},
	}

	_, handler := tools.DockerInspect(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "radarr"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		Name          string `json:"name"`
		MemoryLimitMB int64  `json:"memory_limit_mb"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "radarr" {
		t.Errorf("name = %q, want radarr", got.Name)
	}
	if got.MemoryLimitMB != 384 {
		t.Errorf("memory_limit_mb = %d, want 384", got.MemoryLimitMB)
	}
}

func TestDockerRestart(t *testing.T) {
	mc := &mockDockerClient{
		containerListFn: makeListFn("sonarr", "def456"),
		containerRestartFn: func(_ context.Context, _ string, _ moby.ContainerRestartOptions) (moby.ContainerRestartResult, error) {
			return moby.ContainerRestartResult{}, nil
		},
	}

	_, handler := tools.DockerRestart(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"names": []any{"sonarr"}}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		Restarted []string `json:"restarted"`
		Failed    []any    `json:"failed"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Restarted) != 1 || got.Restarted[0] != "sonarr" {
		t.Errorf("restarted = %v, want [sonarr]", got.Restarted)
	}
	if len(got.Failed) != 0 {
		t.Errorf("expected no failures, got %v", got.Failed)
	}
}

func TestDockerRestart_notFound(t *testing.T) {
	mc := &mockDockerClient{
		containerListFn: func(_ context.Context, _ moby.ContainerListOptions) (moby.ContainerListResult, error) {
			return moby.ContainerListResult{}, nil
		},
	}

	_, handler := tools.DockerRestart(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"names": []any{"nonexistent"}}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		Failed []any `json:"failed"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Failed) != 1 {
		t.Errorf("expected 1 failure, got %d", len(got.Failed))
	}
}

func TestDockerPull(t *testing.T) {
	callCount := 0
	mc := &mockDockerClient{
		containerListFn: makeListFn("jellyfin", "abc123"),
		containerInspectFn: func(_ context.Context, _ string, _ moby.ContainerInspectOptions) (moby.ContainerInspectResult, error) {
			return moby.ContainerInspectResult{
				Container: containerapi.InspectResponse{Config: &containerapi.Config{Image: "jellyfin/jellyfin:latest"}},
			}, nil
		},
		imageInspectFn: func(_ context.Context, _ string, _ ...moby.ImageInspectOption) (moby.ImageInspectResult, error) {
			callCount++
			r := moby.ImageInspectResult{}
			if callCount == 1 {
				r.RepoDigests = []string{"jellyfin/jellyfin@sha256:aaa"}
			} else {
				r.RepoDigests = []string{"jellyfin/jellyfin@sha256:bbb"}
			}
			return r, nil
		},
		imagePullFn: func(_ context.Context, _ string, _ moby.ImagePullOptions) (moby.ImagePullResponse, error) {
			return fakePullResponse{io.NopCloser(bytes.NewReader(nil))}, nil
		},
	}

	_, handler := tools.DockerPull(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "jellyfin"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		DigestChanged bool `json:"digest_changed"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.DigestChanged {
		t.Error("expected digest_changed=true")
	}
}

func TestDockerExec(t *testing.T) {
	mc := &mockDockerClient{
		containerListFn: makeListFn("sonarr", "def456"),
		execCreateFn: func(_ context.Context, _ string, _ moby.ExecCreateOptions) (moby.ExecCreateResult, error) {
			return moby.ExecCreateResult{ID: "exec123"}, nil
		},
		execAttachFn: func(_ context.Context, _ string, _ moby.ExecAttachOptions) (moby.ExecAttachResult, error) {
			r := moby.ExecAttachResult{}
			r.Conn = fakeConn{}
			r.Reader = bufio.NewReader(bytes.NewReader(nil))
			return r, nil
		},
		execInspectFn: func(_ context.Context, _ string, _ moby.ExecInspectOptions) (moby.ExecInspectResult, error) {
			return moby.ExecInspectResult{ExitCode: 0}, nil
		},
	}

	_, handler := tools.DockerExec(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "sonarr", "command": []any{"echo", "hello"}}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		ExitCode int `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", got.ExitCode)
	}
}

func TestDockerTop(t *testing.T) {
	mc := &mockDockerClient{
		containerListFn: makeListFn("bazarr", "jkl012"),
		containerTopFn: func(_ context.Context, _ string, _ moby.ContainerTopOptions) (moby.ContainerTopResult, error) {
			return moby.ContainerTopResult{
				Titles:    []string{"PID", "USER", "CMD"},
				Processes: [][]string{{"1", "root", "s6-svscan"}, {"149", "abc", "python3 bazarr.py"}, {"162", "abc", "[python3] <defunct>"}},
			}, nil
		},
	}

	_, handler := tools.DockerTop(mc, "services")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "bazarr"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := assertTextResult(t, result)

	var got struct {
		Processes []struct {
			Zombie bool `json:"zombie"`
		} `json:"processes"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Processes) != 3 {
		t.Fatalf("expected 3 processes, got %d", len(got.Processes))
	}
	if !got.Processes[2].Zombie {
		t.Error("expected process[2] to be flagged as zombie")
	}
	if got.Processes[0].Zombie {
		t.Error("expected process[0] to not be a zombie")
	}
}
