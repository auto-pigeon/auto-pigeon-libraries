package mapio

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/internal/apmaptest"
)

// Every Quake 1 / Quake 2 APMap direction refuses a Quake III document, whole, by its game, with and
// without validation — never a map written without the patch, never a matrix flattened into a
// classic projection. The documents are AULIBS' own valid 1.5 vectors, so validation would pass and
// the refusal is about the game alone. (A quake1 document holding a Quake III surface is SEM-Q-4,
// and ToMapDocument's surface refusal still stands behind this gate for every other caller.)
func TestEveryAPMapDirectionRefusesQuake3Surfaces(t *testing.T) {
	root := apmaptest.PackageRoot()
	if root == "" {
		t.Skip("the auto-pigeon-libraries contract package is not beside this checkout")
	}
	for _, file := range []string{"q3-patch-minimal.apmap", "q3-brush-primitives.apmap"} {
		data, err := os.ReadFile(filepath.Join(root, "test-vectors", "valid", file))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"apmap-to-q1map", "apmap-to-q2map", "apmap-to-q2rmf"} {
			for _, validate := range []bool{true, false} {
				direction, ok := LookupDirection(name)
				if !ok {
					t.Fatalf("no %s direction", name)
				}
				output, _, err := Convert(context.Background(), direction, file, data, Options{Validate: validate})
				if len(output) != 0 {
					t.Fatalf("%s %s validate=%v: %d bytes written alongside the refusal", name, file, validate, len(output))
				}
				var refusal *GameError
				if !errors.As(err, &refusal) {
					t.Fatalf("%s %s validate=%v: err = %v, want the game refusal", name, file, validate, err)
				}
				if !strings.Contains(err.Error(), "unsupported game") || !strings.Contains(err.Error(), "apmap-to-q3map") {
					t.Fatalf("%s %s: the refusal does not name the game and the right direction: %v", name, file, err)
				}
			}
		}
	}
}
