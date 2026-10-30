package apmap

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestObjectPath(t *testing.T) {
	cases := []struct {
		entity, brush, face int
		want                string
		wantErr             string
	}{
		{entity: 30, brush: -1, face: -1, want: "e30"},
		{entity: 30, brush: 0, face: -1, want: "e30.b0"},
		{entity: 30, brush: 0, face: 7, want: "e30.b0.f7"},
		{entity: 0, brush: 0, face: 0, want: "e0.b0.f0"},
		{entity: -1, brush: -1, face: -1, wantErr: "non-negative"},
		{entity: 0, brush: -1, face: 3, wantErr: "requires a brush index"},
	}

	for _, testCase := range cases {
		got, err := ObjectPath(testCase.entity, testCase.brush, testCase.face)
		if testCase.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Errorf("ObjectPath(%d,%d,%d) error = %v, want one containing %q",
					testCase.entity, testCase.brush, testCase.face, err, testCase.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("ObjectPath(%d,%d,%d): %v", testCase.entity, testCase.brush, testCase.face, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("ObjectPath(%d,%d,%d) = %q, want %q", testCase.entity, testCase.brush, testCase.face, got, testCase.want)
		}
		// Every path this builds must satisfy the pattern the format declares.
		if !ObjectPathPattern.MatchString(got) {
			t.Errorf("ObjectPath produced %q, which does not match the declared pattern", got)
		}
	}
}

func TestDeriveIDRejectsBadInput(t *testing.T) {
	if _, err := DeriveID("vertex", "a"); err == nil {
		t.Error("an unknown kind must be rejected")
	}
	if _, err := DeriveID(KindEntity); err == nil {
		t.Error("no components must be rejected")
	}
	if _, err := DeriveID(KindEntity, "a\x1fb"); err == nil {
		t.Error("a component containing U+001F must be rejected — it would make two component lists digest alike")
	}
}

// TestDeriveIDSeparatorIsLoadBearing is the property the separator exists for,
// asserted rather than assumed: ("ab","c") and ("a","bc") must not collide.
func TestDeriveIDSeparatorIsLoadBearing(t *testing.T) {
	first, err := DeriveID(KindEntity, "ab", "c")
	if err != nil {
		t.Fatal(err)
	}
	second, err := DeriveID(KindEntity, "a", "bc")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("component boundaries must be preserved in the digest")
	}
}

// TestDeriveIDIsStableAcrossKinds checks the prefix and the shape.
func TestDeriveIDIsStableAcrossKinds(t *testing.T) {
	for kind, prefix := range map[Kind]string{KindEntity: "ent_", KindBrush: "brs_", KindFace: "fac_"} {
		id, err := DeriveID(kind, "pkg", "full_map", "e0")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(id, prefix) {
			t.Errorf("DeriveID(%s) = %q, want the %q prefix", kind, id, prefix)
		}
		if len(id) != len(prefix)+DigestLength {
			t.Errorf("DeriveID(%s) = %q, want %d hex characters after the prefix", kind, id, DigestLength)
		}
	}
	// The three kinds must not collide on the same components — kind is a
	// digest input precisely so a brush and its face cannot share an id.
	entity, _ := DeriveID(KindEntity, "pkg", "full_map", "e0.b0")
	brush, _ := DeriveID(KindBrush, "pkg", "full_map", "e0.b0")
	face, _ := DeriveID(KindFace, "pkg", "full_map", "e0.b0")
	if entity[4:] == brush[4:] || brush[4:] == face[4:] {
		t.Error("kind must be part of the digest, not only the prefix")
	}
}

// TestDeriveIDDigestsAreFrozen pins exact digests over every identity shape this package builds.
// A self-consistent implementation of the wrong payload layout would pass every property test in
// this file, so the digests themselves are frozen from AUE output: a change to the separator, the
// encoding, the prefix or the truncation shows up here as a changed id.
func TestDeriveIDDigestsAreFrozen(t *testing.T) {
	sha := strings.Repeat("a", 64)
	pkg := PackageIdentity{PackageID: "pkg-1", Role: "full_map"}
	pkgContext := PackageIdentity{PackageID: "pkg-1", Role: "reference_context"}
	source := SourceMapIdentity{SourceMap: "e1m1.map", SourceSHA256: sha}
	authored := AuthoredIdentity{DocumentID: "doc-1", OperationID: "op-1"}

	// A table rather than a sequence of statements so a mismatch names the case rather than a
	// line number.
	cases := []struct {
		name string
		call func() (string, error)
		want string
	}{
		{"derive/entity/one", func() (string, error) { return DeriveID(KindEntity, "solo") }, "ent_3dd6e419cd7d6b2b"},
		{"derive/entity/three", func() (string, error) { return DeriveID(KindEntity, "pkg", "full_map", "e0") }, "ent_b4b0297b429d7509"},
		{"derive/brush", func() (string, error) { return DeriveID(KindBrush, "pkg", "prefab_core", "e12.b3") }, "brs_a054eba6836eb588"},
		{"derive/face", func() (string, error) { return DeriveID(KindFace, "pkg", "cropped_context", "e12.b3.f5") }, "fac_ad626e0e11931d92"},
		{"derive/unicode", func() (string, error) { return DeriveID(KindEntity, "pakkét", "full_map", "e0") }, "ent_79ca15c274a7ce21"},
		{"derive/empty-comp", func() (string, error) { return DeriveID(KindEntity, "", "full_map", "e0") }, "ent_9b38f3b929b88305"},
		{"path/entity", func() (string, error) { return ObjectPath(30, -1, -1) }, "e30"},
		{"path/brush", func() (string, error) { return ObjectPath(30, 0, -1) }, "e30.b0"},
		{"path/face", func() (string, error) { return ObjectPath(30, 0, 7) }, "e30.b0.f7"},
		{"package/entity", func() (string, error) { return pkg.IDFor(KindEntity, "e0") }, "ent_cf0b0a92581de580"},
		{"package/brush", func() (string, error) { return pkgContext.IDFor(KindBrush, "e0.b1") }, "brs_53700ab43e8a5fe8"},
		{"source/entity", func() (string, error) { return source.IDFor(KindEntity, "e4") }, "ent_2166df8bbd1c15a6"},
		{"source/face", func() (string, error) { return source.IDFor(KindFace, "e4.b0.f2") }, "fac_98f702de6130e973"},
		{"authored/zero", func() (string, error) { return authored.IDFor(KindEntity, 0) }, "ent_7c5dc8588d7e12d6"},
		{"authored/large", func() (string, error) { return authored.IDFor(KindBrush, 4096) }, "brs_2fc4c1ae968c1c53"},
	}

	for _, testCase := range cases {
		got, err := testCase.call()
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		if got != testCase.want {
			t.Errorf("%s = %q, frozen %q", testCase.name, got, testCase.want)
		}
	}
}

func TestIdentityValidation(t *testing.T) {
	if err := (PackageIdentity{Role: "full_map"}).Validate(); err == nil {
		t.Error("an empty package id must be rejected")
	}
	if err := (PackageIdentity{PackageID: "p", Role: "context"}).Validate(); err == nil {
		t.Error("an unknown role must be rejected — the enum is the schema's, not a suggestion")
	}
	if err := (SourceMapIdentity{SourceSHA256: strings.Repeat("a", 64)}).Validate(); err == nil {
		t.Error("an empty source map name must be rejected")
	}
	for _, bad := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63), strings.Repeat("g", 64)} {
		if err := (SourceMapIdentity{SourceMap: "m", SourceSHA256: bad}).Validate(); err == nil {
			t.Errorf("SHA-256 %q must be rejected: it must be 64 lowercase hex characters", bad)
		}
	}
	if err := (AuthoredIdentity{DocumentID: "d"}).Validate(); err == nil {
		t.Error("a missing operation id must be rejected")
	}
	if _, err := (AuthoredIdentity{DocumentID: "d", OperationID: "o"}).IDFor(KindEntity, -1); err == nil {
		t.Error("a negative ordinal must be rejected")
	}
}

// TestRecordMembersAreFrozen checks the derived_from records, which are what ExpectedID reads
// back — so a record whose member names or order drifted would break SEM-10 checking without
// breaking any digest. The member lists are frozen from AUE output; order is meaning under SER-5/6.
func TestRecordMembersAreFrozen(t *testing.T) {
	sha := strings.Repeat("a", 64)
	pkg := PackageIdentity{PackageID: "pkg-1", Role: "full_map"}

	plain, err := pkg.Record("e0")
	if err != nil {
		t.Fatal(err)
	}
	withProvenance, err := pkg.Record("e0",
		Member{Key: "bounds_at_extraction", Value: []Value{json.Number("1"), json.Number("2"), json.Number("3")}},
		// A nil value is dropped rather than written as null.
		Member{Key: "notes", Value: nil},
		Member{Key: "corpus", Value: "id1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	sourceRecord, err := (SourceMapIdentity{SourceMap: "e1m1.map", SourceSHA256: sha}).Record("e4")
	if err != nil {
		t.Fatal(err)
	}
	authoredRecord, err := (AuthoredIdentity{DocumentID: "doc-1", OperationID: "op-1"}).Record(3)
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name   string
		record *Object
		want   string
	}{
		{"package", plain, "kind,package_id,role,object_path"},
		{"package+", withProvenance, "kind,package_id,role,object_path,bounds_at_extraction,corpus"},
		{"source", sourceRecord, "kind,source_map,source_sha256,source_path"},
		{"authored", authoredRecord, "kind,document_id,operation_id,ordinal"},
	} {
		if got := strings.Join(testCase.record.Keys(), ","); got != testCase.want {
			t.Errorf("%s record members = %s, frozen %s", testCase.name, got, testCase.want)
		}
	}
	if withProvenance.Has("notes") {
		t.Error("a nil provenance value must be dropped, not written as null")
	}
}

// TestExpectedIDOverPublishedVectorsIsFrozen runs ExpectedID over the derived_from records in the
// published APMap 1.0 corpus rather than over hand-written ones. Those are the records SEM-10 is
// checked against. The corpus is immutable deprecated evidence, so the complete walk — every
// record's location and the id it mandates — is frozen from AUE output as a count and a digest over
// the sorted "location=id" lines.
func TestExpectedIDOverPublishedVectorsIsFrozen(t *testing.T) {
	const (
		frozenRecords = 34
		frozenDigest  = "fdd210a88cc07b60e309f4d25de66bf02c034561ff53ddb89582ed532115a037"
	)
	root := mapperRoot(t)

	var lines []string
	for _, kind := range []string{"valid", "invalid"} {
		files, _ := filepath.Glob(filepath.Join(root, vectorRoot, kind, "*.apmap"))
		for _, file := range files {
			document, err := DecodeJSONFile(file)
			if err != nil {
				continue
			}
			object, ok := document.(*Object)
			if !ok {
				continue
			}
			for key, id := range walkDerivedFrom(t, object, kind+"/"+filepath.Base(file)) {
				lines = append(lines, key+"="+id)
			}
		}
	}
	sort.Strings(lines)

	if len(lines) != frozenRecords {
		t.Fatalf("found %d derived_from records in the corpus, frozen %d:\n  %s",
			len(lines), frozenRecords, strings.Join(lines, "\n  "))
	}
	if got := sha256Hex(strings.Join(lines, "\n")); got != frozenDigest {
		t.Fatalf("mandated-id digest = %s, frozen %s:\n  %s", got, frozenDigest, strings.Join(lines, "\n  "))
	}
}

// walkDerivedFrom maps each derived_from record in document to the id it mandates, keyed by
// "<name>/e<entity>[.c<content>[.f<face>]]".
func walkDerivedFrom(t *testing.T, document *Object, name string) map[string]string {
	t.Helper()
	out := map[string]string{}

	entities, _ := document.Get("entities")
	entityList, _ := entities.([]Value)
	for index, entry := range entityList {
		entity, ok := entry.(*Object)
		if !ok {
			continue
		}
		if record, present := entity.Get("derived_from"); present {
			out[name+"/e"+strconv.Itoa(index)] = MandatedID(KindEntity, record)
		}

		content, _ := entity.Get("content")
		contentList, _ := content.([]Value)
		for position, item := range contentList {
			object, ok := item.(*Object)
			if !ok {
				continue
			}
			if kind, _ := object.Get("kind"); kind != "brush" {
				continue
			}
			brush := object
			if nested, present := object.Get("brush"); present {
				if inner, ok := nested.(*Object); ok {
					brush = inner
				}
			}
			prefix := name + "/e" + strconv.Itoa(index) + ".c" + strconv.Itoa(position)
			if record, present := brush.Get("derived_from"); present {
				out[prefix] = MandatedID(KindBrush, record)
			}
			faces, _ := brush.Get("faces")
			faceList, _ := faces.([]Value)
			for faceIndex, faceEntry := range faceList {
				face, ok := faceEntry.(*Object)
				if !ok {
					continue
				}
				if record, present := face.Get("derived_from"); present {
					out[prefix+".f"+strconv.Itoa(faceIndex)] = MandatedID(KindFace, record)
				}
			}
		}
	}
	return out
}

func TestExpectedIDMandatesNothingForSynthetic(t *testing.T) {
	synthetic := NewObject()
	synthetic.Set("kind", "synthetic")
	synthetic.Set("synthetic_reason", "context_crop")

	id, err := ExpectedID(KindFace, synthetic)
	if err != nil {
		t.Fatalf("a synthetic record is well-formed, it simply mandates nothing: %v", err)
	}
	if id != "" {
		t.Errorf("a synthetic record must mandate no identity in 1.0, got %q", id)
	}

	// Not an object at all, and an unknown kind: both mandate nothing.
	for _, value := range []Value{nil, "text", json.Number("1"), []Value{}} {
		if id, err := ExpectedID(KindFace, value); err != nil || id != "" {
			t.Errorf("ExpectedID(%#v) = (%q, %v), want (\"\", nil)", value, id, err)
		}
	}
}

// TestExpectedIDReportsMalformedRecords: "mandates nothing" and "the record is
// broken" are different answers, and collapsing them would let a malformed
// derived_from pass SEM-10 silently. The error return distinguishes them;
// MandatedID collapses them again for a caller that only wants the id.
func TestExpectedIDReportsMalformedRecords(t *testing.T) {
	broken := NewObject()
	broken.Set("kind", "package")
	broken.Set("package_id", "pkg")
	// role and object_path are missing.

	if _, err := ExpectedID(KindEntity, broken); err == nil {
		t.Error("a package record missing its members must be reported, not silently treated as mandating nothing")
	}
	if id := MandatedID(KindEntity, broken); id != "" {
		t.Errorf("MandatedID must collapse a malformed record to no id, got %q", id)
	}

	badOrdinal := NewObject()
	badOrdinal.Set("kind", "authored")
	badOrdinal.Set("document_id", "d")
	badOrdinal.Set("operation_id", "o")
	badOrdinal.Set("ordinal", "not a number")
	if _, err := ExpectedID(KindEntity, badOrdinal); err == nil {
		t.Error("a non-numeric ordinal must be reported")
	}
}

// TestExpectedIDRoundTripsWithRecord is the property SEM-10 rests on: an id
// built by an identity type must be exactly the id its own record mandates.
func TestExpectedIDRoundTripsWithRecord(t *testing.T) {
	sha := strings.Repeat("b", 64)

	pkg := PackageIdentity{PackageID: "pkg-9", Role: "prefab_core"}
	record, err := pkg.Record("e2.b1")
	if err != nil {
		t.Fatal(err)
	}
	built, err := pkg.IDFor(KindBrush, "e2.b1")
	if err != nil {
		t.Fatal(err)
	}
	mandated, err := ExpectedID(KindBrush, record)
	if err != nil {
		t.Fatal(err)
	}
	if built != mandated {
		t.Errorf("package: built %q, record mandates %q", built, mandated)
	}

	source := SourceMapIdentity{SourceMap: "e1m1.map", SourceSHA256: sha}
	record, err = source.Record("e2.b1.f3")
	if err != nil {
		t.Fatal(err)
	}
	built, err = source.IDFor(KindFace, "e2.b1.f3")
	if err != nil {
		t.Fatal(err)
	}
	mandated, err = ExpectedID(KindFace, record)
	if err != nil {
		t.Fatal(err)
	}
	if built != mandated {
		t.Errorf("source_map: built %q, record mandates %q", built, mandated)
	}

	authored := AuthoredIdentity{DocumentID: "doc", OperationID: "op"}
	record, err = authored.Record(17)
	if err != nil {
		t.Fatal(err)
	}
	built, err = authored.IDFor(KindEntity, 17)
	if err != nil {
		t.Fatal(err)
	}
	mandated, err = ExpectedID(KindEntity, record)
	if err != nil {
		t.Fatal(err)
	}
	if built != mandated {
		t.Errorf("authored: built %q, record mandates %q", built, mandated)
	}
}
