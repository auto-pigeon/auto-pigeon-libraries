package mapio

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/convex"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

// Q2_002A. A Classic face on a 45° base-axis tie keeps its texels through q2map-to-apmap, as AUP's encoder keeps
// them. The rooms under testdata/q2-classic-tie are AUT's authored controls (auto-pigeon-tools
// scripts/q2corpus/tie_fixtures.py), copied byte for byte from AUP's packages/quake-geometry/testdata: every
// `aut_q2/checker` face sits on a tie that six-place rounding of its points flips.
//
// The witness below is written here, from qbsp's own arithmetic (common/mapfile.cc parse_brush_face, mapfile.hh
// texture_axis_t, set_texinfo), and shares no code with apmap/classicfidelity.go.

type oracleFace struct {
	points [3][3]float64
	shift  [2]float64
	rot    float64
	scale  [2]float64
	valve  *[2][4]float64
}

func oracleUV(face oracleFace, p [3]float64) [2]float64 {
	if face.valve != nil {
		s, t := face.valve[0], face.valve[1]
		return [2]float64{(s[0]*p[0]+s[1]*p[1]+s[2]*p[2])/face.scale[0] + s[3], (t[0]*p[0]+t[1]*p[1]+t[2]*p[2])/face.scale[1] + t[3]}
	}
	a, b, c := face.points[0], face.points[1], face.points[2]
	ab := [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
	cb := [3]float64{c[0] - b[0], c[1] - b[1], c[2] - b[2]}
	n := [3]float64{float64(ab[1]*cb[2]) - float64(ab[2]*cb[1]), float64(ab[2]*cb[0]) - float64(ab[0]*cb[2]), float64(ab[0]*cb[1]) - float64(ab[1]*cb[0])}
	l := math.Sqrt(float64(n[0]*n[0]) + float64(n[1]*n[1]) + float64(n[2]*n[2]))
	n = [3]float64{n[0] / l, n[1] / l, n[2] / l}
	base := [6][3][3]float64{
		{{0, 0, 1}, {1, 0, 0}, {0, -1, 0}}, {{0, 0, -1}, {1, 0, 0}, {0, -1, 0}},
		{{1, 0, 0}, {0, 1, 0}, {0, 0, -1}}, {{-1, 0, 0}, {0, 1, 0}, {0, 0, -1}},
		{{0, 1, 0}, {1, 0, 0}, {0, 0, -1}}, {{0, -1, 0}, {1, 0, 0}, {0, 0, -1}},
	}
	best, axis := 0.0, 0
	for i, row := range base {
		if d := n[0]*row[0][0] + n[1]*row[0][1] + n[2]*row[0][2]; d > best {
			best, axis = d, i
		}
	}
	vectors := [2][3]float64{base[axis][1], base[axis][2]}
	sv, tv := 2, 2
	if vectors[0][0] != 0 {
		sv = 0
	} else if vectors[0][1] != 0 {
		sv = 1
	}
	if vectors[1][0] != 0 {
		tv = 0
	} else if vectors[1][1] != 0 {
		tv = 1
	}
	angle := face.rot / 180 * math.Pi
	sinv, cosv := math.Sin(angle), math.Cos(angle)
	for i := range vectors {
		ns, nt := cosv*vectors[i][sv]-sinv*vectors[i][tv], sinv*vectors[i][sv]+cosv*vectors[i][tv]
		vectors[i][sv], vectors[i][tv] = ns, nt
	}
	var uv [2]float64
	for i := range vectors {
		scale := face.scale[i]
		if scale == 0 {
			scale = 1
		}
		uv[i] = (vectors[i][0]*p[0]+vectors[i][1]*p[1]+vectors[i][2]*p[2])/scale + face.shift[i]
	}
	return uv
}

// sourceFaces reads a `.map` with the production parser: only its numbers are used.
func sourceFaces(t *testing.T, text string) [][]oracleFace {
	t.Helper()
	parsed, err := mapfile.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]oracleFace
	for _, entity := range parsed.Entities() {
		for _, item := range entity.Items() {
			brush, ok := item.(mapfile.Brush)
			if !ok {
				continue
			}
			var faces []oracleFace
			for _, face := range brush.Faces() {
				f := oracleFace{shift: [2]float64{face.XOffset(), face.YOffset()}, rot: face.Rotation(), scale: [2]float64{face.XScale(), face.YScale()}}
				for i, point := range face.Points() {
					f.points[i] = [3]float64{point.X(), point.Y(), point.Z()}
				}
				if u, ok := face.UAxis(); ok {
					v, _ := face.VAxis()
					f.valve = &[2][4]float64{{u.X(), u.Y(), u.Z(), u.Offset()}, {v.X(), v.Y(), v.Z(), v.Offset()}}
				}
				faces = append(faces, f)
			}
			out = append(out, faces)
		}
	}
	return out
}

type storedDocument struct {
	MapDialect string `json:"map_dialect"`
	Entities   []struct {
		Content []struct {
			Kind  string `json:"kind"`
			Faces []struct {
				Plane struct {
					Points [3][3]float64 `json:"points"`
				} `json:"plane"`
				Texture    string `json:"texture"`
				Projection struct {
					Mode     string     `json:"mode"`
					Shift    [2]float64 `json:"shift"`
					Rotation float64    `json:"rotation"`
					Scale    [2]float64 `json:"scale"`
					UAxis    [4]float64 `json:"u_axis"`
					VAxis    [4]float64 `json:"v_axis"`
				} `json:"projection"`
			} `json:"faces"`
		} `json:"content"`
	} `json:"entities"`
}

func storedFaces(t *testing.T, apmapText string) (string, [][]oracleFace) {
	t.Helper()
	var document storedDocument
	if err := json.Unmarshal([]byte(apmapText), &document); err != nil {
		t.Fatal(err)
	}
	var out [][]oracleFace
	for _, entity := range document.Entities {
		for _, item := range entity.Content {
			if item.Kind != "brush" {
				continue
			}
			var faces []oracleFace
			for _, face := range item.Faces {
				p := face.Projection
				f := oracleFace{points: face.Plane.Points, shift: p.Shift, rot: p.Rotation, scale: p.Scale}
				if p.Mode == "valve220" {
					f.valve = &[2][4]float64{p.UAxis, p.VAxis}
				}
				faces = append(faces, f)
			}
			out = append(out, faces)
		}
	}
	return document.MapDialect, out
}

// worstError compares every checker face of `got` with the same face of `want` at the corners and centroid of
// the face `got` describes.
func worstError(t *testing.T, sourceText string, want, got [][]oracleFace) (float64, int) {
	t.Helper()
	worst, counted := 0.0, 0
	textures := checkerFaces(t, sourceText)
	for b := range got {
		planes := make([]convex.Plane, len(got[b]))
		for f, face := range got[b] {
			plane, err := convex.PlaneFromPoints(convex.Vec3f(face.points[0]), convex.Vec3f(face.points[1]), convex.Vec3f(face.points[2]))
			if err != nil {
				t.Fatal(err)
			}
			planes[f] = plane
		}
		for f := range got[b] {
			if !textures[[2]int{b, f}] {
				continue
			}
			winding := convex.BaseWinding(planes[f], convex.BaseWindingExtent)
			for g := range planes {
				if g != f {
					winding = convex.ClipWinding(winding, planes[g], convex.PlaneDistanceEpsilon)
				}
			}
			if len(winding) < 3 {
				t.Fatalf("b%d.f%d bounds nothing", b, f)
			}
			var centre [3]float64
			samples := make([][3]float64, 0, len(winding)+1)
			for _, point := range winding {
				samples = append(samples, [3]float64(point))
				for k := range centre {
					centre[k] += point[k] / float64(len(winding))
				}
			}
			samples = append(samples, centre)
			for _, point := range samples {
				a, c := oracleUV(want[b][f], point), oracleUV(got[b][f], point)
				worst = math.Max(worst, math.Max(math.Abs(a[0]-c[0]), math.Abs(a[1]-c[1])))
			}
			counted++
		}
	}
	return worst, counted
}

func checkerFaces(t *testing.T, text string) map[[2]int]bool {
	t.Helper()
	parsed, err := mapfile.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	out := map[[2]int]bool{}
	b := 0
	for _, entity := range parsed.Entities() {
		for _, item := range entity.Items() {
			brush, ok := item.(mapfile.Brush)
			if !ok {
				continue
			}
			for f, face := range brush.Faces() {
				if face.Texture() == "aut_q2/checker" {
					out[[2]int{b, f}] = true
				}
			}
			b++
		}
	}
	return out
}

func readTie(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "q2-classic-tie", name+".map"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func convertWithWarnings(t *testing.T, direction, name, text string) (string, []string) {
	t.Helper()
	resolved, _ := LookupDirection(direction)
	output, result, err := Convert(context.Background(), resolved, name, []byte(text), Options{})
	if err != nil {
		t.Fatalf("%s %s: %v", direction, name, err)
	}
	return string(output), warningTexts(result.Warnings)
}

func warningTexts(warnings []Warning) []string {
	texts := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		texts = append(texts, warning.Text)
	}
	return texts
}

func TestQuake2ClassicTieFacesKeepTheirTexels(t *testing.T) {
	for _, room := range []string{"tie-o0", "tie-n4096"} {
		source := readTie(t, room)
		document, warnings := convertWithWarnings(t, "q2map-to-apmap", room+".map", source)
		dialect, got := storedFaces(t, document)
		if dialect != "quake_standard" {
			t.Fatalf("%s: dialect %q", room, dialect)
		}
		worst, counted := worstError(t, source, sourceFaces(t, source), got)
		if counted != 6 || worst > 0.01 {
			t.Fatalf("%s: %d tie faces, worst %.6f texels; want 6 within 0.01", room, counted, worst)
		}
		if !strings.Contains(strings.Join(warnings, "\n"), "classic_uv_rebased: 6 face(s)") {
			t.Fatalf("%s: warnings %q", room, warnings)
		}
		// Export and read back: nothing is rebased twice and the document is the same.
		mapText := convertText(t, "apmap-to-q2map", room+".apmap", document)
		again, warnings := convertWithWarnings(t, "q2map-to-apmap", room+".map", mapText)
		if len(warnings) != 0 {
			t.Fatalf("%s: second import warned %q", room, warnings)
		}
		_, gotAgain := storedFaces(t, again)
		if worst, _ := worstError(t, source, sourceFaces(t, source), gotAgain); worst > 0.01 {
			t.Fatalf("%s: after a round trip, worst %.6f texels", room, worst)
		}
	}
}

func TestQuake2ClassicShearIsNamedNotFaked(t *testing.T) {
	source := readTie(t, "tie-shear")
	document, warnings := convertWithWarnings(t, "q2map-to-apmap", "tie-shear.map", source)
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "classic_uv_unrepresentable: 2 face(s)") || strings.Contains(joined, "classic_uv_rebased") {
		t.Fatalf("warnings %q", warnings)
	}
	if worst, counted := worstError(t, source, sourceFaces(t, source), func() [][]oracleFace { _, f := storedFaces(t, document); return f }()); counted != 2 || worst < 1 {
		t.Fatalf("the shear faces should still be named as moved: %d faces, worst %.3f", counted, worst)
	}
}

func TestQuake2Valve220TieRoomIsUntouched(t *testing.T) {
	source := readTie(t, "tie-valve220")
	document, warnings := convertWithWarnings(t, "q2map-to-apmap", "tie-valve220.map", source)
	dialect, got := storedFaces(t, document)
	if dialect != "valve_220" || len(warnings) != 0 {
		t.Fatalf("dialect %q, warnings %q", dialect, warnings)
	}
	if worst, counted := worstError(t, source, sourceFaces(t, source), got); counted != 6 || worst > 0.01 {
		t.Fatalf("%d faces, worst %.6f", counted, worst)
	}
}

// A sign tie between the two spellings of a re-expressed face goes to the rotation nearer the source's — the same
// choice AUP's encoder makes, so the two producers write the same bytes (Q2_002A).
func TestQuake2ClassicSignTieGoesToTheNearerRotation(t *testing.T) {
	source := readTie(t, "tie-o0")
	at := strings.Index(source, "aut_q2/checker ")
	end := at + len("aut_q2/checker ")
	fields := strings.Fields(source[end : end+80])
	source = source[:end] + "0 0 0 -1 1" + source[end+len(strings.Join(fields[:5], " ")):]
	document, _ := convertWithWarnings(t, "q2map-to-apmap", "tie-o0.map", source)
	var parsed storedDocument
	if err := json.Unmarshal([]byte(document), &parsed); err != nil {
		t.Fatal(err)
	}
	brushes := 0
	for _, item := range parsed.Entities[0].Content {
		if item.Kind != "brush" {
			continue
		}
		if brushes == 6 {
			p := item.Faces[0].Projection
			if p.Rotation != 0 || p.Scale != [2]float64{1, 1} {
				t.Fatalf("b6.f0 re-expressed as rotation %v scale %v; want 0 and (1, 1)", p.Rotation, p.Scale)
			}
			return
		}
		brushes++
	}
	t.Fatal("no b6")
}
