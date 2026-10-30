package mapio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/internal/apmaptest"
)

// A legacy document stays convertible. The frozen 1.3 vector group-two-groups.apmap is published
// VALID and carries a SEM-G-5 containment fault; declared as 1.3 it converts, with the finding as a
// warning. Relabelled as the current version, the same bytes are refused.
func TestALegacyContainmentFaultConvertsWithAWarning(t *testing.T) {
	root := apmaptest.PackageRoot()
	if root == "" {
		t.Skip("the auto-pigeon-libraries contract package is not beside this checkout")
	}
	data, err := os.ReadFile(filepath.Join(root, "deprecated", "1.3", "test-vectors", "valid", "group-two-groups.apmap"))
	if err != nil {
		t.Fatal(err)
	}
	direction, ok := LookupDirection("apmap-to-q1map")
	if !ok {
		t.Fatal("no apmap-to-q1map direction")
	}

	output, result, err := Convert(context.Background(), direction, "group-two-groups.apmap", data, Options{Validate: true})
	if err != nil {
		t.Fatalf("the legacy document was refused: %v", err)
	}
	if len(output) == 0 || !result.Validated {
		t.Fatalf("no validated output (validated=%v, %d bytes)", result.Validated, len(output))
	}
	if len(result.Warnings) != 1 || !strings.HasPrefix(result.Warnings[0].Text, "semantic_warning: SEM-G-5 ") {
		t.Fatalf("warnings %v, want the one SEM-G-5 warning", result.Warnings)
	}

	current := apmaptest.PromoteToCurrent(string(data))
	_, _, err = Convert(context.Background(), direction, "relabelled.apmap", []byte(current), Options{Validate: true})
	if err == nil || !strings.Contains(err.Error(), "SEM-G-5") {
		t.Fatalf("the relabelled current document was not refused for SEM-G-5: %v", err)
	}
}
