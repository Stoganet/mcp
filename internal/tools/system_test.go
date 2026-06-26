package tools_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"

	"github.com/Stoganet/mcp/internal/tools"
)

type mockSystemReader struct {
	statfsFn         func(path string) (syscall.Statfs_t, error)
	readMountsFn     func() ([]byte, error)
	interfacesFn     func() ([]net.Interface, error)
	interfaceAddrsFn func(iface net.Interface) ([]net.Addr, error)
}

func (m *mockSystemReader) Statfs(path string) (syscall.Statfs_t, error) {
	return m.statfsFn(path)
}

func (m *mockSystemReader) ReadMounts() ([]byte, error) {
	return m.readMountsFn()
}

func (m *mockSystemReader) Interfaces() ([]net.Interface, error) {
	return m.interfacesFn()
}

func (m *mockSystemReader) InterfaceAddrs(iface net.Interface) ([]net.Addr, error) {
	return m.interfaceAddrsFn(iface)
}

func TestSystemDiskUsage(t *testing.T) {
	stats := map[string]syscall.Statfs_t{
		"/":               {Blocks: 1000, Bfree: 400, Bavail: 380, Bsize: 1 << 20},
		"/mnt/wd":         {Blocks: 2000, Bfree: 500, Bavail: 475, Bsize: 1 << 20},
		"/var/lib/docker": {Blocks: 500, Bfree: 100, Bavail: 95, Bsize: 1 << 20},
	}
	mock := &mockSystemReader{
		statfsFn: func(path string) (syscall.Statfs_t, error) {
			s, ok := stats[path]
			if !ok {
				t.Errorf("unexpected Statfs call for path %q", path)
			}
			return s, nil
		},
	}
	_, handler := tools.SystemDiskUsage(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out []struct {
		Path    string  `json:"path"`
		FreeGB  float64 `json:"free_gb"`
		Percent float64 `json:"percent_used"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("want 3 entries, got %d", len(out))
	}
	want := []struct {
		path    string
		freeGB  float64
		percent float64
	}{
		{"/", 0.37, 60},
		{"/mnt/wd", 0.46, 75},
		{"/var/lib/docker", 0.09, 80},
	}
	for i, w := range want {
		if out[i].Path != w.path {
			t.Errorf("[%d] path = %q, want %q", i, out[i].Path, w.path)
		}
		if out[i].FreeGB != w.freeGB {
			t.Errorf("[%d] free_gb = %v, want %v", i, out[i].FreeGB, w.freeGB)
		}
		if out[i].Percent != w.percent {
			t.Errorf("[%d] percent = %v, want %v", i, out[i].Percent, w.percent)
		}
	}
}

func TestSystemMountStatus_Mounted(t *testing.T) {
	mock := &mockSystemReader{
		readMountsFn: func() ([]byte, error) {
			return []byte("/dev/sdb1 /mnt/wd ext4 rw 0 0\n"), nil
		},
	}
	_, handler := tools.SystemMountStatus(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Mounted bool   `json:"mounted"`
		Device  string `json:"device"`
		FSType  string `json:"fs_type"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Mounted {
		t.Error("want mounted=true")
	}
	if out.Device != "/dev/sdb1" {
		t.Errorf("device = %q, want /dev/sdb1", out.Device)
	}
	if out.FSType != "ext4" {
		t.Errorf("fs_type = %q, want ext4", out.FSType)
	}
}

func TestSystemMountStatus_NotMounted(t *testing.T) {
	mock := &mockSystemReader{
		readMountsFn: func() ([]byte, error) {
			return []byte("/dev/sda1 / ext4 rw 0 0\n"), nil
		},
	}
	_, handler := tools.SystemMountStatus(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Mounted bool `json:"mounted"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Mounted {
		t.Error("want mounted=false")
	}
}

func TestSystemNetbirdStatus_Up(t *testing.T) {
	mock := &mockSystemReader{
		interfacesFn: func() ([]net.Interface, error) {
			return []net.Interface{
				{Name: "eth0", Flags: net.FlagUp},
				{Name: "wt0", Flags: net.FlagUp | net.FlagMulticast, Index: 5},
			}, nil
		},
		interfaceAddrsFn: func(iface net.Interface) ([]net.Addr, error) {
			if iface.Name != "wt0" {
				t.Errorf("InterfaceAddrs called with %q, want wt0", iface.Name)
			}
			_, ipnet, _ := net.ParseCIDR("100.64.0.1/10")
			return []net.Addr{ipnet}, nil
		},
	}
	_, handler := tools.SystemNetbirdStatus(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Up        bool     `json:"up"`
		Addresses []string `json:"addresses"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Up {
		t.Error("want up=true")
	}
	if len(out.Addresses) == 0 {
		t.Error("want at least one address")
	}
}

func TestSystemNetbirdStatus_Down(t *testing.T) {
	mock := &mockSystemReader{
		interfacesFn: func() ([]net.Interface, error) {
			return []net.Interface{
				{Name: "eth0", Flags: net.FlagUp},
			}, nil
		},
		interfaceAddrsFn: func(_ net.Interface) ([]net.Addr, error) {
			return nil, nil
		},
	}
	_, handler := tools.SystemNetbirdStatus(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Up bool `json:"up"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Up {
		t.Error("want up=false when wt0 absent")
	}
}

func TestSystemVPNStatus_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/openvpn/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"running"}`))
		case "/v1/publicip/ip":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"public_ip":"203.0.113.42"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, handler := tools.SystemVPNStatus(srv.URL)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Up     bool   `json:"up"`
		ExitIP string `json:"exit_ip"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Up {
		t.Error("want up=true")
	}
	if out.ExitIP != "203.0.113.42" {
		t.Errorf("exit_ip = %q, want 203.0.113.42", out.ExitIP)
	}
}

func TestSystemVPNStatus_Down(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"stopped"}`))
	}))
	defer srv.Close()

	_, handler := tools.SystemVPNStatus(srv.URL)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Up     bool   `json:"up"`
		ExitIP string `json:"exit_ip"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Up {
		t.Error("want up=false")
	}
	if out.ExitIP != "" {
		t.Errorf("want no exit_ip when down, got %q", out.ExitIP)
	}
}

func TestSystemVPNStatus_Unreachable(t *testing.T) {
	_, handler := tools.SystemVPNStatus("http://127.0.0.1:1")
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Up    bool   `json:"up"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Up {
		t.Error("want up=false on connection failure")
	}
	if out.Error == "" {
		t.Error("want error field populated on connection failure")
	}
}
