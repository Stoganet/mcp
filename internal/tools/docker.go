package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/moby/moby/api/pkg/stdcopy"
	moby "github.com/moby/moby/client"
)

type DockerClient interface {
	ContainerList(ctx context.Context, options moby.ContainerListOptions) (moby.ContainerListResult, error)
	ContainerLogs(ctx context.Context, containerID string, options moby.ContainerLogsOptions) (moby.ContainerLogsResult, error)
	ContainerRestart(ctx context.Context, containerID string, options moby.ContainerRestartOptions) (moby.ContainerRestartResult, error)
	ContainerInspect(ctx context.Context, containerID string, options moby.ContainerInspectOptions) (moby.ContainerInspectResult, error)
	ImagePull(ctx context.Context, refStr string, options moby.ImagePullOptions) (moby.ImagePullResponse, error)
	ImageInspect(ctx context.Context, imageID string, inspectOpts ...moby.ImageInspectOption) (moby.ImageInspectResult, error)
}

func NewDockerClient(host string) (DockerClient, error) {
	opts := []moby.Opt{moby.FromEnv}
	if host != "" {
		opts = append(opts, moby.WithHost(host))
	}
	return moby.New(opts...)
}

func findContainer(ctx context.Context, dc DockerClient, project, service string) (string, *mcp.CallToolResult) {
	result, err := dc.ContainerList(ctx, moby.ContainerListOptions{
		All: true,
		Filters: make(moby.Filters).
			Add("label", "com.docker.compose.project="+project).
			Add("label", "com.docker.compose.service="+service),
	})
	if err != nil {
		return "", mcp.NewToolResultError("docker error: " + err.Error())
	}
	if len(result.Items) == 0 {
		return "", mcp.NewToolResultError(fmt.Sprintf("service %q not found in compose project %q", service, project))
	}
	return result.Items[0].ID, nil
}

func ListContainers(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("list_containers",
		mcp.WithDescription("List all containers in the compose stack with their status, uptime, and image"),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := dc.ContainerList(ctx, moby.ContainerListOptions{
			All:     true,
			Filters: make(moby.Filters).Add("label", "com.docker.compose.project="+project),
		})
		if err != nil {
			return mcp.NewToolResultError("docker error: " + err.Error()), nil //nolint:nilerr
		}

		type entry struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			State   string `json:"state"`
			Image   string `json:"image"`
			Uptime  string `json:"uptime"`
			Service string `json:"service"`
		}
		entries := make([]entry, 0, len(result.Items))
		now := time.Now()
		for _, c := range result.Items {
			name := c.ID
			if len(name) > 12 {
				name = name[:12]
			}
			if len(c.Names) > 0 {
				name = c.Names[0]
				if len(name) > 0 && name[0] == '/' {
					name = name[1:]
				}
			}
			uptime := ""
			if c.Created > 0 {
				d := now.Sub(time.Unix(c.Created, 0)).Truncate(time.Second)
				uptime = d.String()
			}
			entries = append(entries, entry{
				Name:    name,
				Status:  c.Status,
				State:   string(c.State),
				Image:   c.Image,
				Uptime:  uptime,
				Service: c.Labels["com.docker.compose.service"],
			})
		}

		b, err := json.Marshal(entries)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func GetLogs(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("get_logs",
		mcp.WithDescription("Get recent log lines from a named service container"),
		mcp.WithString("service", mcp.Required(), mcp.Description("Compose service name")),
		mcp.WithNumber("lines", mcp.Description("Number of log lines to return (default 50)")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		service := mcp.ParseString(req, "service", "")
		if service == "" {
			return mcp.NewToolResultError("missing required argument: service"), nil //nolint:nilerr
		}
		lines := mcp.ParseInt(req, "lines", 50)
		if lines <= 0 {
			lines = 50
		}

		id, errResult := findContainer(ctx, dc, project, service)
		if errResult != nil {
			return errResult, nil
		}

		rc, err := dc.ContainerLogs(ctx, id, moby.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Tail:       strconv.Itoa(lines),
		})
		if err != nil {
			return mcp.NewToolResultError("docker error: " + err.Error()), nil //nolint:nilerr
		}
		defer rc.Close()

		raw, err := io.ReadAll(rc)
		if err != nil {
			return mcp.NewToolResultError("read logs error: " + err.Error()), nil //nolint:nilerr
		}

		// Try to demux Docker stream headers (non-TTY containers).
		// Fall back to raw bytes for TTY containers (single stream, no headers).
		var buf bytes.Buffer
		if _, err := stdcopy.StdCopy(&buf, &buf, bytes.NewReader(raw)); err != nil {
			buf.Write(raw)
		}
		return mcp.NewToolResultText(buf.String()), nil
	}
	return tool, handler
}

func RestartContainer(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("restart_container",
		mcp.WithDescription("Restart a named service container and return its new status"),
		mcp.WithString("service", mcp.Required(), mcp.Description("Compose service name")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		service := mcp.ParseString(req, "service", "")
		if service == "" {
			return mcp.NewToolResultError("missing required argument: service"), nil //nolint:nilerr
		}

		id, errResult := findContainer(ctx, dc, project, service)
		if errResult != nil {
			return errResult, nil
		}

		if _, err := dc.ContainerRestart(ctx, id, moby.ContainerRestartOptions{}); err != nil {
			return mcp.NewToolResultError("restart failed: " + err.Error()), nil //nolint:nilerr
		}

		inspectResult, err := dc.ContainerInspect(ctx, id, moby.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("restarted but inspect failed: " + err.Error()), nil //nolint:nilerr
		}

		state := inspectResult.Container.State
		running := false
		status := "unknown"
		if state != nil {
			running = state.Running
			status = string(state.Status)
		}

		type result struct {
			Service string `json:"service"`
			Running bool   `json:"running"`
			Status  string `json:"status"`
		}
		b, err := json.Marshal(result{Service: service, Running: running, Status: status})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func PullImage(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("pull_image",
		mcp.WithDescription("Pull the latest image for a named service container and report if the digest changed"),
		mcp.WithString("service", mcp.Required(), mcp.Description("Compose service name")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		service := mcp.ParseString(req, "service", "")
		if service == "" {
			return mcp.NewToolResultError("missing required argument: service"), nil //nolint:nilerr
		}

		id, errResult := findContainer(ctx, dc, project, service)
		if errResult != nil {
			return errResult, nil
		}

		inspectBefore, err := dc.ContainerInspect(ctx, id, moby.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("inspect failed: " + err.Error()), nil //nolint:nilerr
		}
		imageRef := inspectBefore.Container.Config.Image

		digestBefore := ""
		if imgBefore, err := dc.ImageInspect(ctx, imageRef); err == nil && len(imgBefore.RepoDigests) > 0 {
			digestBefore = imgBefore.RepoDigests[0]
		}

		pullResp, err := dc.ImagePull(ctx, imageRef, moby.ImagePullOptions{})
		if err != nil {
			return mcp.NewToolResultError("pull failed: " + err.Error()), nil //nolint:nilerr
		}
		if err := pullResp.Wait(ctx); err != nil {
			return mcp.NewToolResultError("pull failed: " + err.Error()), nil //nolint:nilerr
		}

		digestAfter := ""
		if imgAfter, err := dc.ImageInspect(ctx, imageRef); err == nil && len(imgAfter.RepoDigests) > 0 {
			digestAfter = imgAfter.RepoDigests[0]
		}

		type result struct {
			Service       string `json:"service"`
			Image         string `json:"image"`
			DigestBefore  string `json:"digest_before,omitempty"`
			DigestAfter   string `json:"digest_after,omitempty"`
			DigestChanged bool   `json:"digest_changed"`
		}
		b, err := json.Marshal(result{
			Service:       service,
			Image:         imageRef,
			DigestBefore:  digestBefore,
			DigestAfter:   digestAfter,
			DigestChanged: digestBefore != digestAfter,
		})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}
