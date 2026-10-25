package theme

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// partnerFixture is a theme file another product wrote, pinned by its SHA-256: a
// fixture edited here would test nothing of theirs.
type partnerFixture struct {
	sum, source string
}

// partnerFixtures are the theme files Stator and Exciter export, vendored as they came (#295).
var partnerFixtures = map[string]partnerFixture{
	"exciter.armature-theme.json": {"3278a30d8e7811ee69947126a3f6fa83a2a9be3b6a411af2a5c5d5070203d339", "Exciter f94121d, backend/internal/theme/testdata"},
}

func TestThePartnersFixturesAreTheirFilesByteForByte(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.armature-theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := filepath.Base(file)
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		want, ok := partnerFixtures[name]
		if !ok || hex.EncodeToString(sum[:]) != want.sum {
			t.Errorf("%s is not the partner's file as it came (%s); vendor it again rather than edit it", name, want.source)
			continue
		}
		// What arrives has to be a package this version reads and a spec its editor would save.
		var pkg Package
		if err := json.Unmarshal(body, &pkg); err != nil || pkg.Format != PackageFormat {
			t.Errorf("%s is not a %s package: %v", name, PackageFormat, err)
			continue
		}
		assets := map[uuid.UUID]bool{}
		for _, a := range pkg.Assets {
			assets[a.ID] = true
		}
		if err := Validate(&pkg.Spec, uuid.Nil, assets); err != nil {
			t.Errorf("%s does not validate here: %v", name, err)
		}
	}
	if len(files) != len(partnerFixtures) {
		t.Errorf("testdata holds %d partner theme files, want %d", len(files), len(partnerFixtures))
	}
}
