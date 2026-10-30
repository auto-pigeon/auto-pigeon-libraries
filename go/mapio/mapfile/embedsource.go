package mapfile

import _ "embed"

// The `.map` parser, model and writer, embedded so a consumer that derives data
// from them can hash them into its own compatibility key. Editing any of them
// then invalidates what was derived, without anyone having to remember to bump a
// version by hand.
//
// The material is split four ways, so all four files are exposed.

//go:embed parser.go
var ParserSource []byte

//go:embed model.go
var ModelSource []byte

//go:embed writer.go
var WriterSource []byte

//go:embed geometry.go
var GeometrySource []byte
