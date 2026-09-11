package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

// 本文件只测沙箱 DTO 的序列化契约——壳层经 sidecar RPC 透传 Policy /
// BackendStatus 时必须保证 JSON 字段名与内核契约一致。本地裁决行为
// （AllowsCommand / AllowsWrite / CommandViolation / GuardedRunner）已随
// 死代码删除，裁决由 Rust 内核负责，不再在此测试。

func TestPolicyJSONSerialization(t *testing.T) {
	policy := Policy{
		Mode:          ModeWorkspaceWrite,
		WorkspaceRoot: "/home/user/project",
		WritableRoots: []string{"/tmp", "/var/cache"},
		AllowNetwork:  true,
	}
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(data)
	for _, field := range []string{
		`"mode"`,
		`"workspace_root"`,
		`"writable_roots"`,
		`"allow_network"`,
	} {
		if !strings.Contains(jsonStr, field) {
			t.Fatalf("policy JSON missing field %s: %s", field, jsonStr)
		}
	}

	var decoded Policy
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Mode != policy.Mode {
		t.Fatalf("mode round-trip: %q != %q", decoded.Mode, policy.Mode)
	}
	if !decoded.AllowNetwork {
		t.Fatalf("allow_network round-trip: got %v, want true", decoded.AllowNetwork)
	}
	if len(decoded.WritableRoots) != 2 {
		t.Fatalf("writable_roots round-trip len: %d", len(decoded.WritableRoots))
	}
}

func TestPolicyJSONOmitEmptyBehavior(t *testing.T) {
	// 空值字段应被 omitempty 省略，避免向前端/内核传无意义空串。
	policy := Policy{Mode: ModeReadOnly}
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(data)
	for _, omitted := range []string{
		`"workspace_root"`,
		`"writable_roots"`,
	} {
		if strings.Contains(jsonStr, omitted) {
			t.Fatalf("omitempty field %s should be absent: %s", omitted, jsonStr)
		}
	}
}

func TestBackendStatusJSONRoundTripAllPlatforms(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		status := BackendStatus{
			GOOS:                    goos,
			Backend:                 "bubblewrap",
			Enforced:                true,
			Degraded:                false,
			Reason:                  "round-trip",
			UnsupportedCapabilities: []string{"seccomp-filter"},
		}
		data, err := json.Marshal(status)
		if err != nil {
			t.Fatalf("marshal %s: %v", goos, err)
		}
		var decoded BackendStatus
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", goos, err)
		}
		if decoded.GOOS != goos {
			t.Fatalf("%s GOOS round-trip: %q", goos, decoded.GOOS)
		}
		if decoded.Backend == "" {
			t.Fatalf("%s backend empty", goos)
		}
	}
}

func TestBackendStatusJSONFieldNamesMatchAPIContract(t *testing.T) {
	status := BackendStatus{
		GOOS:                    "linux",
		Backend:                 "bubblewrap",
		Enforced:                false,
		Degraded:                true,
		Reason:                  "test",
		UnsupportedCapabilities: []string{"seccomp-filter"},
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(data)
	// 这些字段名是 sidecar RPC 与前端共同依赖的契约，必须稳定。
	for _, field := range []string{
		`"goos"`, `"backend"`, `"enforced"`, `"degraded"`,
		`"reason"`, `"unsupported_capabilities"`,
	} {
		if !strings.Contains(jsonStr, field) {
			t.Fatalf("BackendStatus JSON missing contract field %s: %s", field, jsonStr)
		}
	}
}

func TestBackendStatusZeroValueIsNotDegraded(t *testing.T) {
	// 零值 BackendStatus（未初始化）不应误报 degraded，避免前端误显示降级。
	var zero BackendStatus
	if zero.Degraded {
		t.Fatal("zero-value BackendStatus should not be Degraded")
	}
	if zero.Enforced {
		t.Fatal("zero-value BackendStatus should not be Enforced")
	}
}

func TestNormalizeModeCoversAllVariants(t *testing.T) {
	cases := map[string]Mode{
		"read-only":          ModeReadOnly,
		"readonly":           ModeReadOnly,
		"READ_ONLY":          ModeReadOnly,
		"workspace-write":    ModeWorkspaceWrite,
		"workspace_write":    ModeWorkspaceWrite,
		"WORKSPACE":          ModeWorkspaceWrite,
		"danger-full-access": ModeDangerFullAccess,
		"full-access":        ModeDangerFullAccess,
		"fullaccess":         ModeDangerFullAccess,
		"full-access-mode":   ModeDangerFullAccess,
		"":                   ModeWorkspaceWrite, // 默认
		"unknown":            ModeWorkspaceWrite, // 未知归默认
	}
	for input, want := range cases {
		if got := NormalizeMode(input); got != want {
			t.Fatalf("NormalizeMode(%q) = %q, want %q", input, got, want)
		}
	}
}
