package apmap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// APMap (game "quake3") → Quake III `.map`, the text Q3Map2 compiles — Q3_004.
//
// # Conversion only
//
// This is a FORMAT conversion. It is no Quake III extractor: the game profiles, the
// extraction families, the texture knowledge and the worker stay Quake 1 (gameprofile.Load refuses
// "quake3" by name), and nothing here reads a game profile.
//
// # One serializer, two implementations, byte parity
//
// The editor (AUP, `packages/quake-geometry/src/map/writer.ts`, `mapTextFromApmap` with its default
// options — the ones its Export map button uses) is the reference this writer matches BYTE FOR BYTE
// for every document both accept: the same `// entity N` / `// brush N` / `// patch N` markers, the
// same two-space indent, the same one-syntax-per-file rule, the same `textures/` stripping and the
// same two number spellings (formatQuake3MapNumber / formatQuake3ExactNumber below). A shared golden
// corpus under testdata/q3map, produced by AUP's writer and pinned by digest, proves it; the
// cross-language check is scripts/q3map-parity.sh. A Go copy of a TypeScript writer can drift, and
// the golden bytes are what fail the build the day it does.
//
// This writer is STRICTER than the editor's in one direction only — it refuses what the editor
// would write but Q3Map2 or the engine would misread (an unsafe shader path, a 1- or 2-number face
// tail, a shader name the engine truncates) — and it never writes something the editor would
// refuse. So for any document both accept the bytes are identical, and every difference is a
// refusal, never a different map.
//
// # Refusal is whole
//
// A bad patch, an unsupported projection, a mixed-syntax file, a document for another game — each
// refuses the WHOLE output with a *Quake3MapError naming the object's own id and path. There is
// never a truncated map: the text is built in memory and returned only when every object wrote.
//
// # What travels beside the map
//
// A Quake3MapManifest: the source's identity, the brush syntax, counts, every shader (as the full
// logical name Q3Map2 resolves, `textures/…`), every model and sound an entity key names, and every
// warning. Nothing is substituted: a bare Quake 1 tool name (`clip`, `skip`) stays what it is and is
// warned about, because `common/clip` and Quake 1's `clip` are different tools.

// Quake3MapGame is the only game this conversion reads.
const Quake3MapGame = "quake3"

// Quake3MapDialects are the map_dialect values a quake3 document may carry. `quake3_extended` is
// the registry's Quake III grammar; `valve_220` is what AUP's Quake III importer stores for a map
// written in Valve 220 syntax, which Q3Map2 also compiles (Q3_002, measured on the private corpus).
var Quake3MapDialects = []string{"quake3_extended", "valve_220"}

// quake3MinimumVersion is the first APMap version that gives "quake3" a meaning (SEMANTICS §2):
// before 1.5 the name is reserved, and a consumer must refuse rather than guess.
const quake3MinimumVersion = "1.5"

// Quake3MapManifestFormat names the manifest's own contract.
const Quake3MapManifestFormat = "auto-pigeon.q3map-conversion-manifest"

// Quake3MapManifestVersion is the manifest contract's version. Bumped only by a change a reader
// would misinterpret.
const Quake3MapManifestVersion = 1

// quake3MaxQPath is the engine's MAX_QPATH: a shader name, including `textures/` and the NUL, is
// held in 64 bytes. A longer name is silently truncated by the BSP (dshader_t) and resolves to a
// different shader, so it is refused.
const quake3MaxQPath = 64

// quake3ShaderPrefix is the directory Q3Map2 prefixes to every shader name it reads from a `.map`.
const quake3ShaderPrefix = "textures/"

// quake3MaxPatchDimension / quake3MaxPatchComponent are the APMap 1.5 patch bounds (SEMANTICS §9c),
// which are Q3Map2's: MAX_PATCH_SIZE 32 with odd sizes, MAX_WORLD_COORD 131072.
const (
	quake3MinPatchDimension = 3
	quake3MaxPatchDimension = 31
	quake3MaxPatchComponent = 131072
)

// quake1ToolTextures are the bare Quake 1 compiler tool names. In a Quake III map each is an
// ordinary shader Q3Map2 looks for under textures/ and draws — not a tool — so they are warned
// about by name, never renamed (AUP `quake3Names.ts` QUAKE1_TOOL_TEXTURES, the same set).
var quake1ToolTextures = map[string]bool{
	"skip": true, "clip": true, "trigger": true, "hint": true, "hintskip": true,
	"origin": true, "null": true, "aaatrigger": true,
}

// quake3CancelStride is how many content items are written between two cancellation checks.
const quake3CancelStride = 256

// Quake3MapError is the whole-output refusal of a conversion. Code is stable and machine-readable;
// Path is the object's location (entities[e].content[c].faces[f]); ObjectID is its own APMap id
// when it has one, so a diagnostic points at the object a person can find in the editor.
type Quake3MapError struct {
	Code     string
	Path     string
	ObjectID string
	Message  string
}

func (e *Quake3MapError) Error() string {
	where := ""
	switch {
	case e.Path != "" && e.ObjectID != "":
		where = fmt.Sprintf("%s (%s): ", e.ObjectID, e.Path)
	case e.Path != "":
		where = e.Path + ": "
	case e.ObjectID != "":
		where = e.ObjectID + ": "
	}
	return fmt.Sprintf("apmap-to-q3map refused [%s]: %s%s; nothing was written", e.Code, where, e.Message)
}

func (e *Quake3MapError) Unwrap() error { return ErrAPMap }

// Stable refusal codes.
const (
	Q3MapCodeMalformed          = "q3map_document_malformed"
	Q3MapCodeGameUnsupported    = "q3map_game_unsupported"
	Q3MapCodeDialectUnsupported = "q3map_dialect_unsupported"
	Q3MapCodeVersionUnsupported = "q3map_version_unsupported"
	Q3MapCodeMixedBrush         = "q3map_mixed_brush_syntax"
	Q3MapCodeMixedSyntax        = "q3map_mixed_file_syntax"
	Q3MapCodeContentInvalid     = "q3map_content_invalid"
	Q3MapCodePropertyInvalid    = "q3map_property_invalid"
	Q3MapCodeBrushInvalid       = "q3map_brush_invalid"
	Q3MapCodeFaceInvalid        = "q3map_face_invalid"
	Q3MapCodeProjection         = "q3map_projection_unsupported"
	Q3MapCodeShaderUnsafe       = "q3map_shader_unsafe"
	Q3MapCodeShaderMissing      = "q3map_shader_missing"
	Q3MapCodeNumber             = "q3map_number_unwritable"
	Q3MapCodeTail               = "q3map_face_tail_invalid"
	Q3MapCodePatchInvalid       = "q3map_patch_invalid"
	Q3MapCodeCancelled          = "q3map_cancelled"
)

// Quake3MapWarning is one non-fatal observation, with the object it is about.
type Quake3MapWarning struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	ObjectID string `json:"object_id,omitempty"`
	Message  string `json:"message"`
}

// String is the one-line form the CLI and Result.Warnings carry.
func (w Quake3MapWarning) String() string {
	where := w.Path
	if w.ObjectID != "" {
		where = w.ObjectID + " (" + w.Path + ")"
	}
	return fmt.Sprintf("%s %s: %s", w.Code, where, w.Message)
}

// Quake3MapShader is one shader the map names, as Q3Map2 resolves it.
type Quake3MapShader struct {
	// Name is the full logical name (`textures/base_wall/concrete`).
	Name string `json:"name"`
	// Written is the token in the `.map` (`base_wall/concrete`).
	Written string `json:"written"`
	Faces   int    `json:"faces"`
	Patches int    `json:"patches"`
}

// Quake3MapAsset is a model or sound path an entity key names.
type Quake3MapAsset struct {
	Name string `json:"name"`
	// Keys are the entity keys that name it (`model`, `model2`, `noise`, `music`), sorted.
	Keys []string `json:"keys"`
	// Entities is how many entities name it.
	Entities int `json:"entities"`
}

// Quake3MapCounts are the document's object counts, as written.
type Quake3MapCounts struct {
	Entities   int `json:"entities"`
	Properties int `json:"properties"`
	Brushes    int `json:"brushes"`
	Faces      int `json:"faces"`
	Patches    int `json:"patches"`
	// PatchControlPoints is the sum of width×height over every patch.
	PatchControlPoints int `json:"patch_control_points"`
}

// Quake3MapDocumentIdentity is what the source APMap says about itself.
type Quake3MapDocumentIdentity struct {
	DocumentID   string `json:"document_id,omitempty"`
	APMapVersion string `json:"apmap_version"`
	Game         string `json:"game"`
	MapDialect   string `json:"map_dialect"`
	Role         string `json:"role,omitempty"`
}

// Quake3MapFile is a file's identity: name, digest, size.
type Quake3MapFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

// Quake3MapManifest travels beside the `.map`. It is deterministic — no time, no host, no path
// beyond the file names — so two conversions of one document produce identical manifests.
type Quake3MapManifest struct {
	Format        string                    `json:"format"`
	FormatVersion int                       `json:"format_version"`
	Direction     string                    `json:"direction"`
	Source        *Quake3MapFile            `json:"source,omitempty"`
	Output        *Quake3MapFile            `json:"output,omitempty"`
	Document      Quake3MapDocumentIdentity `json:"document"`
	// BrushSyntax is "classic", "valve220", "brush_primitives", or "none" for a map with no brush.
	BrushSyntax string             `json:"brush_syntax"`
	Counts      Quake3MapCounts    `json:"counts"`
	Shaders     []Quake3MapShader  `json:"shaders"`
	Models      []Quake3MapAsset   `json:"models"`
	Sounds      []Quake3MapAsset   `json:"sounds"`
	Warnings    []Quake3MapWarning `json:"warnings"`
}

// Quake3MapOptions configures a conversion.
type Quake3MapOptions struct {
	// Context cancels a conversion part-way. A cancelled conversion returns a Quake3MapError with
	// Q3MapCodeCancelled and no text. Nil means context.Background().
	Context context.Context
	// Contract is the contract whose readable versions gate the document. Nil means the process's
	// default.
	Contract *Contract
}

// ExportQuake3MapText converts a quake3 APMap document into Q3Map2 `.map` text and its manifest.
//
// The source value is only read. On any refusal the text is "" and the manifest nil.
func ExportQuake3MapText(document Value, options Quake3MapOptions) (string, *Quake3MapManifest, error) {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", nil, q3Refuse(Q3MapCodeCancelled, "", "", "the conversion was cancelled (%v)", err)
	}
	root, ok := document.(*Object)
	if !ok {
		return "", nil, q3Refuse(Q3MapCodeMalformed, "", "", "an APMap document must be a JSON object")
	}
	identity, err := quake3Identity(options.Contract, root)
	if err != nil {
		return "", nil, err
	}
	entities, present := root.Get("entities")
	entityList, isList := entities.([]Value)
	if !present || !isList {
		return "", nil, q3Refuse(Q3MapCodeMalformed, "entities", "", "the document has no entities array")
	}
	syntax, err := quake3Preflight(entityList)
	if err != nil {
		return "", nil, err
	}

	writer := &quake3Writer{
		ctx:      ctx,
		shaders:  map[string]*Quake3MapShader{},
		models:   map[string]*quake3AssetUse{},
		sounds:   map[string]*quake3AssetUse{},
		manifest: &Quake3MapManifest{Format: Quake3MapManifestFormat, FormatVersion: Quake3MapManifestVersion, Direction: "apmap-to-q3map", Document: identity, BrushSyntax: syntax},
	}
	for index, entry := range entityList {
		if err := writer.entity(entry, index); err != nil {
			return "", nil, err
		}
	}
	if len(entityList) > 0 {
		if first, ok := entityList[0].(*Object); ok && entityClassname(first) != "worldspawn" {
			writer.warn(Quake3MapWarning{Code: "q3map_first_entity_not_worldspawn", Path: "entities[0]",
				Message: fmt.Sprintf("the first entity is %q; Q3Map2 treats the first entity as the world", entityClassname(first))})
		}
	}
	writer.finish()
	return strings.Join(writer.lines, "\n") + "\n", writer.manifest, nil
}

// CheckQuake3MapIdentity is the game / dialect / version gate on its own, so a caller can refuse a
// document for another game before validating it.
func CheckQuake3MapIdentity(document Value) error {
	return CheckQuake3MapIdentityFor(nil, document)
}

// CheckQuake3MapIdentityFor is CheckQuake3MapIdentity against an explicit contract.
func CheckQuake3MapIdentityFor(contract *Contract, document Value) error {
	root, ok := document.(*Object)
	if !ok {
		return q3Refuse(Q3MapCodeMalformed, "", "", "an APMap document must be a JSON object")
	}
	_, err := quake3Identity(contract, root)
	return err
}

func q3Refuse(code, path, id, format string, args ...any) *Quake3MapError {
	return &Quake3MapError{Code: code, Path: path, ObjectID: id, Message: fmt.Sprintf(format, args...)}
}

// quake3Identity is the game / dialect / version gate: a document for another game, another
// grammar, or a version that does not give "quake3" its meaning (or that this build cannot read)
// is refused before a byte is written.
func quake3Identity(contract *Contract, root *Object) (Quake3MapDocumentIdentity, error) {
	identity := Quake3MapDocumentIdentity{}
	identity.DocumentID, _ = stringMember(root, "document_id")
	identity.Role, _ = stringMember(root, "role")
	game, _ := root.Get("game")
	gameName, _ := game.(string)
	identity.Game = gameName
	if gameName != Quake3MapGame {
		return identity, q3Refuse(Q3MapCodeGameUnsupported, "game", identity.DocumentID,
			"unsupported game %s: apmap-to-q3map converts only documents whose game is \"quake3\"%s",
			render(game), gameHint(gameName))
	}
	dialect, _ := stringMember(root, "map_dialect")
	identity.MapDialect = dialect
	if !containsString(Quake3MapDialects, dialect) {
		return identity, q3Refuse(Q3MapCodeDialectUnsupported, "map_dialect", identity.DocumentID,
			"map_dialect %s is not a Quake III grammar this conversion writes (%s)",
			strconv.Quote(dialect), strings.Join(Quake3MapDialects, ", "))
	}
	version, _ := stringMember(root, "apmap_version")
	identity.APMapVersion = version
	if !versionAtLeast(version, quake3MinimumVersion) {
		return identity, q3Refuse(Q3MapCodeVersionUnsupported, "apmap_version", identity.DocumentID,
			"APMap %s gives \"quake3\" no meaning; Quake III documents are APMap %s or later",
			strconv.Quote(version), quake3MinimumVersion)
	}
	if contract == nil {
		if !IsReadableVersion(version) {
			return identity, q3Refuse(Q3MapCodeVersionUnsupported, "apmap_version", identity.DocumentID,
				"%s", UnsupportedVersionMessage(version))
		}
	} else if !contract.IsReadable(version) {
		return identity, q3Refuse(Q3MapCodeVersionUnsupported, "apmap_version", identity.DocumentID,
			"%s", contract.UnsupportedVersionMessage(version))
	}
	return identity, nil
}

func gameHint(game string) string {
	switch game {
	case "quake1":
		return "; a Quake 1 document converts with apmap-to-q1map"
	case "quake2":
		return "; a Quake 2 document converts with apmap-to-q2map"
	}
	return ""
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// versionAtLeast compares two "major.minor" versions numerically. A malformed version is never
// at least anything.
func versionAtLeast(version, minimum string) bool {
	parse := func(text string) (int, int, bool) {
		major, minor, found := strings.Cut(text, ".")
		if !found {
			return 0, 0, false
		}
		a, errA := strconv.Atoi(major)
		b, errB := strconv.Atoi(minor)
		return a, b, errA == nil && errB == nil && a >= 0 && b >= 0
	}
	a, b, ok := parse(version)
	c, d, okMin := parse(minimum)
	if !ok || !okMin {
		return false
	}
	return a > c || (a == c && b >= d)
}

// faceMode is a face's projection mode as the one-syntax rule counts it: anything that is not
// valve220 or brush_primitives counts as classic (and faceLine then refuses a mode it cannot write).
func faceMode(face Value) string {
	object, ok := face.(*Object)
	if !ok {
		return "classic"
	}
	projection, _ := optionalMember(object, "projection").(*Object)
	if projection == nil {
		return "classic"
	}
	mode, _ := stringMember(projection, "mode")
	if mode == "valve220" || mode == "brush_primitives" {
		return mode
	}
	return "classic"
}

// quake3Preflight is the one-syntax-per-file rule (Q3_002, measured: Q3Map2 fixes classic, Valve
// 220 or brushDef from the first brush and refuses a later brush in another). A brush that mixes
// syntaxes inside itself is SEM-Q-5. Both refuse the whole output. It returns the file's syntax.
func quake3Preflight(entities []Value) (string, error) {
	counts := map[string]int{}
	for entityIndex, entry := range entities {
		entity, ok := entry.(*Object)
		if !ok {
			continue
		}
		for position, raw := range arrayMember(entity, "content") {
			item, ok := raw.(*Object)
			if !ok {
				continue
			}
			if kind, _ := stringMember(item, "kind"); kind != "brush" {
				continue
			}
			faces := arrayMember(item, "faces")
			modes := []string{}
			for _, face := range faces {
				if mode := faceMode(face); !containsString(modes, mode) {
					modes = append(modes, mode)
				}
			}
			if len(modes) == 0 {
				continue
			}
			if len(modes) > 1 {
				id, _ := stringMember(item, "brush_id")
				return "", q3Refuse(Q3MapCodeMixedBrush, fmt.Sprintf("entities[%d].content[%d]", entityIndex, position), id,
					"the brush mixes face syntaxes (%s); a brush's faces are all brush_primitives or none (SEM-Q-5)",
					strings.Join(modes, ", "))
			}
			counts[modes[0]]++
		}
	}
	used := []string{}
	for _, syntax := range []string{"classic", "valve220", "brush_primitives"} {
		if counts[syntax] > 0 {
			used = append(used, syntax)
		}
	}
	switch len(used) {
	case 0:
		return "none", nil
	case 1:
		return used[0], nil
	}
	parts := make([]string, 0, len(used))
	for _, syntax := range used {
		label := map[string]string{"classic": "classic", "valve220": "Valve 220", "brush_primitives": "brushDef"}[syntax]
		parts = append(parts, fmt.Sprintf("%d %s", counts[syntax], label))
	}
	return "", q3Refuse(Q3MapCodeMixedSyntax, "", "",
		"the map mixes brush syntaxes (%s); Q3Map2 reads one brush syntax per file, and converting "+
			"between them needs every image's pixel size, which a document does not hold", strings.Join(parts, ", "))
}

type quake3AssetUse struct {
	keys     map[string]bool
	entities int
}

type quake3Writer struct {
	ctx      context.Context
	lines    []string
	items    int
	shaders  map[string]*Quake3MapShader
	models   map[string]*quake3AssetUse
	sounds   map[string]*quake3AssetUse
	manifest *Quake3MapManifest
}

func (w *quake3Writer) warn(warning Quake3MapWarning) {
	w.manifest.Warnings = append(w.manifest.Warnings, warning)
}

func (w *quake3Writer) cancelled() error {
	w.items++
	if w.items%quake3CancelStride != 0 {
		return nil
	}
	if err := w.ctx.Err(); err != nil {
		return q3Refuse(Q3MapCodeCancelled, "", "", "the conversion was cancelled (%v)", err)
	}
	return nil
}

func (w *quake3Writer) entity(entry Value, index int) error {
	path := fmt.Sprintf("entities[%d]", index)
	entity, ok := entry.(*Object)
	if !ok {
		return q3Refuse(Q3MapCodeMalformed, path, "", "an entity must be an object")
	}
	entityID, _ := stringMember(entity, "entity_id")
	contentValue, _ := entity.Get("content")
	content, ok := contentValue.([]Value)
	if !ok {
		return q3Refuse(Q3MapCodeMalformed, path, entityID, "content must be an array")
	}
	w.manifest.Counts.Entities++
	w.lines = append(w.lines, fmt.Sprintf("// entity %d", index), "{")
	brushes, patches := 0, 0
	assetKeys := map[string]map[string]string{} // kind -> key -> value, one entity's references
	for position, raw := range content {
		if err := w.cancelled(); err != nil {
			return err
		}
		itemPath := fmt.Sprintf("%s.content[%d]", path, position)
		item, ok := raw.(*Object)
		if !ok {
			return q3Refuse(Q3MapCodeContentInvalid, itemPath, entityID, "a content item must be an object")
		}
		kind, _ := stringMember(item, "kind")
		switch kind {
		case "brush":
			if err := w.brush(item, itemPath, brushes); err != nil {
				return err
			}
			brushes++
		case "patch":
			if err := w.patch(item, itemPath, patches); err != nil {
				return err
			}
			patches++
		case "property":
			key, value, err := quake3Property(item, itemPath, entityID)
			if err != nil {
				return err
			}
			w.manifest.Counts.Properties++
			w.lines = append(w.lines, fmt.Sprintf("  \"%s\" \"%s\"", key, value))
			noteAssetKey(assetKeys, key, value)
		default:
			return q3Refuse(Q3MapCodeContentInvalid, itemPath, entityID,
				"content item kind %s is not property, brush or patch", render(optionalMember(item, "kind")))
		}
	}
	w.lines = append(w.lines, "}")
	for kind, keys := range assetKeys {
		target := w.models
		if kind == "sound" {
			target = w.sounds
		}
		seen := map[string]bool{}
		for key, value := range keys {
			use := target[value]
			if use == nil {
				use = &quake3AssetUse{keys: map[string]bool{}}
				target[value] = use
			}
			use.keys[key] = true
			if !seen[value] {
				seen[value] = true
				use.entities++
			}
		}
	}
	return nil
}

// noteAssetKey records the entity keys that name a model or a sound. `model` "*N" is an inline
// brush model (the entity's own brushes), not a file.
func noteAssetKey(keys map[string]map[string]string, key, value string) {
	kind := ""
	switch key {
	case "model", "model2":
		if strings.HasPrefix(value, "*") {
			return
		}
		kind = "model"
	case "noise", "music":
		if strings.HasPrefix(value, "*") {
			return
		}
		kind = "sound"
	default:
		return
	}
	if strings.TrimSpace(value) == "" {
		return
	}
	if keys[kind] == nil {
		keys[kind] = map[string]string{}
	}
	keys[kind][key] = value
}

func entityClassname(entity *Object) string {
	for _, raw := range arrayMember(entity, "content") {
		item, ok := raw.(*Object)
		if !ok {
			continue
		}
		if kind, _ := stringMember(item, "kind"); kind != "property" {
			continue
		}
		if key, _ := stringMember(item, "key"); key == "classname" {
			value, _ := stringMember(item, "value")
			return value
		}
	}
	return ""
}

// quake3Property is a key/value pair as a MAP writer can spell it: `"` and a line break end the
// token and the format has no escape for either; NUL ends a C string in Q3Map2.
func quake3Property(item *Object, path, entityID string) (string, string, error) {
	out := [2]string{}
	for index, member := range []string{"key", "value"} {
		value, ok := item.Get(member)
		text, isString := value.(string)
		if !ok || !isString {
			return "", "", q3Refuse(Q3MapCodePropertyInvalid, path, entityID, "property %s must be a string", member)
		}
		if strings.ContainsAny(text, "\"\r\n\x00") {
			return "", "", q3Refuse(Q3MapCodePropertyInvalid, path, entityID,
				"property %s %s holds a quote, a line break or NUL, which a .map cannot carry", member, strconv.Quote(text))
		}
		out[index] = text
	}
	return out[0], out[1], nil
}

func (w *quake3Writer) brush(item *Object, path string, ordinal int) error {
	brushID, _ := stringMember(item, "brush_id")
	facesValue, _ := item.Get("faces")
	faces, ok := facesValue.([]Value)
	if !ok || len(faces) < 4 {
		return q3Refuse(Q3MapCodeBrushInvalid, path, brushID, "a brush needs at least four faces")
	}
	primitives := faceMode(faces[0]) == "brush_primitives"
	w.lines = append(w.lines, fmt.Sprintf("  // brush %d", ordinal), "  {")
	indent := "    "
	if primitives {
		w.lines = append(w.lines, "    brushDef", "    {")
		indent = "      "
	}
	for index, raw := range faces {
		facePath := fmt.Sprintf("%s.faces[%d]", path, index)
		line, err := w.face(raw, facePath, brushID)
		if err != nil {
			return err
		}
		w.lines = append(w.lines, indent+line)
	}
	if primitives {
		w.lines = append(w.lines, "    }")
	}
	w.lines = append(w.lines, "  }")
	w.manifest.Counts.Brushes++
	w.manifest.Counts.Faces += len(faces)
	return nil
}

func (w *quake3Writer) face(raw Value, path, brushID string) (string, error) {
	face, ok := raw.(*Object)
	if !ok {
		return "", q3Refuse(Q3MapCodeFaceInvalid, path, brushID, "a face must be an object")
	}
	faceID, _ := stringMember(face, "face_id")
	id := faceID
	if id == "" {
		id = brushID
	}
	plane, _ := optionalMember(face, "plane").(*Object)
	var pointList []Value
	if plane != nil {
		pointList = arrayMember(plane, "points")
	}
	if len(pointList) != 3 {
		return "", q3Refuse(Q3MapCodeFaceInvalid, path, id, "a face plane needs exactly three points")
	}
	points := make([]string, 0, 3)
	for index, point := range pointList {
		components, ok := point.([]Value)
		if !ok || len(components) != 3 {
			return "", q3Refuse(Q3MapCodeFaceInvalid, path, id, "plane point %d needs exactly three numbers", index)
		}
		spelled := make([]string, 0, 3)
		for _, component := range components {
			text, err := q3Number(component, path, id, false)
			if err != nil {
				return "", err
			}
			spelled = append(spelled, text)
		}
		points = append(points, "( "+strings.Join(spelled, " ")+" )")
	}
	planeText := strings.Join(points, " ")

	textureValue, present := face.Get("texture")
	texture, isString := textureValue.(string)
	if !present || !isString || texture == "" {
		return "", q3Refuse(Q3MapCodeShaderMissing, path, id, "the face names no shader")
	}
	written, err := quake3ShaderToken(texture, path, id)
	if err != nil {
		return "", err
	}

	tail := ""
	if tailValue, ok := face.Get("tail"); ok && tailValue != nil {
		values, isList := tailValue.([]Value)
		if !isList {
			return "", q3Refuse(Q3MapCodeTail, path, id, "a face tail must be an array of numbers")
		}
		// Q3Map2 reads a face's content flags, surface flags and value as three numbers when a token
		// follows; one or two would be read across into the next face.
		if len(values) != 0 && len(values) != 3 {
			return "", q3Refuse(Q3MapCodeTail, path, id,
				"a Quake III face tail is three numbers (content flags, surface flags, value), not %d", len(values))
		}
		spelled := make([]string, 0, len(values))
		for _, value := range values {
			text, err := q3Number(value, path, id, false)
			if err != nil {
				return "", err
			}
			spelled = append(spelled, text)
		}
		if len(spelled) > 0 {
			tail = " " + strings.Join(spelled, " ")
		}
	}

	projection, _ := optionalMember(face, "projection").(*Object)
	if projection == nil {
		return "", q3Refuse(Q3MapCodeProjection, path, id, "the face has no projection")
	}
	mode, _ := stringMember(projection, "mode")
	switch mode {
	case "brush_primitives":
		rows := arrayMember(projection, "matrix")
		if len(rows) != 2 {
			return "", q3Refuse(Q3MapCodeProjection, path, id, "a brush_primitives matrix is two rows of three numbers")
		}
		rowText := make([]string, 0, 2)
		for _, row := range rows {
			coefficients, ok := row.([]Value)
			if !ok || len(coefficients) != 3 {
				return "", q3Refuse(Q3MapCodeProjection, path, id, "a brush_primitives matrix is two rows of three numbers")
			}
			spelled := make([]string, 0, 3)
			for _, coefficient := range coefficients {
				text, err := q3Number(coefficient, path, id, true)
				if err != nil {
					return "", err
				}
				spelled = append(spelled, text)
			}
			rowText = append(rowText, "( "+strings.Join(spelled, " ")+" )")
		}
		if degenerateMatrix(face) {
			return "", q3Refuse(Q3MapCodeProjection, path, id,
				"the brush_primitives texture matrix is degenerate (m00·m11 − m01·m10 = 0, SEM-Q-3)")
		}
		w.noteShader(written, false)
		return fmt.Sprintf("%s ( %s %s ) %s%s", planeText, rowText[0], rowText[1], written, tail), nil
	case "valve220":
		axes := make([]string, 0, 2)
		for _, name := range []string{"u_axis", "v_axis"} {
			values := arrayMember(projection, name)
			if len(values) != 4 {
				return "", q3Refuse(Q3MapCodeProjection, path, id, "a valve220 %s needs four numbers", name)
			}
			spelled, err := q3Numbers(values, path, id)
			if err != nil {
				return "", err
			}
			axes = append(axes, "[ "+strings.Join(spelled, " ")+" ]")
		}
		rotation, scale, err := q3RotationScale(projection, path, id)
		if err != nil {
			return "", err
		}
		w.noteShader(written, false)
		return fmt.Sprintf("%s %s %s %s %s %s%s", planeText, written, axes[0], axes[1], rotation, scale, tail), nil
	case "classic":
		shift := arrayMember(projection, "shift")
		if len(shift) != 2 {
			return "", q3Refuse(Q3MapCodeProjection, path, id, "a classic projection needs a two-number shift")
		}
		shiftText, err := q3Numbers(shift, path, id)
		if err != nil {
			return "", err
		}
		rotation, scale, err := q3RotationScale(projection, path, id)
		if err != nil {
			return "", err
		}
		w.noteShader(written, false)
		return fmt.Sprintf("%s %s %s %s %s%s", planeText, written, strings.Join(shiftText, " "), rotation, scale, tail), nil
	}
	return "", q3Refuse(Q3MapCodeProjection, path, id,
		"projection mode %s is not classic, valve220 or brush_primitives", render(optionalMember(projection, "mode")))
}

func q3RotationScale(projection *Object, path, id string) (string, string, error) {
	rotationValue, present := projection.Get("rotation")
	if !present {
		return "", "", q3Refuse(Q3MapCodeProjection, path, id, "the projection has no rotation")
	}
	rotation, err := q3Number(rotationValue, path, id, false)
	if err != nil {
		return "", "", err
	}
	scale := arrayMember(projection, "scale")
	if len(scale) != 2 {
		return "", "", q3Refuse(Q3MapCodeProjection, path, id, "the projection needs a two-number scale")
	}
	spelled, err := q3Numbers(scale, path, id)
	if err != nil {
		return "", "", err
	}
	return rotation, strings.Join(spelled, " "), nil
}

func q3Numbers(values []Value, path, id string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, err := q3Number(value, path, id, false)
		if err != nil {
			return nil, err
		}
		out = append(out, text)
	}
	return out, nil
}

// q3Number reads a JSON number and spells it for the `.map`: exact (SER-7Q) for a brushDef matrix
// coefficient and a patch component, the map spelling otherwise.
func q3Number(value Value, path, id string, exact bool) (string, error) {
	number, ok := value.(json.Number)
	if !ok {
		return "", q3Refuse(Q3MapCodeNumber, path, id, "expected a number, got %s", render(value))
	}
	parsed, err := strconv.ParseFloat(string(number), 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return "", q3Refuse(Q3MapCodeNumber, path, id, "non-finite number %s (SEM-11)", string(number))
	}
	if math.Abs(parsed) >= 1e21 {
		// JavaScript spells these with an exponent the `.map` lexers do not read.
		return "", q3Refuse(Q3MapCodeNumber, path, id, "number %s is too large to write to a .map", string(number))
	}
	if exact {
		return formatQuake3ExactNumber(parsed), nil
	}
	return formatQuake3MapNumber(parsed), nil
}

// formatQuake3MapNumber is AUP's `formatMapNumber` (writer.ts) for a finite |value| < 1e21: an
// integer is written as one; any other value as the shortest round-trip decimal (JavaScript's
// `String(value)`), except that a magnitude below 1e-6 — which JavaScript would spell with an
// exponent the `.map` grammar has no room for — is its six-decimal rounding.
func formatQuake3MapNumber(value float64) string {
	if value == math.Trunc(value) {
		if value == 0 {
			return "0"
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	if math.Abs(value) >= 1e-6 {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	fixed := strconv.FormatFloat(value, 'f', 6, 64)
	fixed = strings.TrimRight(fixed, "0")
	fixed = strings.TrimSuffix(fixed, ".")
	if fixed == "-0" || fixed == "" || fixed == "0" {
		return "0"
	}
	return fixed
}

// formatQuake3ExactNumber is AUP's `formatExactMapNumber`: the shortest round-trip digits, written
// positionally — so the digits Q3Map2 reads are exactly the ones the document holds (SER-7Q).
func formatQuake3ExactNumber(value float64) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// quake3ShaderToken is the shader name as the `.map` spells it — one leading `textures/` removed
// (Q3Map2 adds it back; AUP `quake3MapShaderName`) — after refusing a name that is not a safe
// relative shader path. Refused: whitespace, control or non-ASCII bytes, `"`, `\`, brackets and
// parentheses (grammar tokens), `:` (a drive or URL), a leading `/`, a `..` segment, `//` or `/*`
// (Q3Map2 comment openers), and a logical name the engine would truncate at MAX_QPATH.
func quake3ShaderToken(texture, path, id string) (string, error) {
	written := texture
	if strings.HasPrefix(strings.ToLower(texture), quake3ShaderPrefix) {
		written = texture[len(quake3ShaderPrefix):]
	}
	refuse := func(reason string) error {
		return q3Refuse(Q3MapCodeShaderUnsafe, path, id, "shader %s is not a safe Quake III shader path: %s", strconv.Quote(texture), reason)
	}
	if written == "" {
		return "", q3Refuse(Q3MapCodeShaderMissing, path, id, "shader %s names only the textures/ directory", strconv.Quote(texture))
	}
	for _, r := range written {
		switch {
		case r <= 0x20 || r >= 0x7f:
			return "", refuse("it holds whitespace, a control character or a non-ASCII character")
		case strings.ContainsRune("\"\\(){}[]:", r):
			return "", refuse(fmt.Sprintf("it holds %q", r))
		}
	}
	if strings.HasPrefix(written, "/") || strings.HasSuffix(written, "/") {
		return "", refuse("it begins or ends with /")
	}
	if strings.Contains(written, "//") || strings.Contains(written, "/*") {
		return "", refuse("it holds a comment opener")
	}
	for _, segment := range strings.Split(written, "/") {
		if segment == ".." || segment == "." {
			return "", refuse("it holds a relative . or .. segment")
		}
	}
	if len(quake3ShaderPrefix)+len(written) >= quake3MaxQPath {
		return "", refuse(fmt.Sprintf("textures/%s is %d bytes; the engine holds %d including the terminator",
			written, len(quake3ShaderPrefix)+len(written), quake3MaxQPath))
	}
	return written, nil
}

func (w *quake3Writer) noteShader(written string, patch bool) {
	name := quake3ShaderPrefix + written
	shader := w.shaders[name]
	if shader == nil {
		shader = &Quake3MapShader{Name: name, Written: written}
		w.shaders[name] = shader
	}
	if patch {
		shader.Patches++
	} else {
		shader.Faces++
	}
}

// patch writes one patchDef2 — the exact layout of AUP's `patchBlock`: one parenthesised group per
// COLUMN, `height` points in each (`control_points[c][r]`, SEMANTICS §9c), every number exact.
func (w *quake3Writer) patch(item *Object, path string, ordinal int) error {
	patchID, _ := stringMember(item, "patch_id")
	width, okWidth := integerValue(optionalMember(item, "width"))
	height, okHeight := integerValue(optionalMember(item, "height"))
	oddInRange := func(value int) bool {
		return value%2 == 1 && value >= quake3MinPatchDimension && value <= quake3MaxPatchDimension
	}
	if !okWidth || !okHeight || !oddInRange(width) || !oddInRange(height) {
		return q3Refuse(Q3MapCodePatchInvalid, path, patchID,
			"%sx%s: each dimension must be odd, %d to %d (Q3Map2: ParsePatch: bad size)",
			render(optionalMember(item, "width")), render(optionalMember(item, "height")), quake3MinPatchDimension, quake3MaxPatchDimension)
	}
	textureValue, present := item.Get("texture")
	texture, isString := textureValue.(string)
	if !present || !isString || texture == "" {
		return q3Refuse(Q3MapCodeShaderMissing, path, patchID, "the patch names no shader")
	}
	written, err := quake3ShaderToken(texture, path, patchID)
	if err != nil {
		return err
	}
	columns := arrayMember(item, "control_points")
	if len(columns) != width {
		return q3Refuse(Q3MapCodePatchInvalid, path, patchID, "the grid has %d column(s), not %d (SEM-Q-2)", len(columns), width)
	}
	grid := make([][][5]float64, width)
	for c, column := range columns {
		points, ok := column.([]Value)
		if !ok || len(points) != height {
			return q3Refuse(Q3MapCodePatchInvalid, path, patchID, "column %d does not hold %d points (SEM-Q-2)", c, height)
		}
		grid[c] = make([][5]float64, height)
		for r, point := range points {
			components, ok := point.([]Value)
			if !ok || len(components) != 5 {
				return q3Refuse(Q3MapCodePatchInvalid, path, patchID, "control point %d,%d is not five numbers [x, y, z, s, t]", c, r)
			}
			for k, component := range components {
				number, ok := component.(json.Number)
				if !ok {
					return q3Refuse(Q3MapCodePatchInvalid, path, patchID, "control point %d,%d holds %s, not a number", c, r, render(component))
				}
				parsed, err := strconv.ParseFloat(string(number), 64)
				if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
					return q3Refuse(Q3MapCodePatchInvalid, path, patchID, "control point %d,%d holds a non-finite number (SEM-11)", c, r)
				}
				if math.Abs(parsed) > quake3MaxPatchComponent {
					return q3Refuse(Q3MapCodePatchInvalid, path, patchID,
						"control point %d,%d holds %s, outside ±%d", c, r, string(number), quake3MaxPatchComponent)
				}
				grid[c][r][k] = parsed
			}
		}
	}
	tail := [3]string{"0", "0", "0"}
	if tailValue, ok := item.Get("tail"); ok && tailValue != nil {
		values, isList := tailValue.([]Value)
		if !isList || len(values) != 3 {
			return q3Refuse(Q3MapCodePatchInvalid, path, patchID, "a patch tail is three numbers")
		}
		for index, value := range values {
			text, err := q3Number(value, path, patchID, true)
			if err != nil {
				return err
			}
			tail[index] = text
		}
	}

	w.lines = append(w.lines,
		fmt.Sprintf("  // patch %d", ordinal),
		"  {", "    patchDef2", "    {", "      "+written,
		fmt.Sprintf("      ( %d %d %s )", width, height, strings.Join(tail[:], " ")),
		"      (")
	for _, column := range grid {
		points := make([]string, 0, len(column))
		for _, point := range column {
			spelled := make([]string, 0, 5)
			for _, component := range point {
				spelled = append(spelled, formatQuake3ExactNumber(component))
			}
			points = append(points, "( "+strings.Join(spelled, " ")+" )")
		}
		w.lines = append(w.lines, "        ( "+strings.Join(points, " ")+" )")
	}
	w.lines = append(w.lines, "      )", "    }", "  }")

	if quake3NetArea(grid) <= 1e-6 {
		w.warn(Quake3MapWarning{Code: "q3map_patch_degenerate", Path: path, ObjectID: patchID,
			Message: "the control net spans no area; it is written as it is and Q3Map2 builds it, but it draws nothing"})
	}
	w.noteShader(written, true)
	w.manifest.Counts.Patches++
	w.manifest.Counts.PatchControlPoints += width * height
	return nil
}

// quake3NetArea is AUP's `patchNetArea`: the control net's area summed over its quads.
func quake3NetArea(grid [][][5]float64) float64 {
	area := 0.0
	for c := 0; c+1 < len(grid); c++ {
		for r := 0; r+1 < len(grid[c]); r++ {
			a, b, d, e := grid[c][r], grid[c+1][r], grid[c][r+1], grid[c+1][r+1]
			p := [3]float64{e[0] - a[0], e[1] - a[1], e[2] - a[2]}
			q := [3]float64{d[0] - b[0], d[1] - b[1], d[2] - b[2]}
			cross := [3]float64{p[1]*q[2] - p[2]*q[1], p[2]*q[0] - p[0]*q[2], p[0]*q[1] - p[1]*q[0]}
			area += math.Sqrt(cross[0]*cross[0]+cross[1]*cross[1]+cross[2]*cross[2]) / 2
		}
	}
	return area
}

// finish sorts the manifest's sets and adds the per-shader warnings, so the manifest is a pure
// function of the document.
func (w *quake3Writer) finish() {
	names := make([]string, 0, len(w.shaders))
	for name := range w.shaders {
		names = append(names, name)
	}
	sort.Strings(names)
	w.manifest.Shaders = make([]Quake3MapShader, 0, len(names))
	for _, name := range names {
		shader := *w.shaders[name]
		w.manifest.Shaders = append(w.manifest.Shaders, shader)
		lower := strings.ToLower(shader.Written)
		switch {
		case quake1ToolTextures[lower]:
			w.warn(Quake3MapWarning{Code: "q3map_quake1_tool_name", Path: shader.Name,
				Message: fmt.Sprintf("%q is a Quake 1 tool name; in Quake III it is an ordinary shader Q3Map2 looks for as %s and draws (the Quake III tools are common/…); nothing was renamed", shader.Written, shader.Name)})
		case !strings.Contains(shader.Written, "/"):
			w.warn(Quake3MapWarning{Code: "q3map_shader_outside_directory", Path: shader.Name,
				Message: fmt.Sprintf("%q is not in a directory under textures/, as every Quake III shader is; it may be a Quake 1 WAD name, and nothing was substituted", shader.Written)})
		}
	}
	w.manifest.Models = assetList(w.models)
	w.manifest.Sounds = assetList(w.sounds)
	if w.manifest.Warnings == nil {
		w.manifest.Warnings = []Quake3MapWarning{}
	}
}

func assetList(uses map[string]*quake3AssetUse) []Quake3MapAsset {
	names := make([]string, 0, len(uses))
	for name := range uses {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Quake3MapAsset, 0, len(names))
	for _, name := range names {
		keys := make([]string, 0, len(uses[name].keys))
		for key := range uses[name].keys {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out = append(out, Quake3MapAsset{Name: name, Keys: keys, Entities: uses[name].entities})
	}
	return out
}

// IsQuake3MapCancelled reports whether err is a cancelled conversion.
func IsQuake3MapCancelled(err error) bool {
	var refusal *Quake3MapError
	return errors.As(err, &refusal) && refusal.Code == Q3MapCodeCancelled
}
