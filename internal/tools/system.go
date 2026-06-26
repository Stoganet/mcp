package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

type SystemReader interface {
	Statfs(path string) (syscall.Statfs_t, error)
	ReadMounts() ([]byte, error)
	Interfaces() ([]net.Interface, error)
	InterfaceAddrs(iface net.Interface) ([]net.Addr, error)
}

type realSystemReader struct{}

func NewSystemReader() SystemReader { return realSystemReader{} }

func (realSystemReader) Statfs(path string) (syscall.Statfs_t, error) {
	var s syscall.Statfs_t
	return s, syscall.Statfs(path, &s)
}

func (realSystemReader) ReadMounts() ([]byte, error) {
	return os.ReadFile("/proc/mounts")
}

func (realSystemReader) Interfaces() ([]net.Interface, error) {
	return net.Interfaces()
}

func (realSystemReader) InterfaceAddrs(iface net.Interface) ([]net.Addr, error) {
	return iface.Addrs()
}

type diskInfo struct {
	Path    string  `json:"path"`
	TotalGB float64 `json:"total_gb"`
	UsedGB  float64 `json:"used_gb"`
	FreeGB  float64 `json:"free_gb"`
	Percent float64 `json:"percent_used"`
}

var diskPaths = []string{"/", "/mnt/wd", "/var/lib/docker"}

func SystemDiskUsage(sr SystemReader) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("get_disk_usage",
		mcp.WithDescription("Get disk usage for /, /mnt/wd, and /var/lib/docker"),
	)
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		results := make([]diskInfo, 0, len(diskPaths))
		for _, p := range diskPaths {
			fs, err := sr.Statfs(p)
			if err != nil {
				results = append(results, diskInfo{Path: p, TotalGB: -1, UsedGB: -1, FreeGB: -1, Percent: -1})
				continue
			}
			total := float64(fs.Blocks) * float64(fs.Bsize)
			free := float64(fs.Bavail) * float64(fs.Bsize)
			used := total - float64(fs.Bfree)*float64(fs.Bsize)
			var pct float64
			if total > 0 {
				pct = used / total * 100
			}
			const gb = 1 << 30
			results = append(results, diskInfo{
				Path:    p,
				TotalGB: round2(total / gb),
				UsedGB:  round2(used / gb),
				FreeGB:  round2(free / gb),
				Percent: round2(pct),
			})
		}
		b, err := json.Marshal(results)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

type mountInfo struct {
	Mounted bool   `json:"mounted"`
	Device  string `json:"device,omitempty"`
	FSType  string `json:"fs_type,omitempty"`
}

func SystemMountStatus(sr SystemReader) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("get_mount_status",
		mcp.WithDescription("Check whether /mnt/wd is mounted and report device and filesystem type"),
	)
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		data, err := sr.ReadMounts()
		if err != nil {
			return mcp.NewToolResultError("read /proc/mounts: " + err.Error()), nil //nolint:nilerr
		}
		info := parseMountEntry(string(data), "/mnt/wd")
		b, err := json.Marshal(info)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func parseMountEntry(content, target string) mountInfo {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[1] == target {
			return mountInfo{Mounted: true, Device: fields[0], FSType: fields[2]}
		}
	}
	return mountInfo{Mounted: false}
}

type netbirdStatus struct {
	Up        bool     `json:"up"`
	Addresses []string `json:"addresses,omitempty"`
}

const netbirdIface = "wt0"

func SystemNetbirdStatus(sr SystemReader) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("get_netbird_status",
		mcp.WithDescription("Check NetBird tunnel interface (wt0): up/down and overlay IP addresses"),
	)
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ifaces, err := sr.Interfaces()
		if err != nil {
			return mcp.NewToolResultError("interfaces: " + err.Error()), nil //nolint:nilerr
		}
		status := netbirdStatus{}
		for _, iface := range ifaces {
			if iface.Name != netbirdIface {
				continue
			}
			status.Up = iface.Flags&net.FlagUp != 0
			addrs, err := sr.InterfaceAddrs(iface)
			if err == nil {
				for _, a := range addrs {
					status.Addresses = append(status.Addresses, a.String())
				}
			}
			break
		}
		b, err := json.Marshal(status)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

type vpnStatus struct {
	Up     bool   `json:"up"`
	ExitIP string `json:"exit_ip,omitempty"`
	Error  string `json:"error,omitempty"`
}

func SystemVPNStatus(gluetunURL string) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	client := &http.Client{Timeout: 5 * time.Second}
	tool := mcp.NewTool("get_vpn_status",
		mcp.WithDescription("Check Gluetun VPN tunnel state and current exit IP via Gluetun control API"),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status := vpnStatus{}

		tunnelURL, err := joinURL(gluetunURL, "/v1/openvpn/status")
		if err != nil {
			return mcp.NewToolResultError("invalid gluetun URL: " + err.Error()), nil //nolint:nilerr
		}
		tunnelState, tunnelErr := gluetunGet(ctx, client, tunnelURL)
		if tunnelErr != nil {
			status.Error = tunnelErr.Error()
		} else {
			status.Up = strings.EqualFold(tunnelState["status"], "running")
		}

		if status.Up {
			ipURL, err := joinURL(gluetunURL, "/v1/publicip/ip")
			if err == nil {
				if ipData, err := gluetunGet(ctx, client, ipURL); err == nil {
					status.ExitIP = ipData["public_ip"]
				}
			}
		}

		b, err := json.Marshal(status)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func gluetunGet(ctx context.Context, client *http.Client, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gluetun %s: %d", url, resp.StatusCode)
	}
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode gluetun response: %w", err)
	}
	return result, nil
}

func joinURL(base, path string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("empty base URL")
	}
	return url.JoinPath(base, path)
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
