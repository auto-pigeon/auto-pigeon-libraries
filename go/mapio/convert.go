// Package mapio converts and validates Quake map formats: the format-conversion matrix over APMap,
// `.map` and Worldcraft `.rmf`.
//
// # Bytes in, bytes out
//
// Convert takes and returns bytes. Reading the input and writing the output (and the manifest it may
// return) belong to the caller: this module opens no file, reads no environment variable, starts no
// goroutine or timer, and logs nothing. Every diagnostic is a returned value.
//
// # Decoding and validation are separate
//
// Decoding a document never validates it (apmap.DecodeJSON, mapfile.ParseNamed, rmf.Decode).
// Validation runs when a caller asks for it: Validate, or Options.Validate for a conversion that must
// hand a compiler checked input. Nothing here wakes up and declares a map broken.
//
// # What "validation" means here
//
// APMap validation: the versioned JSON schema plus the semantic rules. It is applied to whichever
// side of the conversion is APMap — the input when converting *from* APMap, the output when
// converting *to* it — because that is the side a schema can speak about.
//
// # The one conversion that is not lossless
//
// `.map` ↔ APMap round-trips byte-for-byte in both games. Anything through RMF does not, in one
// specific way: RMF stores a face's Quake 2 contents, flags and value as three int32s that are always
// present, so it cannot tell an explicit `0 0 0` tail from no tail at all. Worldcraft resolves this
// the same way when it exports — it omits the three fields when all three are zero — and this
// package reproduces its rule, so a `.map` face written `... 1 1 0 0 0` comes back as `... 1 1`. The
// face means the same thing to the compiler. Faces carrying real surface data are unaffected, and a
// second trip is a fixed point. rmf's encode.go lists what else an RMF write cannot carry.
//
// There is deliberately no separate "`.map` validation" step. A `.map` file is validated by being
// parsed: mapfile's parser is strict, reports `file:line:column`, and refuses everything it cannot
// represent. Adding a second pass that re-checked the same document would imply a stronger guarantee
// than exists.
package mapio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/quake3map"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/rmf"
)

// Format is one end of a conversion.
type Format string

const (
	FormatQ1Map Format = "q1map"
	FormatQ2Map Format = "q2map"
	FormatQ2RMF Format = "q2rmf"
	FormatAPMap Format = "apmap"
	// FormatQ3Map is Quake III `.map` text as Q3Map2 reads it: read by q3map-to-apmap (classic,
	// Valve 220, brushDef and patchDef2, through the quake3map reader) and written by apmap-to-q3map
	// from an APMap whose game is "quake3" (Q3_004).
	FormatQ3Map Format = "q3map"
)

// Extension is the file extension a format is written with.
func (f Format) Extension() string {
	switch f {
	case FormatQ1Map, FormatQ2Map, FormatQ3Map:
		return ".map"
	case FormatQ2RMF:
		return ".rmf"
	default:
		return ".apmap"
	}
}

// MaxQuake3MapInputBytes bounds one apmap-to-q3map input. The writer holds the decoded document and
// the text it writes, both linear in the input, so the bound is what keeps a conversion's memory
// bounded; it is well above the largest map in the private Quake III corpus (a few MiB).
const MaxQuake3MapInputBytes = 256 << 20

// MaxQuake3MapImportBytes bounds one q3map-to-apmap input. The reader's tree and the document it
// becomes are each several times the text, so this bound is lower than the writer's; it is still an
// order of magnitude above the largest Quake III source in the private corpus. Every brush is also
// bounded (apmap.MaxQuake3BrushFaces) and every patch grid is (3..31 per side, by the format).
const MaxQuake3MapImportBytes = 64 << 20

// Direction is one supported conversion.
//
// The set is closed and enumerated rather than computed from a From/To pair,
// so that the CLI flag names, the HTTP path segments and the usage text all
// come from one place and cannot drift apart.
type Direction struct {
	// Name is the identifier used everywhere: the CLI flag without its
	// leading dashes, and the last segment of the HTTP path.
	Name string
	From Format
	To   Format
	// Game is the value written into an APMap document's `game` field, and
	// the game a produced `.map` is meant for. Empty for a direction that
	// does not decide it.
	Game    string
	Summary string
}

// directions is the whole matrix.
//
// RMF converts only to and from APMap, never directly to or from `.map`. That
// is the boundary the human set when this was specified, and rmf's package note
// gives the reason: APMap is the format that carries identity and provenance,
// so a direct shortcut would be a second conversion contract to keep in step.
// No summary names a version. Which APMap version a direction READS is decided by the document
// (any version the loaded bundle holds) and which it WRITES is the current contract — so a version
// printed in help text is a claim that goes stale on the next promotion, and did: these said
// "APMap 1.0" while the converter wrote 1.1.
var directions = []Direction{
	{
		Name: "q1map-to-apmap", From: FormatQ1Map, To: FormatAPMap, Game: "quake1",
		Summary: "Quake 1 .map to APMap",
	},
	{
		Name: "apmap-to-q1map", From: FormatAPMap, To: FormatQ1Map, Game: "quake1",
		Summary: "APMap to Quake 1 .map",
	},
	{
		Name: "q2map-to-apmap", From: FormatQ2Map, To: FormatAPMap, Game: "quake2",
		Summary: "Quake 2 .map to APMap",
	},
	{
		Name: "apmap-to-q2map", From: FormatAPMap, To: FormatQ2Map, Game: "quake2",
		Summary: "APMap to Quake 2 .map",
	},
	{
		Name: "q2rmf-to-apmap", From: FormatQ2RMF, To: FormatAPMap, Game: "quake2",
		Summary: "Quake 2 Worldcraft .rmf to APMap",
	},
	{
		Name: "apmap-to-q2rmf", From: FormatAPMap, To: FormatQ2RMF, Game: "quake2",
		Summary: "APMap to Quake 2 Worldcraft .rmf",
	},
	{
		// The Go port of the editor's Quake III importer (ts/mapio quake3.ts + quake3Encode.ts), held
		// to it by shared conformance fixtures. The game is never guessed from a `.map`: a classic
		// Quake III face and a Quake 2 one look alike, so the caller names this direction.
		Name: "q3map-to-apmap", From: FormatQ3Map, To: FormatAPMap, Game: "quake3",
		Summary: "Quake III .map (classic, Valve 220, brushDef, patchDef2) to APMap",
	},
	{
		// Conversion only: there is no Quake III extraction anywhere in this build.
		Name: "apmap-to-q3map", From: FormatAPMap, To: FormatQ3Map, Game: "quake3",
		Summary: "APMap (game quake3) to Quake III .map for Q3Map2, with a manifest",
	},
}

// Directions returns the matrix in display order.
func Directions() []Direction {
	out := make([]Direction, len(directions))
	copy(out, directions)
	return out
}

// DirectionNames returns every direction name, sorted.
func DirectionNames() []string {
	names := make([]string, 0, len(directions))
	for _, direction := range directions {
		names = append(names, direction.Name)
	}
	sort.Strings(names)
	return names
}

// LookupDirection resolves a direction by name.
func LookupDirection(name string) (Direction, bool) {
	for _, direction := range directions {
		if direction.Name == name {
			return direction, true
		}
	}
	return Direction{}, false
}

// Options configures a conversion.
type Options struct {
	// Validate runs APMap schema and semantic validation. The zero value is false because Go has no
	// other choice; a caller producing compiler input sets it.
	Validate bool
	// Contract is the APMap contract this conversion reads and writes. Nil means the contract
	// embedded in this module, so a standalone program needs no configuration at all. A caller that
	// has been told to use another bundle parses it with apmap.ParseContract and passes it here.
	Contract *apmap.Contract
	// Schema is a schema the caller NAMES, which replaces version-matched validation: the document
	// is measured against this one whatever it declares. Nil means the contract's own schema for the
	// version each document declares. A caller holding a schema file reads it and calls
	// apmap.ParseSchema.
	Schema *apmap.Object
}

// Warning is one non-fatal observation about a conversion.
type Warning struct {
	// Code is the stable name of the observation. It is what a program branches on.
	Code WarningCode
	// Text is the human sentence, complete and ready to show.
	Text string
}

func (w Warning) String() string { return w.Text }

// WarningCode names a kind of Warning.
type WarningCode string

const (
	WarningEmptyBrushBlocksSkipped    WarningCode = "empty_brush_blocks_skipped"
	WarningClassicUVRebased           WarningCode = "classic_uv_rebased"
	WarningClassicUVUnrepresentable   WarningCode = "classic_uv_unrepresentable"
	WarningSemantic                   WarningCode = "semantic_warning"
	WarningQuake3Map                  WarningCode = "q3map_warning"
	WarningRMFBrushWithoutVertexCache WarningCode = "rmf_brush_without_vertex_cache"
	// WarningQuake3RedundantPlanes is q3map-to-apmap leaving out planes that bound nothing, by brush.
	WarningQuake3RedundantPlanes WarningCode = "q3map_redundant_planes"
	// WarningQuake3ImportRisk is a legality finding that did not refuse the import, by code and count.
	WarningQuake3ImportRisk WarningCode = "q3map_import_risk"
	// WarningQuake3ShaderPrefix is shader names written with `textures/`, stored without it.
	WarningQuake3ShaderPrefix WarningCode = "q3map_shader_prefix_removed"
)

// Result describes one completed conversion.
type Result struct {
	// Name is the input's file name, as the caller supplied it.
	Name string
	// OutputName is the file name the output should be written under: the
	// input's base name with the target format's extension.
	OutputName string
	Direction  string
	InputBytes int
	// OutputBytes is the size of the produced document.
	OutputBytes int
	// Duration is the whole conversion: parse, convert, validate, serialize.
	// It excludes reading the input file and writing the output, which belong
	// to the caller.
	Duration time.Duration
	// Validated reports whether APMap validation actually ran.
	Validated bool
	// Warnings are non-fatal observations, such as brushes whose planes did
	// not bound a solid when writing RMF.
	Warnings []Warning
	// Manifest is apmap-to-q3map's sidecar — source identity, shaders, models,
	// sounds, counts and every warning — and nil for every other direction.
	Manifest *apmap.Quake3MapManifest
	// ManifestName and ManifestBytes are that sidecar as the caller must write it, beside the
	// output. Both are empty when the direction writes none.
	ManifestName  string
	ManifestBytes []byte
	// Quake3Import is q3map-to-apmap's summary — counts, risks and every brush that lost a plane —
	// and nil for every other direction.
	Quake3Import *apmap.Quake3ImportSummary
}

// ValidationError is a conversion refused because an APMap document did not
// validate. It carries every problem rather than only the first, so one run
// reports everything wrong with a document.
//
// The two LAYERS are kept apart as well as reported together, and that is not
// bookkeeping: they are different faults with different fixes and, since
// OBS-05, different terminal reasons. A schema failure means the document says
// something this build does not accept — `schema_invalid`. A SEM-* failure
// means the document is well-formed and its GEOMETRY does not hold up:
// collinear plane points, faces that enclose no finite volume —
// `geometry_invalid`, which is the reason behind the sentence *"Candidate
// failed geometry validation."*
//
// The split is a FIELD rather than a prefix a caller has to look for in
// `Problems`. A classifier that read error text would be a taxonomy that
// silently reshuffled itself the day somebody reworded a message.
type ValidationError struct {
	Name string
	// Version is the current version of the contract the document was measured against.
	Version string
	// Problems is every problem, both layers, in the order they are reported.
	Problems []string
	// SchemaProblems and SemanticProblems are the same problems, separated.
	// Their concatenation is Problems.
	SchemaProblems   []string
	SemanticProblems []string

	// IdentityProblems is the SEM-8 / SEM-10 subset of SemanticProblems, from
	// `apmap.IdentityProblems` rather than from reading the messages back.
	//
	// It exists so a caller can tell a document whose GEOMETRY does not hold up
	// from one whose PROVENANCE does not, which are different faults with
	// different sentences (`AUP 120G`). Always a subset; never a fourth list.
	IdentityProblems []string
}

// Semantic reports whether any SEM-* rule was violated.
func (e *ValidationError) Semantic() bool { return len(e.SemanticProblems) > 0 }

// IdentityOnly reports a document that failed the CONTRACT and nothing else:
// every semantic problem it has is a SEM-8 / SEM-10 identity-policy violation.
//
// Such a document parsed, its geometry is coherent, and what it got wrong is the
// claim `id_policy: "derived"` makes about its own object ids. Telling its
// author to check their brushes for degenerate solids is advice about the one
// thing that is fine (`AUP 120G`).
func (e *ValidationError) IdentityOnly() bool {
	return len(e.IdentityProblems) > 0 && len(e.IdentityProblems) == len(e.SemanticProblems)
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s is not a valid APMap %s document:\n  %s",
		e.Name, e.Version, strings.Join(e.Problems, "\n  "))
}

// Error is a conversion failure that is not a validation failure.
type Error struct {
	Message string
	// Code is the refusal's name; empty means RefusalInvalidInput.
	Code RefusalCode
}

func (e *Error) Error() string { return e.Message }

func fail(format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// RefusalCode names why a conversion refused a document.
type RefusalCode string

const (
	// RefusalValidation is an APMap document that failed the schema or the semantic rules.
	RefusalValidation RefusalCode = "validation_failed"
	// RefusalVersion is a document declaring a version the contract does not hold, or none.
	RefusalVersion RefusalCode = "unsupported_version"
	// RefusalGame is a document routed to a direction for another game.
	RefusalGame RefusalCode = "unsupported_game"
	// RefusalQuake2Admission is a Quake II document its compiler could not build (ADM-Q2-*).
	RefusalQuake2Admission RefusalCode = "quake2_admission"
	// RefusalQuake3Map is a document the Quake III writer will not write; Refusal.Detail is its code.
	RefusalQuake3Map RefusalCode = "quake3_map"
	// RefusalQuake3Surface is a Quake III surface sent to a format that has no place for it.
	RefusalQuake3Surface RefusalCode = "quake3_surface"
	// RefusalQuake3Import is a Quake III `.map` q3map-to-apmap refused whole; Refusal.Detail is its
	// code (quake3_import_parse, quake3_import_brush_illegal, quake3_import_empty, apmap_encode_*).
	RefusalQuake3Import RefusalCode = "quake3_import"
	// RefusalCancelled is a conversion stopped by its context.
	RefusalCancelled RefusalCode = "cancelled"
	// RefusalTooLarge is an input over a direction's size bound.
	RefusalTooLarge RefusalCode = "input_too_large"
	// RefusalInvalidInput is an input that could not be read as its format, or that the target
	// format cannot represent.
	RefusalInvalidInput RefusalCode = "invalid_input"
)

// Refusal is a conversion error classified for a program: Code is stable, Detail narrows it where the
// format has its own vocabulary, and Message is the complete human sentence.
type Refusal struct {
	Code    RefusalCode
	Detail  string
	Message string
}

// RefusalOf classifies an error Convert returned. Nothing is lost by it: the original typed error is
// still there for errors.As.
func RefusalOf(err error) Refusal {
	refusal := Refusal{Code: RefusalInvalidInput, Message: err.Error()}
	var (
		validation *ValidationError
		game       *GameError
		convert    *Error
		version    *apmap.VersionError
		admission  *apmap.Quake2AdmissionError
		quake3     *apmap.Quake3MapError
		surface    *apmap.Quake3UnsupportedError
		imported   *apmap.Quake3ImportError
		syntax     *Quake3SyntaxError
	)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		refusal.Code = RefusalCancelled
	case errors.As(err, &validation):
		refusal.Code = RefusalValidation
	case errors.As(err, &game), errors.As(err, &syntax):
		refusal.Code = RefusalGame
	case errors.As(err, &imported):
		refusal.Code = RefusalQuake3Import
		refusal.Detail = imported.Code
		if imported.Code == apmap.Quake3ImportTooLarge {
			refusal.Code = RefusalTooLarge
		}
	case errors.As(err, &version):
		refusal.Code = RefusalVersion
	case errors.As(err, &admission):
		refusal.Code = RefusalQuake2Admission
		refusal.Detail = admission.Code
	case errors.As(err, &quake3):
		refusal.Code = RefusalQuake3Map
		refusal.Detail = quake3.Code
		if quake3.Code == apmap.Q3MapCodeCancelled {
			refusal.Code = RefusalCancelled
		}
	case errors.As(err, &surface):
		refusal.Code = RefusalQuake3Surface
	case errors.As(err, &convert) && convert.Code != "":
		refusal.Code = convert.Code
	}
	return refusal
}

// OutputName is the file name a conversion of input produces.
func OutputName(direction Direction, name string) string {
	base := filepath.Base(name)
	if extension := filepath.Ext(base); extension != "" {
		base = strings.TrimSuffix(base, extension)
	}
	if base == "" || base == "." {
		base = "converted"
	}
	return base + direction.To.Extension()
}

// documentIDAllowed reports whether r may appear in a document_id after the
// first character. The set is the schema's, not a guess:
// ^[0-9A-Za-z][0-9A-Za-z._:-]{0,127}$.
func documentIDAllowed(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		return true
	case r == '.', r == '_', r == ':', r == '-':
		return true
	}
	return false
}

// DocumentID derives a schema-valid document_id from an input file name.
//
// The file name is the only thing a format conversion knows about the
// document's identity, so it is what the id is built from — a converted
// `e1m1.map` becomes document `e1m1.map`, which is legible and stable across
// runs. Characters the schema does not allow become "-", a name that would
// start with one of them is prefixed, and the result is truncated to the
// schema's 128-character limit.
//
// Determinism matters more than prettiness here: two runs over the same corpus
// must produce the same ids, and an id that silently varied would defeat the
// content-hash identity the objects inside the document already carry.
func DocumentID(name string) string {
	base := filepath.Base(name)

	var builder strings.Builder
	for _, r := range base {
		if documentIDAllowed(r) {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('-')
	}
	id := builder.String()

	if id == "" || id == "." || id == ".." {
		return "converted"
	}
	// The first character is restricted to alphanumerics, so a leading "." or
	// "-" — which a dotfile or a sanitised name can easily produce — is
	// prefixed rather than stripped, so that ".map" and "map" stay distinct.
	if first := rune(id[0]); !(first >= '0' && first <= '9') &&
		!(first >= 'A' && first <= 'Z') && !(first >= 'a' && first <= 'z') {
		id = "x" + id
	}
	if len(id) > 128 {
		id = id[:128]
	}
	return id
}

// Convert performs one conversion. name is the input's file name, used in error
// messages, in the produced document's provenance, and to derive the output
// name; it is never opened.
//
// ctx stops the conversion between its stages and between entities. A stopped conversion returns no
// output and an error RefusalOf reports as RefusalCancelled.
func Convert(ctx context.Context, direction Direction, name string, data []byte, opts Options) ([]byte, Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	if opts.Contract == nil {
		contract, err := apmap.EmbeddedContract()
		if err != nil {
			return nil, Result{Name: name, Direction: direction.Name}, err
		}
		opts.Contract = contract
	}
	result := Result{
		Name:       name,
		OutputName: OutputName(direction, name),
		Direction:  direction.Name,
		InputBytes: len(data),
	}

	var (
		output []byte
		err    error
	)
	switch direction.To {
	case FormatAPMap:
		output, err = toAPMap(ctx, direction, name, data, opts, &result)
	default:
		output, err = fromAPMap(ctx, direction, name, data, opts, &result)
	}
	if err != nil {
		return nil, result, err
	}

	result.OutputBytes = len(output)
	if result.Manifest != nil {
		sourceDigest := sha256.Sum256(data)
		outputDigest := sha256.Sum256(output)
		result.Manifest.Source = &apmap.Quake3MapFile{
			Name: filepath.Base(name), SHA256: hex.EncodeToString(sourceDigest[:]), Bytes: len(data)}
		result.Manifest.Output = &apmap.Quake3MapFile{
			Name: result.OutputName, SHA256: hex.EncodeToString(outputDigest[:]), Bytes: len(output)}
		manifest, err := EncodeManifest(result.Manifest)
		if err != nil {
			return nil, result, err
		}
		result.ManifestName = ManifestName(direction, name)
		result.ManifestBytes = manifest
	}
	result.Duration = time.Since(started)
	return output, result, nil
}

// ManifestName is the file a conversion's manifest is written under, beside its output, or ""
// for a direction that writes none.
func ManifestName(direction Direction, name string) string {
	if direction.To != FormatQ3Map {
		return ""
	}
	output := OutputName(direction, name)
	return strings.TrimSuffix(output, filepath.Ext(output)) + ".q3map-manifest.json"
}

// EncodeManifest is a manifest's bytes: indented JSON with a trailing newline, a pure function of
// the manifest.
func EncodeManifest(manifest *apmap.Quake3MapManifest) ([]byte, error) {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// GameError is a document routed to a direction for another game: a Quake 1 APMap sent to the
// Quake III writer, or a Quake III APMap sent to a Quake 1 / Quake 2 one. Refused before anything
// is written — a Quake III map without its patches, or a Quake 1 map spelled as Quake III, is a
// different map that looks like the right one.
type GameError struct {
	Name      string
	Direction string
	Game      string
}

func (e *GameError) Error() string {
	// The caller prefixes the file name (the CLI's per-file line, the HTTP 422), so it is not repeated.
	return fmt.Sprintf("unsupported game: the document is a Quake III APMap (game \"quake3\"), and %s "+
		"writes a Quake 1/Quake 2 format that has no place for Quake III surfaces; convert it with apmap-to-q3map",
		e.Direction)
}

// Quake3SyntaxError is a Quake 1 or Quake 2 read of a file that holds Quake III-only syntax: said to
// be one, by the direction that reads it, rather than failing as a malformed Quake 1 map.
type Quake3SyntaxError struct {
	Name      string
	Direction string
	// Cause is the parse error the Quake 1 / Quake 2 reader stopped at.
	Cause error
}

func (e *Quake3SyntaxError) Error() string {
	return fmt.Sprintf("unsupported game: %s holds Quake III brush syntax (brushDef or patchDef2), which %s does not read; "+
		"convert it with q3map-to-apmap (%v)", e.Name, e.Direction, e.Cause)
}

func (e *Quake3SyntaxError) Unwrap() error { return e.Cause }

// toAPMap handles every direction whose target is APMap.
func toAPMap(ctx context.Context, direction Direction, name string, data []byte, opts Options, result *Result) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if direction.From == FormatQ3Map {
		return quake3ToAPMap(ctx, direction, name, data, opts, result)
	}
	geometry, err := readGeometry(direction, name, data)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if skipped := geometry.EmptyBrushBlocksSkipped(); skipped > 0 {
		result.Warnings = append(result.Warnings, Warning{Code: WarningEmptyBrushBlocksSkipped,
			Text: fmt.Sprintf("empty_brush_blocks_skipped: %d", skipped)})
	}

	// Identity is derived from the source file and its content hash, which is
	// SourceMapIdentity's documented purpose and the only one of the three
	// identity kinds a conversion can honestly claim: the objects genuinely
	// come from this file, and re-converting a modified file must not reuse
	// the previous ids.
	digest := sha256.Sum256(data)
	// Q2_002A: a Classic face SER-7 would have moved is stored as the numbers that keep its texels (counted), or,
	// when no Classic spelling can, kept literal and NAMED by address and movement — never silently.
	rebased := 0
	var unrepresentable []string
	document, err := apmap.FromMapDocument(geometry, apmap.ConvertOptions{
		Contract: opts.Contract,
		Context:  ctx,
		OnClassicFidelity: func(path string, fidelity apmap.ClassicFidelity) {
			if fidelity.Outcome == apmap.ClassicRebased {
				rebased++
				return
			}
			unrepresentable = append(unrepresentable, fmt.Sprintf("%s (%.3f texels)", path, fidelity.ErrorTexels))
		},
		Identity: &apmap.SourceMapIdentity{
			SourceMap:    filepath.Base(name),
			SourceSHA256: hex.EncodeToString(digest[:]),
		},
		Game: direction.Game,
		// Converting a whole file is what full_map means. The other three
		// roles describe a prefab or a cropped neighbourhood, which is a
		// claim about provenance that a plain format conversion cannot make.
		Role:       apmap.RoleFullMap,
		DocumentID: DocumentID(name),
		// Q2_002 (finding F20): the dialect the faces are written in. AULIBS SEMANTICS §game
		// settles it — `map_dialect` names the face projection form, `quake_standard` or
		// `valve_220` — and AUP's encoder already reads it so: a document whose faces are
		// Valve 220 declared `quake_standard` here, and the editor then authored every new face
		// classic inside a Valve 220 map. Any Valve 220 face makes it a `valve_220` document,
		// the AUP rule exactly.
		MapDialect: dialectOfFaces(geometry),
		Source:     name,
	})
	if err != nil {
		return nil, err
	}
	if rebased > 0 {
		result.Warnings = append(result.Warnings, Warning{Code: WarningClassicUVRebased,
			Text: fmt.Sprintf("classic_uv_rebased: %d face(s) stored as the Classic numbers that keep their texels on six-decimal points", rebased)})
	}
	if len(unrepresentable) > 0 {
		result.Warnings = append(result.Warnings, Warning{Code: WarningClassicUVUnrepresentable,
			Text: fmt.Sprintf("classic_uv_unrepresentable: %d face(s) cannot keep their texture alignment in Classic numbers: %s",
				len(unrepresentable), strings.Join(unrepresentable, ", "))})
	}

	if err := validateDocument(ctx, document, name, opts, result); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	encoded, err := apmap.Encode(document)
	if err != nil {
		return nil, err
	}
	return []byte(encoded), nil
}

// dialectOfFaces is the document dialect a parsed `.map` is written in: `valve_220` when any face
// carries Valve 220 axes, `quake_standard` otherwise.
func dialectOfFaces(geometry *mapfile.MapFile) string {
	for _, entity := range geometry.Entities() {
		for _, item := range entity.Items() {
			brush, ok := item.(mapfile.Brush)
			if !ok {
				continue
			}
			for index := 0; index < brush.FaceCount(); index++ {
				if brush.Face(index).TextureMode() == mapfile.ModeValve220 {
					return "valve_220"
				}
			}
		}
	}
	return apmap.DefaultDialect
}

// readGeometry parses the non-APMap side of a conversion.
func readGeometry(direction Direction, name string, data []byte) (*mapfile.MapFile, error) {
	switch direction.From {
	case FormatQ1Map, FormatQ2Map:
		// The same parser reads both games. It is not a simplification: a
		// Quake 2 `.map` differs from a Quake 1 one only by the three trailing
		// content/surface/value fields per face, which mapfile reads
		// for either and which a Quake 1 file simply does not have. The
		// direction still distinguishes the two, because the `game` field of
		// the produced document is not something the syntax reveals.
		geometry, err := mapfile.ParseNamed(string(data), name)
		if err != nil && quake3map.HasQuake3OnlySyntax(string(data)) {
			return nil, &Quake3SyntaxError{Name: name, Direction: direction.Name, Cause: err}
		}
		return geometry, err
	case FormatQ2RMF:
		document, _, err := rmf.Decode(data)
		return document, err
	default:
		return nil, fail("%s does not read %s", direction.Name, direction.From)
	}
}

// fromAPMap handles every direction whose source is APMap.
func fromAPMap(ctx context.Context, direction Direction, name string, data []byte, opts Options, result *Result) ([]byte, error) {
	// Bounded before it is decoded: the decoded tree is what costs memory.
	if direction.To == FormatQ3Map && len(data) > MaxQuake3MapInputBytes {
		return nil, &Error{Code: RefusalTooLarge, Message: fmt.Sprintf(
			"%s is %d bytes; apmap-to-q3map converts documents up to %d bytes",
			name, len(data), MaxQuake3MapInputBytes)}
	}
	if err := ctx.Err(); err != nil && direction.To != FormatQ3Map {
		return nil, err
	}
	document, err := apmap.DecodeJSON(data)
	if err != nil {
		return nil, fail("%s is not valid JSON: %v", name, err)
	}

	// The game gate runs before validation: a Quake III document is valid APMap, and validating it
	// first would only postpone the answer that it cannot be written in this direction.
	if root, ok := document.(*apmap.Object); ok && direction.To != FormatQ3Map {
		if game, _ := root.Get("game"); game == apmap.Quake3MapGame {
			return nil, &GameError{Name: name, Direction: direction.Name, Game: apmap.Quake3MapGame}
		}
	}
	if direction.To == FormatQ3Map {
		// Decoding is the one step that cannot stop part-way; an interrupt that arrived during it
		// is honoured here, before validation spends as long again.
		if ctx.Err() != nil {
			return nil, &apmap.Quake3MapError{Code: apmap.Q3MapCodeCancelled,
				Message: fmt.Sprintf("the conversion was cancelled (%v)", ctx.Err())}
		}
		if err := apmap.CheckQuake3MapIdentityFor(opts.Contract, document); err != nil {
			return nil, err
		}
	}

	// Validation runs before conversion, not after. A document that fails the
	// schema can still often be converted into *something*, and producing that
	// something would be the worse outcome: the caller asked for a check and
	// would get a file back anyway.
	if err := validateDocument(ctx, document, name, opts, result); err != nil {
		return nil, err
	}

	switch direction.To {
	case FormatQ1Map, FormatQ2Map:
		// A Quake II `.map` is what a Quake II compiler reads, so its tails and resource names are
		// admitted first (ADM-Q2-1..4, Q2_001). The document is valid either way; the refusal is
		// about the build, and Quake 1 output is unchanged.
		if direction.To == FormatQ2Map {
			if err := apmap.Quake2Admission(document); err != nil {
				return nil, err
			}
		}
		text, err := apmap.ExportMapTextContext(ctx, document)
		if err != nil {
			return nil, err
		}
		return []byte(text), nil
	case FormatQ3Map:
		text, manifest, err := apmap.ExportQuake3MapText(document, apmap.Quake3MapOptions{Context: ctx, Contract: opts.Contract})
		if err != nil {
			return nil, err
		}
		for _, warning := range manifest.Warnings {
			result.Warnings = append(result.Warnings, Warning{Code: WarningQuake3Map, Text: "q3map_warning: " + warning.String()})
		}
		result.Manifest = manifest
		return []byte(text), nil
	case FormatQ2RMF:
		encoded, stats, err := rmf.EncodeFromAPMapContext(ctx, document)
		if err != nil {
			return nil, err
		}
		if stats.UncachedBrushes > 0 {
			// Not an error: the geometry is written correctly either way, only
			// Worldcraft's polygon cache is left empty. Reported so a caller
			// converting a corpus can see it happened.
			result.Warnings = append(result.Warnings, Warning{Code: WarningRMFBrushWithoutVertexCache, Text: fmt.Sprintf(
				"%d brush(es) had planes that do not bound a solid; their faces were "+
					"written without Worldcraft's vertex cache", stats.UncachedBrushes)})
		}
		return encoded, nil
	default:
		return nil, fail("%s does not write %s", direction.Name, direction.To)
	}
}

// validateDocument runs both APMap validation layers, unless validation is off.
//
// Off skips the schema and the SEM-* layers, never the VERSION GATE (Q2_001): a document declaring
// a version this build cannot read is not a document validation could have accepted, and without
// the gate an unvalidated conversion was a door through which a future APMap was converted as if it
// were current. A caller that names its own schema has already said which version it means.
func validateDocument(ctx context.Context, document apmap.Value, name string, opts Options, result *Result) error {
	if !opts.Validate {
		if opts.Schema == nil {
			return opts.Contract.GateDeclaredVersion(document)
		}
		return nil
	}

	// THE CONTRACT THIS DOCUMENT IS MEASURED AGAINST.
	//
	// A schema the caller supplied means it has NAMED a contract, and the document is validated
	// against that one whatever it declares. Otherwise the contract is version-matched: the gate and
	// the schema choice are one decision, made in Contract.SchemaForDocument.
	schema := opts.Schema
	if schema == nil {
		matched, err := opts.Contract.SchemaForDocument(document)
		if err != nil {
			return err
		}
		schema = matched
	}

	schemaProblems, err := apmap.ValidateAgainstSchemaContext(ctx, document, schema)
	if err != nil {
		// A defect in the schema, or a cancellation; not a fault in the document.
		return err
	}
	// Both layers always, and reported together: making an author fix one layer and re-run to
	// discover the next is worse than one complete answer. Kept apart as well, because they are
	// different faults; see ValidationError.
	semanticProblems, warnings, identityProblems, err := apmap.SemanticReport(ctx, opts.Contract, document)
	if err != nil {
		return err
	}
	// Non-fatal findings — SEM-G-5 on a document declaring an older, readable version — are
	// reported as advice and never refuse the document (apmap.SemanticWarnings).
	for _, warning := range warnings {
		result.Warnings = append(result.Warnings, Warning{Code: WarningSemantic, Text: "semantic_warning: " + warning})
	}

	result.Validated = true
	if len(schemaProblems)+len(semanticProblems) > 0 {
		return &ValidationError{
			Name:             name,
			Version:          opts.Contract.Version,
			Problems:         append(append([]string{}, schemaProblems...), semanticProblems...),
			SchemaProblems:   schemaProblems,
			SemanticProblems: semanticProblems,
			IdentityProblems: identityProblems,
		}
	}
	return nil
}
