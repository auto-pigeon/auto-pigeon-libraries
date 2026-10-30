package convex

import _ "embed"

// Source is this package's own implementation, embedded so a consumer that derives
// data from it can hash it into its own compatibility key. Editing the
// implementation then invalidates what was derived, without anyone having to
// remember to bump a version by hand.
//
//go:embed convex.go
var Source []byte
