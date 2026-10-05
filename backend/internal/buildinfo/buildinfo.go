// Package buildinfo says which build of Armature is running, for the foot of
// the screen and for the log line that opens each process.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Set by the linker at image build time; see deploy/Dockerfile.backend. A
// build made without them falls back to what the Go toolchain recorded.
var (
	Version string
	Commit  string
	BuiltAt string
)

// Info is one build, as the API reports it.
type Info struct {
	// Version is the release as semver: "1.2.3" on a tag, "1.2.3+4.gabc1234"
	// four commits past one, or "dev" when nothing says.
	Version string `json:"version"`
	// Commit is the full git hash the binary was built from, when known.
	Commit string `json:"commit,omitempty"`
	// BuiltAt is when the image was built, RFC 3339, when known.
	BuiltAt string `json:"builtAt,omitempty"`
}

// Current is the running build.
func Current() Info {
	info := Info{Version: Version, Commit: Commit, BuiltAt: BuiltAt}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.BuiltAt == "" {
					info.BuiltAt = s.Value
				}
			}
		}
	}
	// Tags are written v1.2.3 and the Go toolchain reports them that way; the
	// version itself has no prefix, as in the chart and the image tags.
	info.Version = strings.TrimPrefix(info.Version, "v")
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}
