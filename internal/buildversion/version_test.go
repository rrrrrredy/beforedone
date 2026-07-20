package buildversion

import (
	"runtime/debug"
	"testing"
)

func TestResolvePrefersLinkedReleaseVersion(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v9.8.7"}}
	if got := Resolve("1.2.3", info, true); got != "1.2.3" {
		t.Fatalf("Resolve() = %q, want %q", got, "1.2.3")
	}
}

func TestResolveFallsBackToGoModuleVersion(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.0.0"}}
	if got := Resolve("dev", info, true); got != "1.0.0" {
		t.Fatalf("Resolve() = %q, want %q", got, "1.0.0")
	}
}

func TestResolveKeepsDevForLocalBuild(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
	}{
		{name: "missing build info"},
		{name: "development module", info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, ok: true},
		{name: "empty module", info: &debug.BuildInfo{}, ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve("dev", tt.info, tt.ok); got != "dev" {
				t.Fatalf("Resolve() = %q, want dev", got)
			}
		})
	}
}
