// Package buildversion resolves the version reported by installed CLI binaries.
package buildversion

import (
	"runtime/debug"
	"strings"
)

// Resolve prefers GoReleaser's linked version, then the module version embedded
// by `go install package@version`, and keeps local development builds as dev.
func Resolve(linked string, info *debug.BuildInfo, ok bool) string {
	if candidate := normalize(linked); candidate != "" {
		return candidate
	}
	if ok && info != nil {
		if candidate := normalize(info.Main.Version); candidate != "" {
			return candidate
		}
	}
	return "dev"
}

func normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "dev" || value == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(value, "v")
}
