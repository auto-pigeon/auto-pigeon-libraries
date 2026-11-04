package mapio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/quake3map"
)

// quake3ToAPMap is q3map-to-apmap: the bytes decoded as the editor decodes a picked file, read by
// the Quake III reader, admitted and encoded by apmap.FromQuake3Map, then validated when asked.
//
// Identity is the editor's: `source_map` derivation over the input's bare name and the SHA-256 of
// its BYTES, and the document id `<stem>:full_map`. Nothing partial is returned: a parse fault, an
// impossible brush, a bound or a cancellation is a refusal with no output.
func quake3ToAPMap(ctx context.Context, direction Direction, name string, data []byte, opts Options, result *Result) ([]byte, error) {
	if len(data) > MaxQuake3MapImportBytes {
		return nil, &Error{Code: RefusalTooLarge, Message: fmt.Sprintf(
			"%s is %d bytes; q3map-to-apmap converts maps up to %d bytes", name, len(data), MaxQuake3MapImportBytes)}
	}
	parsed, err := quake3map.Parse(ctx, quake3map.DecodeText(data))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	document, summary, err := apmap.FromQuake3Map(parsed, apmap.Quake3ImportOptions{
		SourceMap:    name,
		SourceSHA256: hex.EncodeToString(digest[:]),
		Contract:     opts.Contract,
		Context:      ctx,
	})
	if err != nil {
		return nil, err
	}
	result.Quake3Import = summary
	result.Warnings = append(result.Warnings, quake3ImportWarnings(summary)...)

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

// quake3ImportWarnings is the summary's non-fatal facts as typed warnings: nothing the import left
// out or changed goes unsaid.
func quake3ImportWarnings(summary *apmap.Quake3ImportSummary) []Warning {
	warnings := []Warning{}
	if summary.RedundantPlanes > 0 {
		brushes := make([]string, 0, len(summary.RedundantPlaneBrushes))
		for _, brush := range summary.RedundantPlaneBrushes {
			faces := make([]string, 0, len(brush.Faces))
			for _, face := range brush.Faces {
				faces = append(faces, fmt.Sprint(face))
			}
			brushes = append(brushes, fmt.Sprintf("entity %d brush %d (line %d) face %s", brush.Entity, brush.Brush, brush.Line, strings.Join(faces, ",")))
		}
		warnings = append(warnings, Warning{Code: WarningQuake3RedundantPlanes, Text: fmt.Sprintf(
			"q3map_redundant_planes: %d plane(s) that bound no part of their brush were left out: %s",
			summary.RedundantPlanes, strings.Join(brushes, "; "))})
	}
	if len(summary.Risks) > 0 {
		codes := make([]string, 0, len(summary.Risks))
		for code := range summary.Risks {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		parts := make([]string, 0, len(codes))
		for _, code := range codes {
			parts = append(parts, fmt.Sprintf("%s %d", code, summary.Risks[code]))
		}
		warnings = append(warnings, Warning{Code: WarningQuake3ImportRisk, Text: fmt.Sprintf(
			"q3map_import_risk: brushes imported with findings that do not refuse a Quake III map: %s", strings.Join(parts, ", "))})
	}
	if summary.PrefixedShaders > 0 {
		warnings = append(warnings, Warning{Code: WarningQuake3ShaderPrefix, Text: fmt.Sprintf(
			"q3map_shader_prefix_removed: %d shader name(s) written with textures/ are stored without it (Q3Map2 adds it)", summary.PrefixedShaders)})
	}
	return warnings
}
