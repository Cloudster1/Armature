package theme

import (
	"embed"
	"encoding/json"
	"path"
	"sort"
	"strings"
)

// shippedFiles are whole themes kept as the files an export makes, so they
// double as what somebody imports to try the format.
//
//go:embed shipped/*.armature-theme.json
var shippedFiles embed.FS

// shippedDescriptions say what each file is for, on the Themes page.
var shippedDescriptions = map[string]string{
	"minecraft":     "Blocks all the way: square corners, bevelled faces, a wall of dirt under a strip of grass.",
	"lucky-rainbow": "A night market of a theme: every colour at once, a backdrop that never stops, a charm on everything.",
}

// shipped reads the embedded theme files as examples, keyed by file name.
func shipped() []Example {
	entries, err := shippedFiles.ReadDir("shipped")
	if err != nil {
		return nil
	}
	var out []Example
	for _, entry := range entries {
		body, err := shippedFiles.ReadFile(path.Join("shipped", entry.Name()))
		if err != nil {
			continue
		}
		var pkg Package
		if err := json.Unmarshal(body, &pkg); err != nil || pkg.Format != PackageFormat {
			continue
		}
		key := strings.TrimSuffix(entry.Name(), ".armature-theme.json")
		out = append(out, Example{Key: key, Name: pkg.Name, Description: shippedDescriptions[key], Spec: pkg.Spec})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
