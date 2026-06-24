package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/moby/moby/api/pkg/stdcopy"
	containerapi "github.com/moby/moby/api/types/container"
	moby "github.com/moby/moby/client"
)

type DockerClient interface {
	ContainerList(ctx context.Context, options moby.ContainerListOptions) (moby.ContainerListResult, error)
	ContainerStats(ctx context.Context, containerID string, options moby.ContainerStatsOptions) (moby.ContainerStatsResult, error)
	ContainerLogs(ctx context.Context, containerID string, options moby.ContainerLogsOptions) (moby.ContainerLogsResult, error)
	ContainerInspect(ctx context.Context, containerID string, options moby.ContainerInspectOptions) (moby.ContainerInspectResult, error)
	ContainerRestart(ctx context.Context, containerID string, options moby.ContainerRestartOptions) (moby.ContainerRestartResult, error)
	ContainerTop(ctx context.Context, containerID string, options moby.ContainerTopOptions) (moby.ContainerTopResult, error)
	ExecCreate(ctx context.Context, containerID string, options moby.ExecCreateOptions) (moby.ExecCreateResult, error)
	ExecAttach(ctx context.Context, execID string, options moby.ExecAttachOptions) (moby.ExecAttachResult, error)
	ExecInspect(ctx context.Context, execID string, options moby.ExecInspectOptions) (moby.ExecInspectResult, error)
	ImagePull(ctx context.Context, refStr string, options moby.ImagePullOptions) (moby.ImagePullResponse, error)
	ImageInspect(ctx context.Context, imageID string, inspectOpts ...moby.ImageInspectOption) (moby.ImageInspectResult, error)
}

func NewDockerClient(host string) (DockerClient, error) {
	opts := []moby.Opt{moby.FromEnv, moby.WithTimeout(30 * time.Second)}
	if host != "" {
		opts = append(opts, moby.WithHost(host))
	}
	return moby.New(opts...)
}

func findService(ctx context.Context, dc DockerClient, project, service string) (string, *mcp.CallToolResult) {
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
		return "", mcp.NewToolResultError(fmt.Sprintf("service %q not found in project %q", service, project))
	}
	return result.Items[0].ID, nil
}

func parseStringSlice(req mcp.CallToolRequest, key string) []string {
	v := mcp.ParseArgument(req, key, nil)
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func calcCPUPercent(stats containerapi.StatsResponse) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	if sysDelta <= 0 || cpuDelta <= 0 {
		return 0
	}
	numCPUs := float64(stats.CPUStats.OnlineCPUs)
	if numCPUs == 0 {
		numCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	return math.Round((cpuDelta/sysDelta)*numCPUs*100*100) / 100
}

func DockerPS(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("docker_ps",
		mcp.WithDescription("List compose stack containers with status, health, and resource usage"),
		mcp.WithString("name", mcp.Description("Filter to a single service by name")),
		mcp.WithBoolean("all", mcp.Description("Include stopped containers (default false)")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nameFilter := mcp.ParseString(req, "name", "")
		all := mcp.ParseBoolean(req, "all", false)

		filters := make(moby.Filters).Add("label", "com.docker.compose.project="+project)
		if nameFilter != "" {
			filters = filters.Add("label", "com.docker.compose.service="+nameFilter)
		}
		listResult, err := dc.ContainerList(ctx, moby.ContainerListOptions{All: all, Filters: filters})
		if err != nil {
			return mcp.NewToolResultError("docker error: " + err.Error()), nil //nolint:nilerr
		}

		type entry struct {
			Name          string  `json:"name"`
			Image         string  `json:"image"`
			Status        string  `json:"status"`
			State         string  `json:"state"`
			Uptime        string  `json:"uptime"`
			MemoryUsageMB float64 `json:"memory_usage_mb"`
			MemoryLimitMB float64 `json:"memory_limit_mb"`
			MemoryPercent float64 `json:"memory_percent"`
			CPUPercent    float64 `json:"cpu_percent"`
		}

		entries := make([]entry, len(listResult.Items))
		now := time.Now()
		var wg sync.WaitGroup

		for i, c := range listResult.Items {
			wg.Add(1)
			go func(i int, c containerapi.Summary) {
				defer wg.Done()
				name := c.ID
				if len(name) > 12 {
					name = name[:12]
				}
				if len(c.Names) > 0 {
					n := c.Names[0]
					if len(n) > 0 && n[0] == '/' {
						n = n[1:]
					}
					name = n
				}
				uptime := ""
				if c.Created > 0 {
					uptime = now.Sub(time.Unix(c.Created, 0)).Truncate(time.Second).String()
				}
				e := entry{
					Name:   name,
					Image:  c.Image,
					Status: c.Status,
					State:  string(c.State),
					Uptime: uptime,
				}
				statsRes, err := dc.ContainerStats(ctx, c.ID, moby.ContainerStatsOptions{
					Stream:                false,
					IncludePreviousSample: true,
				})
				if err == nil {
					defer statsRes.Body.Close()
					var s containerapi.StatsResponse
					if json.NewDecoder(statsRes.Body).Decode(&s) == nil {
						memUsage := s.MemoryStats.Usage
						if cache, ok := s.MemoryStats.Stats["inactive_file"]; ok && cache < memUsage {
							memUsage -= cache
						}
						memLimit := s.MemoryStats.Limit
						e.MemoryUsageMB = math.Round(float64(memUsage)/1024/1024*100) / 100
						e.MemoryLimitMB = math.Round(float64(memLimit)/1024/1024*100) / 100
						if memLimit > 0 {
							e.MemoryPercent = math.Round(float64(memUsage)/float64(memLimit)*100*100) / 100
						}
						e.CPUPercent = calcCPUPercent(s)
					}
				}
				entries[i] = e
			}(i, c)
		}
		wg.Wait()

		b, err := json.Marshal(entries)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func DockerLogs(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("docker_logs",
		mcp.WithDescription("Get container logs with optional filtering by time, line count, and grep pattern"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Compose service name")),
		mcp.WithString("since", mcp.Description("Return logs since duration (10m, 2h) or ISO timestamp")),
		mcp.WithNumber("tail", mcp.Description("Number of lines from the end (default 100)")),
		mcp.WithString("grep", mcp.Description("Filter lines containing this substring (case-insensitive)")),
		mcp.WithBoolean("grep_invert", mcp.Description("Exclude matching lines instead of including them")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := mcp.ParseString(req, "name", "")
		if name == "" {
			return mcp.NewToolResultError("missing required argument: name"), nil //nolint:nilerr
		}
		since := mcp.ParseString(req, "since", "")
		tail := mcp.ParseInt(req, "tail", 100)
		if tail <= 0 {
			tail = 100
		}
		grepStr := mcp.ParseString(req, "grep", "")
		grepInvert := mcp.ParseBoolean(req, "grep_invert", false)

		id, errResult := findService(ctx, dc, project, name)
		if errResult != nil {
			return errResult, nil
		}

		rc, err := dc.ContainerLogs(ctx, id, moby.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Since:      since,
			Tail:       fmt.Sprintf("%d", tail),
		})
		if err != nil {
			return mcp.NewToolResultError("docker error: " + err.Error()), nil //nolint:nilerr
		}
		defer rc.Close()

		raw, err := io.ReadAll(rc)
		if err != nil {
			return mcp.NewToolResultError("read error: " + err.Error()), nil //nolint:nilerr
		}

		// Try to demux Docker stream headers (non-TTY containers).
		// Fall back to raw bytes for TTY containers (single stream, no headers).
		var buf bytes.Buffer
		if _, err := stdcopy.StdCopy(&buf, &buf, bytes.NewReader(raw)); err != nil {
			buf.Reset()
			buf.Write(raw)
		}

		scanner := bufio.NewScanner(&buf)
		var lines []string
		for scanner.Scan() {
			line := scanner.Text()
			if grepStr != "" {
				matches := strings.Contains(strings.ToLower(line), strings.ToLower(grepStr))
				if grepInvert {
					matches = !matches
				}
				if !matches {
					continue
				}
			}
			lines = append(lines, line)
		}

		const maxLines = 500
		totalLines := len(lines)
		truncated := totalLines > maxLines
		if truncated {
			lines = lines[:maxLines]
		}
		if lines == nil {
			lines = []string{}
		}

		type result struct {
			Container  string   `json:"container"`
			Lines      []string `json:"lines"`
			TotalLines int      `json:"total_lines"`
			Truncated  bool     `json:"truncated"`
		}
		b, err := json.Marshal(result{
			Container:  name,
			Lines:      lines,
			TotalLines: totalLines,
			Truncated:  truncated,
		})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func DockerInspect(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("docker_inspect",
		mcp.WithDescription("Get container networking, mounts, and config (env vars excluded)"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Compose service name")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := mcp.ParseString(req, "name", "")
		if name == "" {
			return mcp.NewToolResultError("missing required argument: name"), nil //nolint:nilerr
		}

		id, errResult := findService(ctx, dc, project, name)
		if errResult != nil {
			return errResult, nil
		}

		res, err := dc.ContainerInspect(ctx, id, moby.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("docker error: " + err.Error()), nil //nolint:nilerr
		}
		c := res.Container

		type mountInfo struct {
			Source      string `json:"source"`
			Destination string `json:"destination"`
			Mode        string `json:"mode"`
		}
		mounts := make([]mountInfo, 0, len(c.Mounts))
		for _, m := range c.Mounts {
			mounts = append(mounts, mountInfo{Source: m.Source, Destination: m.Destination, Mode: m.Mode})
		}

		ipAddresses := map[string]string{}
		if c.NetworkSettings != nil {
			for netName, ep := range c.NetworkSettings.Networks {
				if ep != nil {
					ipAddresses[netName] = ep.IPAddress.String()
				}
			}
		}

		var memLimitMB int64
		var networkMode, restartPolicy string
		if c.HostConfig != nil {
			if c.HostConfig.Memory > 0 {
				memLimitMB = c.HostConfig.Memory / 1024 / 1024
			}
			networkMode = string(c.HostConfig.NetworkMode)
			restartPolicy = string(c.HostConfig.RestartPolicy.Name)
		}

		type result struct {
			Name          string            `json:"name"`
			Image         string            `json:"image"`
			IPAddresses   map[string]string `json:"ip_addresses"`
			Mounts        []mountInfo       `json:"mounts"`
			NetworkMode   string            `json:"network_mode"`
			RestartPolicy string            `json:"restart_policy"`
			MemoryLimitMB int64             `json:"memory_limit_mb"`
		}
		b, err := json.Marshal(result{
			Name:          strings.TrimPrefix(c.Name, "/"),
			Image:         c.Config.Image,
			IPAddresses:   ipAddresses,
			Mounts:        mounts,
			NetworkMode:   networkMode,
			RestartPolicy: restartPolicy,
			MemoryLimitMB: memLimitMB,
		})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func DockerRestart(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("docker_restart",
		mcp.WithDescription("Restart one or more service containers"),
		mcp.WithArray("names",
			mcp.Required(),
			mcp.Description("Compose service names to restart"),
			mcp.WithStringItems(),
		),
		mcp.WithNumber("timeout", mcp.Description("Seconds to wait for graceful stop (default 10)")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		names := parseStringSlice(req, "names")
		if len(names) == 0 {
			return mcp.NewToolResultError("missing required argument: names"), nil //nolint:nilerr
		}
		timeoutSec := mcp.ParseInt(req, "timeout", 10)
		if timeoutSec <= 0 {
			timeoutSec = 10
		}

		type failure struct {
			Name  string `json:"name"`
			Error string `json:"error"`
		}
		restarted := make([]string, 0, len(names))
		var failed []failure
		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, name := range names {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				id, errResult := findService(ctx, dc, project, name)
				if errResult != nil {
					tc, _ := errResult.Content[0].(mcp.TextContent)
					mu.Lock()
					failed = append(failed, failure{Name: name, Error: tc.Text})
					mu.Unlock()
					return
				}
				if _, err := dc.ContainerRestart(ctx, id, moby.ContainerRestartOptions{Timeout: &timeoutSec}); err != nil {
					mu.Lock()
					failed = append(failed, failure{Name: name, Error: err.Error()})
					mu.Unlock()
					return
				}
				mu.Lock()
				restarted = append(restarted, name)
				mu.Unlock()
			}(name)
		}
		wg.Wait()

		if failed == nil {
			failed = []failure{}
		}

		type result struct {
			Restarted []string  `json:"restarted"`
			Failed    []failure `json:"failed"`
		}
		b, err := json.Marshal(result{Restarted: restarted, Failed: failed})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func DockerPull(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("docker_pull",
		mcp.WithDescription("Pull the latest image for a service container and report if the digest changed"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Compose service name")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := mcp.ParseString(req, "name", "")
		if name == "" {
			return mcp.NewToolResultError("missing required argument: name"), nil //nolint:nilerr
		}

		id, errResult := findService(ctx, dc, project, name)
		if errResult != nil {
			return errResult, nil
		}

		inspectBefore, err := dc.ContainerInspect(ctx, id, moby.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("inspect failed: " + err.Error()), nil //nolint:nilerr
		}
		imageRef := inspectBefore.Container.Config.Image

		digestBefore := ""
		if img, err := dc.ImageInspect(ctx, imageRef); err == nil && len(img.RepoDigests) > 0 {
			digestBefore = img.RepoDigests[0]
		}

		pullResp, err := dc.ImagePull(ctx, imageRef, moby.ImagePullOptions{})
		if err != nil {
			return mcp.NewToolResultError("pull failed: " + err.Error()), nil //nolint:nilerr
		}
		defer pullResp.Close()
		if err := pullResp.Wait(ctx); err != nil {
			return mcp.NewToolResultError("pull failed: " + err.Error()), nil //nolint:nilerr
		}

		digestAfter := ""
		if img, err := dc.ImageInspect(ctx, imageRef); err == nil && len(img.RepoDigests) > 0 {
			digestAfter = img.RepoDigests[0]
		}

		type result struct {
			Service       string `json:"service"`
			Image         string `json:"image"`
			DigestBefore  string `json:"digest_before,omitempty"`
			DigestAfter   string `json:"digest_after,omitempty"`
			DigestChanged bool   `json:"digest_changed"`
		}
		b, err := json.Marshal(result{
			Service:       name,
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

// TTY and stdin are always disabled; output is capped at 64 KB.
func DockerExec(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("docker_exec",
		mcp.WithDescription("Run a command inside a service container and return stdout/stderr (no TTY, no stdin)"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Compose service name")),
		mcp.WithArray("command",
			mcp.Required(),
			mcp.Description(`Command and arguments, e.g. ["cat", "/config/config.xml"]`),
			mcp.WithStringItems(),
		),
		mcp.WithNumber("timeout", mcp.Description("Seconds before killing the command (default 30)")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := mcp.ParseString(req, "name", "")
		if name == "" {
			return mcp.NewToolResultError("missing required argument: name"), nil //nolint:nilerr
		}
		command := parseStringSlice(req, "command")
		if len(command) == 0 {
			return mcp.NewToolResultError("missing required argument: command"), nil //nolint:nilerr
		}
		timeoutSec := mcp.ParseInt(req, "timeout", 30)
		if timeoutSec <= 0 {
			timeoutSec = 30
		}

		id, errResult := findService(ctx, dc, project, name)
		if errResult != nil {
			return errResult, nil
		}

		execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()

		createRes, err := dc.ExecCreate(execCtx, id, moby.ExecCreateOptions{
			Cmd:          command,
			AttachStdout: true,
			AttachStderr: true,
			AttachStdin:  false,
			TTY:          false,
		})
		if err != nil {
			return mcp.NewToolResultError("exec create failed: " + err.Error()), nil //nolint:nilerr
		}

		attachRes, err := dc.ExecAttach(execCtx, createRes.ID, moby.ExecAttachOptions{TTY: false})
		if err != nil {
			return mcp.NewToolResultError("exec attach failed: " + err.Error()), nil //nolint:nilerr
		}
		defer attachRes.Close()

		const maxOutput = 64 * 1024
		var stdout, stderr bytes.Buffer
		stdcopy.StdCopy(&stdout, &stderr, io.LimitReader(attachRes.Reader, maxOutput)) //nolint:errcheck // best-effort; context timeout handles hung exec

		inspectRes, err := dc.ExecInspect(execCtx, createRes.ID, moby.ExecInspectOptions{})
		exitCode := -1
		if err == nil {
			exitCode = inspectRes.ExitCode
		}

		type result struct {
			Container string `json:"container"`
			ExitCode  int    `json:"exit_code"`
			Stdout    string `json:"stdout"`
			Stderr    string `json:"stderr"`
		}
		b, err := json.Marshal(result{
			Container: name,
			ExitCode:  exitCode,
			Stdout:    stdout.String(),
			Stderr:    stderr.String(),
		})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func DockerTop(dc DockerClient, project string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("docker_top",
		mcp.WithDescription("List processes inside a service container, with zombie detection"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Compose service name")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := mcp.ParseString(req, "name", "")
		if name == "" {
			return mcp.NewToolResultError("missing required argument: name"), nil //nolint:nilerr
		}

		id, errResult := findService(ctx, dc, project, name)
		if errResult != nil {
			return errResult, nil
		}

		topRes, err := dc.ContainerTop(ctx, id, moby.ContainerTopOptions{})
		if err != nil {
			return mcp.NewToolResultError("docker error: " + err.Error()), nil //nolint:nilerr
		}

		colIdx := map[string]int{}
		for i, title := range topRes.Titles {
			colIdx[strings.TrimSpace(title)] = i
		}
		colVal := func(row []string, key string) string {
			idx, ok := colIdx[key]
			if !ok || idx >= len(row) {
				return ""
			}
			return row[idx]
		}

		type process struct {
			PID     string `json:"pid"`
			User    string `json:"user"`
			Command string `json:"command"`
			Zombie  bool   `json:"zombie"`
		}
		processes := make([]process, 0, len(topRes.Processes))
		for _, row := range topRes.Processes {
			cmd := colVal(row, "CMD")
			if cmd == "" {
				cmd = colVal(row, "COMMAND")
			}
			p := process{
				PID:     colVal(row, "PID"),
				User:    colVal(row, "USER"),
				Command: cmd,
				Zombie:  strings.Contains(cmd, "<defunct>"),
			}
			processes = append(processes, p)
		}

		type result struct {
			Container string    `json:"container"`
			Processes []process `json:"processes"`
		}
		b, err := json.Marshal(result{Container: name, Processes: processes})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}
