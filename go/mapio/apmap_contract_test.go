package mapio

import (
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/internal/apmaptest"
)

// TestMain loads the real APMap contract before this package's tests run.
//
// Production loads it once in cli.Run and refuses the command if it cannot; a test binary has no
// such entrypoint, so every package that touches an APMap version says so here. Explicit on
// purpose: a lazy fallback inside the apmap package would be the "discover it when somebody first
// needs it" behaviour the current-only design forbids.
func TestMain(m *testing.M) { apmaptest.Load(m) }
