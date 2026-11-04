package apmap

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/legality"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/quake3map"
)

// A Quake III `.map` as an APMap document — the import half of the Quake III codec, and the Go port
// of the TypeScript reference (`ts/mapio/src/apmap/quake3Encode.ts`). The two produce the same
// document for the same file bytes, name and digest, byte for byte except `provenance.producer` and
// `provenance.producer_version`, which name the implementation that wrote it.
//
//   - `game: quake3`, and `map_dialect: valve_220` for a map holding any Valve 220 brush,
//     `quake3_extended` otherwise.
//   - Content keeps the file's order: a key written after a brush stays after it.
//   - A brushDef face keeps its matrix verbatim and a patch every control-point number verbatim
//     (SER-7Q); every other number is SER-7, rounded as the reference rounds it.
//   - Shader names carry no `textures/` prefix: Q3Map2 prepends it, so a name written WITH it is
//     stored once without it, and counted. Nothing else about a name is changed.
//   - Every brush crosses the legality check, and the import is refused whole — naming each brush by
//     entity, brush and source line — when one is impossible. Nothing is imported as "the brushes
//     that were fine".
//   - A plane that bounds nothing is left out and reported, when the brush without it is sound; the
//     faces that stay keep the ids their positions in the FILE give them.
//
// Identity is `source_map` derivation over the file's own name and digest, at `e<n>`, `e<n>.b<n>`,
// `e<n>.b<n>.f<n>` and `e<n>.p<n>`, brushes and patches counted separately.

// Quake3ImportProducer and Quake3ImportProducerVersion are what a document this importer writes says
// produced it. The version is this importer's output contract, not the module's: a binary's module
// version is in its build info (mapio.BuildModuleVersion), and a document whose bytes changed with
// every build would forfeit the reproducibility SER-8 asks for.
const (
	Quake3ImportProducer        = "github.com/auto-pigeon/auto-pigeon-libraries/go/mapio"
	Quake3ImportProducerVersion = "1.0.0"
	// Quake3ImportPipeline is the pipeline step a Quake III import records — the reference's.
	Quake3ImportPipeline = "quake3-map-import"
	// Quake3MapDialect is the dialect of a Quake III map not written in Valve 220.
	Quake3MapDialect = "quake3_extended"
	// Quake3ShaderPrefix is the prefix Q3Map2 adds to every shader name itself.
	Quake3ShaderPrefix = "textures/"
	// MaxQuake3BrushFaces bounds one brush. Legality is quadratic in a brush's faces and its vertex
	// search cubic up to 64; a thousand faces is far past anything an editor writes, and the bound
	// is what keeps one hostile brush from holding a converter for minutes.
	MaxQuake3BrushFaces = 1024
)

const (
	maxQuake3PatchCoordinate   = 131072
	maxQuake3MatrixCoefficient = 1e6
)

// The Quake III import refusal codes. The first three are the reference's Quake3ImportError codes;
// the `apmap_encode_*` ones are its ApmapEncodeError codes; Quake3ImportTooLarge is this port's own
// work bound.
const (
	Quake3ImportParse           = "quake3_import_parse"
	Quake3ImportBrushIllegal    = "quake3_import_brush_illegal"
	Quake3ImportEmpty           = "quake3_import_empty"
	Quake3ImportTooLarge        = "quake3_import_too_large"
	Quake3EncodeSourceSHA256    = "apmap_encode_source_sha256"
	Quake3EncodeDocumentID      = "apmap_encode_document_id"
	Quake3EncodeTextureInvalid  = "apmap_encode_texture_invalid"
	Quake3EncodeMatrixRange     = "apmap_encode_matrix_range"
	Quake3EncodePatchRange      = "apmap_encode_patch_range"
	Quake3EncodePropertyInvalid = "apmap_encode_property_invalid"
	Quake3EncodeEntityEmpty     = "apmap_encode_entity_empty"
	Quake3EncodeNonFinite       = "apmap_encode_non_finite"
)

// Quake3ImportRefusedCodes are the legality codes that make a brush impossible rather than risky.
// texture_basis_degenerate is NOT here, and that was measured: a real Quake III source has hundreds
// of Valve 220 faces with parallel axes and Q3Map2 compiles it. The one degenerate texture that IS
// refused is a brushDef matrix with determinant 0, because SEM-Q-3 makes that document invalid.
var Quake3ImportRefusedCodes = map[string]bool{
	legality.CodeNonFinite: true, legality.CodeUnserializable: true, legality.CodeNotConvex: true,
	legality.CodeWindingInward: true, legality.CodeWindingMixed: true,
}

// Quake3RejectedBrush is one brush the import refused, by address and line — never its numbers or
// its shader names.
type Quake3RejectedBrush struct {
	Entity int      `json:"entity"`
	Brush  int      `json:"brush"`
	Line   int      `json:"line"`
	Codes  []string `json:"codes"`
}

// Quake3RedundantPlaneBrush is one brush imported WITHOUT some of the planes its source declares:
// Faces are the indices, in the file's order, of the planes that bound no part of the solid.
type Quake3RedundantPlaneBrush struct {
	Entity int   `json:"entity"`
	Brush  int   `json:"brush"`
	Line   int   `json:"line"`
	Faces  []int `json:"faces"`
}

// Quake3ImportSummary is what the import did, in counts — the reference's summary, member for
// member and under its names. No map content.
type Quake3ImportSummary struct {
	Entities              int            `json:"entities"`
	Properties            int            `json:"properties"`
	Brushes               int            `json:"brushes"`
	ClassicBrushes        int            `json:"classicBrushes"`
	Valve220Brushes       int            `json:"valve220Brushes"`
	BrushPrimitiveBrushes int            `json:"brushPrimitiveBrushes"`
	Faces                 int            `json:"faces"`
	Patches               int            `json:"patches"`
	PrefixedShaders       int            `json:"prefixedShaders"`
	Shaders               int            `json:"shaders"`
	Risks                 map[string]int `json:"risks"`
	RedundantPlanes       int            `json:"redundantPlanes"`
	// RedundantPlaneBrushes is every brush that lost a plane, so nothing was left out silently.
	RedundantPlaneBrushes []Quake3RedundantPlaneBrush `json:"redundantPlaneBrushes"`
}

// Quake3ImportError is a Quake III import refused whole. Code is one of the Quake3Import* /
// Quake3Encode* codes; ParseErrors and Rejected carry every fault, not only the first.
type Quake3ImportError struct {
	Code        string
	Message     string
	Rule        string
	ParseErrors []quake3map.ParseError
	Rejected    []Quake3RejectedBrush
}

func (e *Quake3ImportError) Error() string { return e.Message }
func (e *Quake3ImportError) Unwrap() error { return ErrAPMap }

// Quake3ImportOptions are a Quake III import's identity inputs.
type Quake3ImportOptions struct {
	// SourceMap is the source file's name; its last path segment is used.
	SourceMap string
	// SourceSHA256 is the SHA-256 of the file's bytes, lowercase hex.
	SourceSHA256 string
	// DocumentID overrides the derived `<stem>:full_map`.
	DocumentID string
	// Contract is the contract the document is written in; nil means the process default.
	Contract *Contract
	// Context stops the import between brushes; a stopped import returns no document.
	Context context.Context
}

var (
	quake3DocumentID = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._:-]{0,127}$`)
	quake3SHA256     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// BareSourceMapName is the reference's `bareSourceMapName`: the last `/` or `\` segment, "" for none.
func BareSourceMapName(name string) string {
	if index := strings.LastIndexAny(name, `/\`); index >= 0 {
		return name[index+1:]
	}
	return name
}

// Quake3DocumentID is the document id a Quake III import derives from a source name: the stem
// without `.map`, every run of other characters one `-`, leading non-alphanumerics dropped, and
// `:full_map` after it — the reference's `documentIdFor`.
func Quake3DocumentID(sourceMap string) string {
	stem := sourceMap
	if len(stem) >= 4 && strings.EqualFold(stem[len(stem)-4:], ".map") {
		stem = stem[:len(stem)-4]
	}
	var builder strings.Builder
	run := false
	for _, r := range stem {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '.' || r == '_' || r == '-' {
			builder.WriteRune(r)
			run = false
			continue
		}
		if !run {
			builder.WriteByte('-')
		}
		run = true
	}
	stem = strings.TrimLeftFunc(builder.String(), func(r rune) bool {
		return !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
	})
	if stem == "" {
		stem = "map"
	}
	id := stem + ":full_map"
	if len(id) > 128 {
		id = id[:128]
	}
	return id
}

// Quake3ShaderName is the shader name an APMap document stores: the name without a leading
// `textures/` (compared without case), and otherwise exactly as written.
func Quake3ShaderName(name string) string {
	if len(name) >= len(Quake3ShaderPrefix) {
		for index := 0; index < len(Quake3ShaderPrefix); index++ {
			c := name[index]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != Quake3ShaderPrefix[index] {
				return name
			}
		}
		return name[len(Quake3ShaderPrefix):]
	}
	return name
}

// jsSER7 is the reference's `canonicalApmapNumber`: JavaScript's `Math.round(value * 1e6) / 1e6`
// with -0 as 0. It is the reference's arithmetic on purpose, not Go's decimal round-half-even, so
// a Quake III import rounds every SER-7 number exactly as the editor's does; Encode then finds each
// value already canonical.
func jsSER7(value float64) float64 {
	scaled := float64(value * 1e6)
	rounded := math.Floor(scaled)
	if float64(scaled-rounded) >= 0.5 {
		rounded++
	}
	result := rounded / 1e6
	if result == 0 {
		return 0
	}
	return result
}

// number is the JSON spelling of a float: an integer as an integer, else the shortest round trip.
func number(value float64) (json.Number, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", newError("SEM-11 non-finite number: %v", value)
	}
	if value == math.Trunc(value) && math.Abs(value) < 1e18 {
		return json.Number(strconv.FormatInt(int64(value), 10)), nil
	}
	return encodeNumber(value)
}

type quake3Importer struct {
	ctx          context.Context
	sourceMap    string
	sourceSHA256 string
	summary      *Quake3ImportSummary
	shaderNames  map[string]bool
	rejected     []Quake3RejectedBrush
	checked      int
}

func encodeRefusal(code, rule, format string, args ...any) *Quake3ImportError {
	return &Quake3ImportError{Code: code, Rule: rule, Message: fmt.Sprintf(format, args...)}
}

func (q *quake3Importer) identity(kind Kind, path string) (string, *Object, error) {
	id, err := DeriveID(kind, q.sourceMap, q.sourceSHA256, path)
	if err != nil {
		return "", nil, err
	}
	record := NewObject()
	record.Set("kind", "source_map")
	if q.sourceMap != "" {
		record.Set("source_map", q.sourceMap)
	}
	record.Set("source_sha256", q.sourceSHA256)
	record.Set("source_path", path)
	return id, record, nil
}

func (q *quake3Importer) storedShader(name, path string) (string, error) {
	stored := Quake3ShaderName(name)
	if !legality.Nameable(stored) {
		return "", encodeRefusal(Quake3EncodeTextureInvalid, "SCH-14", "%s: the shader name contains whitespace or a quote", path)
	}
	return stored, nil
}

func (q *quake3Importer) countShader(name, stored string) {
	if stored != name {
		q.summary.PrefixedShaders++
	}
	q.shaderNames[stored] = true
}

// numbers spells SER-7 values.
func jsSER7Values(values []float64) ([]Value, error) {
	out := make([]Value, 0, len(values))
	for _, value := range values {
		spelled, err := number(jsSER7(value))
		if err != nil {
			return nil, err
		}
		out = append(out, spelled)
	}
	return out, nil
}

// exact spells a SER-7Q value: the double itself, -0 as 0.
func exact(value float64) (json.Number, error) {
	if value == 0 {
		value = 0
	}
	return number(value)
}

// face is one face in the document, and its rounded values for the legality check.
func (q *quake3Importer) face(source quake3map.Face, path string) (*Object, legality.Face, error) {
	id, derived, err := q.identity(KindFace, path)
	if err != nil {
		return nil, legality.Face{}, err
	}
	checked := legality.Face{}
	points := make([]Value, 0, 3)
	for index, point := range source.Points {
		for axis := 0; axis < 3; axis++ {
			checked.Points[index][axis] = jsSER7(point[axis])
		}
		spelled, err := jsSER7Values(point[:])
		if err != nil {
			return nil, legality.Face{}, err
		}
		points = append(points, spelled)
	}
	texture, err := q.storedShader(source.Texture, path)
	if err != nil {
		return nil, legality.Face{}, err
	}
	checked.Texture = texture

	projection := NewObject()
	switch {
	case source.Valve != nil:
		valve := source.Valve
		checked.Projection = legality.Projection{Mode: legality.ModeValve220, Rotation: jsSER7(valve.Rotation),
			Scale: [2]float64{jsSER7(valve.Scale[0]), jsSER7(valve.Scale[1])}}
		for axis := 0; axis < 4; axis++ {
			checked.Projection.U[axis] = jsSER7(valve.U[axis])
			checked.Projection.V[axis] = jsSER7(valve.V[axis])
		}
		u, err := jsSER7Values(valve.U[:])
		if err != nil {
			return nil, legality.Face{}, err
		}
		v, err := jsSER7Values(valve.V[:])
		if err != nil {
			return nil, legality.Face{}, err
		}
		rotation, err := number(checked.Projection.Rotation)
		if err != nil {
			return nil, legality.Face{}, err
		}
		scale, err := jsSER7Values(valve.Scale[:])
		if err != nil {
			return nil, legality.Face{}, err
		}
		projection.Set("mode", "valve220")
		projection.Set("u_axis", u)
		projection.Set("v_axis", v)
		projection.Set("rotation", rotation)
		projection.Set("scale", scale)
	case source.Matrix != nil:
		checked.Projection = legality.Projection{Mode: legality.ModeBrushPrimitives, Matrix: *source.Matrix}
		rows := make([]Value, 0, 2)
		for _, row := range source.Matrix {
			values := make([]Value, 0, 3)
			for _, value := range row {
				if math.Abs(value) > maxQuake3MatrixCoefficient {
					return nil, legality.Face{}, encodeRefusal(Quake3EncodeMatrixRange, "SCH-Q-7",
						"%s: a texture matrix coefficient is outside ±%d", path, int(maxQuake3MatrixCoefficient))
				}
				spelled, err := exact(value)
				if err != nil {
					return nil, legality.Face{}, err
				}
				values = append(values, spelled)
			}
			rows = append(rows, values)
		}
		projection.Set("mode", "brush_primitives")
		projection.Set("matrix", rows)
	default:
		classic := source.Classic
		checked.Projection = legality.Projection{Mode: legality.ModeClassic,
			Shift:    [2]float64{jsSER7(classic.Shift[0]), jsSER7(classic.Shift[1])},
			Rotation: jsSER7(classic.Rotation), Scale: [2]float64{jsSER7(classic.Scale[0]), jsSER7(classic.Scale[1])}}
		shift, err := jsSER7Values(classic.Shift[:])
		if err != nil {
			return nil, legality.Face{}, err
		}
		rotation, err := number(checked.Projection.Rotation)
		if err != nil {
			return nil, legality.Face{}, err
		}
		scale, err := jsSER7Values(classic.Scale[:])
		if err != nil {
			return nil, legality.Face{}, err
		}
		projection.Set("mode", "classic")
		projection.Set("shift", shift)
		projection.Set("rotation", rotation)
		projection.Set("scale", scale)
	}

	plane := NewObject()
	plane.Set("points", points)
	face := NewObject()
	face.Set("face_id", id)
	face.Set("derived_from", derived)
	face.Set("plane", plane)
	face.Set("texture", texture)
	face.Set("projection", projection)
	if len(source.Tail) > 0 {
		tail, err := q.tail(source.Tail, path)
		if err != nil {
			return nil, legality.Face{}, err
		}
		face.Set("tail", tail)
	}
	return face, checked, nil
}

// tail is a SER-7 tail. A tail number that rounds to no finite value is refused: the reference would
// write `null` there, which is not a document.
func (q *quake3Importer) tail(values []float64, path string) ([]Value, error) {
	out := make([]Value, 0, len(values))
	for _, value := range values {
		rounded := jsSER7(value)
		if math.IsNaN(rounded) || math.IsInf(rounded, 0) {
			return nil, encodeRefusal(Quake3EncodeNonFinite, "SEM-11", "%s: a tail value is too large for APMap to write", path)
		}
		spelled, err := number(rounded)
		if err != nil {
			return nil, err
		}
		out = append(out, spelled)
	}
	return out, nil
}

func determinantIsZero(matrix [2][3]float64) bool {
	return float64(float64(matrix[0][0]*matrix[1][1])-float64(matrix[0][1]*matrix[1][0])) == 0
}

func (q *quake3Importer) brush(source *quake3map.Brush, entityIndex, brushIndex int) (*Object, error) {
	path := fmt.Sprintf("e%d.b%d", entityIndex, brushIndex)
	if len(source.Faces) > MaxQuake3BrushFaces {
		return nil, encodeRefusal(Quake3ImportTooLarge, "", "entity %d brush %d (line %d) declares %d faces; a Quake III import reads brushes of up to %d",
			entityIndex, brushIndex, source.Line, len(source.Faces), MaxQuake3BrushFaces)
	}
	id, derived, err := q.identity(KindBrush, path)
	if err != nil {
		return nil, err
	}
	faces := make([]Value, 0, len(source.Faces))
	checked := make([]legality.Face, 0, len(source.Faces))
	for index, one := range source.Faces {
		face, values, err := q.face(one, fmt.Sprintf("%s.f%d", path, index))
		if err != nil {
			return nil, err
		}
		faces = append(faces, face)
		checked = append(checked, values)
	}
	result, err := legality.Check(q.ctx, checked)
	if err != nil {
		return nil, err
	}
	kept := make([]int, len(source.Faces))
	for index := range kept {
		kept[index] = index
	}
	// Planes convexity names as carrying no facet are tried WITHOUT — once, because a plane with
	// fewer than three of the solid's corners on it contains the whole solid in its half-space. What
	// is left is judged from scratch by the same contract; only a brush that then passes is imported,
	// and it is imported as what passed.
	if result.Convexity.Reason == legality.ReasonNotConvex && len(result.Convexity.Faces) > 0 {
		idle := map[int]bool{}
		for _, index := range result.Convexity.Faces {
			idle[index] = true
		}
		candidateFaces := []Value{}
		candidateChecked := []legality.Face{}
		candidateKept := []int{}
		for index := range source.Faces {
			if !idle[index] {
				candidateFaces = append(candidateFaces, faces[index])
				candidateChecked = append(candidateChecked, checked[index])
				candidateKept = append(candidateKept, index)
			}
		}
		without, err := legality.Check(q.ctx, candidateChecked)
		if err != nil {
			return nil, err
		}
		refusedWithout := false
		for _, code := range without.Codes {
			if Quake3ImportRefusedCodes[code] {
				refusedWithout = true
			}
		}
		if !refusedWithout {
			faces, checked, kept, result = candidateFaces, candidateChecked, candidateKept, without
			left := make([]int, 0, len(idle))
			for index := range idle {
				left = append(left, index)
			}
			sort.Ints(left)
			q.summary.RedundantPlanes += len(idle)
			q.summary.RedundantPlaneBrushes = append(q.summary.RedundantPlaneBrushes,
				Quake3RedundantPlaneBrush{Entity: entityIndex, Brush: brushIndex, Line: source.Line, Faces: left})
		}
	}
	for position, index := range kept {
		q.countShader(source.Faces[index].Texture, checked[position].Texture)
	}
	refused := []string{}
	has := map[string]bool{}
	for _, code := range result.Codes {
		if Quake3ImportRefusedCodes[code] && !has[code] {
			has[code] = true
			refused = append(refused, code)
		}
	}
	// SEM-Q-3 is a document rule as well as a legality one: a degenerate matrix is refused even when
	// the solid itself is fine, because the document it would produce is invalid.
	if source.Syntax == quake3map.SyntaxBrushPrimitives && !has[legality.CodeTextureBasisDegenerate] {
		for _, face := range checked {
			if face.Projection.Mode == legality.ModeBrushPrimitives && determinantIsZero(face.Projection.Matrix) {
				refused = append(refused, legality.CodeTextureBasisDegenerate)
				break
			}
		}
	}
	if len(refused) > 0 {
		q.rejected = append(q.rejected, Quake3RejectedBrush{Entity: entityIndex, Brush: brushIndex, Line: source.Line, Codes: refused})
	}
	for _, finding := range result.Findings {
		if !Quake3ImportRefusedCodes[finding.Code] {
			q.summary.Risks[finding.Code]++
		}
	}
	q.summary.Brushes++
	q.summary.Faces += len(faces)
	switch source.Syntax {
	case quake3map.SyntaxClassic:
		q.summary.ClassicBrushes++
	case quake3map.SyntaxValve220:
		q.summary.Valve220Brushes++
	default:
		q.summary.BrushPrimitiveBrushes++
	}
	item := NewObject()
	item.Set("kind", "brush")
	item.Set("brush_id", id)
	item.Set("derived_from", derived)
	item.Set("faces", faces)
	return item, nil
}

func (q *quake3Importer) patch(source *quake3map.Patch, entityIndex, patchIndex int) (*Object, error) {
	path := fmt.Sprintf("e%d.p%d", entityIndex, patchIndex)
	id, derived, err := q.identity(KindPatch, path)
	if err != nil {
		return nil, err
	}
	columns := make([]Value, 0, len(source.ControlPoints))
	for _, column := range source.ControlPoints {
		rows := make([]Value, 0, len(column))
		for _, point := range column {
			values := make([]Value, 0, 5)
			for _, value := range point {
				if math.Abs(value) > maxQuake3PatchCoordinate {
					return nil, encodeRefusal(Quake3EncodePatchRange, "SCH-Q-3",
						"%s: a control point component is outside ±%d", path, maxQuake3PatchCoordinate)
				}
				spelled, err := exact(value)
				if err != nil {
					return nil, err
				}
				values = append(values, spelled)
			}
			rows = append(rows, values)
		}
		columns = append(columns, rows)
	}
	q.summary.Patches++
	texture, err := q.storedShader(source.Texture, path)
	if err != nil {
		return nil, err
	}
	q.countShader(source.Texture, texture)
	tail, err := q.tail(source.Tail, path)
	if err != nil {
		return nil, err
	}
	item := NewObject()
	item.Set("kind", "patch")
	item.Set("patch_id", id)
	item.Set("derived_from", derived)
	item.Set("texture", texture)
	item.Set("width", json.Number(strconv.Itoa(source.Width)))
	item.Set("height", json.Number(strconv.Itoa(source.Height)))
	item.Set("control_points", columns)
	item.Set("tail", tail)
	return item, nil
}

func validMapString(value string) bool { return !strings.ContainsAny(value, "\"\r\n") }

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

// FromQuake3Map is the document for a parsed Quake III map, and what the import did. It returns a
// *Quake3ImportError — never a document built from part of the file — when the reader recorded any
// fault or any brush is impossible, and the context's error when it was stopped.
func FromQuake3Map(parsed *quake3map.Map, opts Quake3ImportOptions) (*Object, *Quake3ImportSummary, error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if len(parsed.Errors) > 0 {
		first := parsed.Errors[0]
		return nil, nil, &Quake3ImportError{Code: Quake3ImportParse, ParseErrors: append([]quake3map.ParseError{}, parsed.Errors...),
			Message: fmt.Sprintf("The Quake III map could not be read: %d %s, the first at line %d, column %d (%s): %s. Nothing was imported.",
				len(parsed.Errors), plural(len(parsed.Errors), "error", "errors"), first.Line, first.Column, first.Code, first.Message)}
	}
	if len(parsed.Entities) == 0 {
		return nil, nil, &Quake3ImportError{Code: Quake3ImportEmpty, Message: "The file holds no entities, so there is nothing to import."}
	}
	sourceMap := BareSourceMapName(opts.SourceMap)
	if !quake3SHA256.MatchString(opts.SourceSHA256) {
		return nil, nil, encodeRefusal(Quake3EncodeSourceSHA256, "SCH-2", "sourceSha256 is not a lowercase hex SHA-256 digest")
	}
	documentID := opts.DocumentID
	if documentID == "" {
		base := sourceMap
		if base == "" {
			base = "map"
		}
		documentID = Quake3DocumentID(base)
	}
	if !quake3DocumentID.MatchString(documentID) {
		return nil, nil, encodeRefusal(Quake3EncodeDocumentID, "SCH-2", "document_id %q is not valid", documentID)
	}
	// WRITE current only, from the contract — never a constant.
	var version string
	if opts.Contract != nil {
		version = opts.Contract.Version
	} else {
		version = CurrentVersion()
	}

	q := &quake3Importer{ctx: ctx, sourceMap: sourceMap, sourceSHA256: opts.SourceSHA256,
		summary:     &Quake3ImportSummary{Risks: map[string]int{}, RedundantPlaneBrushes: []Quake3RedundantPlaneBrush{}},
		shaderNames: map[string]bool{}}
	entities := make([]Value, 0, len(parsed.Entities))
	for index, source := range parsed.Entities {
		path := fmt.Sprintf("e%d", index)
		content := make([]Value, 0, len(source.Content))
		brushes, patches := 0, 0
		for _, item := range source.Content {
			switch typed := item.(type) {
			case quake3map.Property:
				if !validMapString(typed.Key) || !validMapString(typed.Value) {
					return nil, nil, encodeRefusal(Quake3EncodePropertyInvalid, "SCH-5", "%s: a property contains a quote or a line break", path)
				}
				property := NewObject()
				property.Set("kind", "property")
				property.Set("key", typed.Key)
				property.Set("value", typed.Value)
				content = append(content, property)
				q.summary.Properties++
			case *quake3map.Brush:
				if err := ctx.Err(); err != nil {
					return nil, nil, err
				}
				brush, err := q.brush(typed, index, brushes)
				if err != nil {
					return nil, nil, err
				}
				brushes++
				content = append(content, brush)
			case *quake3map.Patch:
				patch, err := q.patch(typed, index, patches)
				if err != nil {
					return nil, nil, err
				}
				patches++
				content = append(content, patch)
			}
		}
		if len(content) == 0 {
			return nil, nil, encodeRefusal(Quake3EncodeEntityEmpty, "SCH-12",
				"%s: an entity with no properties, brushes or patches cannot be represented", path)
		}
		id, derived, err := q.identity(KindEntity, path)
		if err != nil {
			return nil, nil, err
		}
		q.summary.Entities++
		entity := NewObject()
		entity.Set("entity_id", id)
		entity.Set("derived_from", derived)
		entity.Set("content", content)
		entities = append(entities, entity)
	}

	if len(q.rejected) > 0 {
		shown := q.rejected
		if len(shown) > 5 {
			shown = shown[:5]
		}
		parts := make([]string, 0, len(shown))
		for _, one := range shown {
			parts = append(parts, fmt.Sprintf("entity %d brush %d (line %d: %s)", one.Entity, one.Brush, one.Line, strings.Join(one.Codes, ", ")))
		}
		first := strings.Join(parts, "; ")
		more := ""
		if len(q.rejected) > 5 {
			more = "; …"
		}
		matrixOnly := true
		for _, one := range q.rejected {
			if len(one.Codes) != 1 || one.Codes[0] != legality.CodeTextureBasisDegenerate {
				matrixOnly = false
			}
		}
		var message string
		if matrixOnly {
			message = fmt.Sprintf("%d brushDef %s a texture matrix with determinant 0 and the map was not imported: %s%s. "+
				"Q3Map2 compiles such a face with its texture collapsed; an APMap document cannot hold the matrix (SEM-Q-3). "+
				"Fit the texture on those faces in the source editor and import again.",
				len(q.rejected), plural(len(q.rejected), "brush has", "brushes have"), first, more)
		} else {
			message = fmt.Sprintf("%d %s impossible geometry and the map was not imported: %s%s.",
				len(q.rejected), plural(len(q.rejected), "brush is", "brushes are"), first, more)
		}
		return nil, nil, &Quake3ImportError{Code: Quake3ImportBrushIllegal, Message: message, Rejected: q.rejected}
	}
	q.summary.Shaders = len(q.shaderNames)

	frame, err := SourceFrame.ToJSON()
	if err != nil {
		return nil, nil, err
	}
	source := NewObject()
	if sourceMap != "" {
		source.Set("map_name", sourceMap)
	}
	source.Set("map_sha256", opts.SourceSHA256)
	provenance := NewObject()
	provenance.Set("producer", Quake3ImportProducer)
	provenance.Set("producer_version", Quake3ImportProducerVersion)
	provenance.Set("pipeline", []Value{Quake3ImportPipeline})
	provenance.Set("source", source)

	dialect := Quake3MapDialect
	if q.summary.Valve220Brushes > 0 {
		dialect = "valve_220"
	}
	document := NewObject()
	document.Set("$schema", fmt.Sprintf(SchemaURLTemplate, version))
	document.Set("apmap_version", version)
	document.Set("document_id", documentID)
	document.Set("revision", json.Number("0"))
	document.Set("role", RoleFullMap)
	document.Set("game", Quake3MapGame)
	document.Set("map_dialect", dialect)
	document.Set("id_policy", PolicyDerived)
	document.Set("frame", frame)
	document.Set("provenance", provenance)
	document.Set("entities", entities)
	document.Set("groups", []Value{})
	return document, q.summary, nil
}
