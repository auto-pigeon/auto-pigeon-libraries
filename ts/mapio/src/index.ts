/**
 * `@auto-pigeon/mapio` — the TypeScript map / APMap IO core shared by Auto-Pigeon's browser editor and any
 * other consumer: Quake `.map` readers and writers for Quake 1 (Classic and Valve 220), Quake II (face tails,
 * ADM-Q2 export admission) and Quake III (`brushDef` brush primitives and `patchDef2` patches), APMap decode
 * with legacy promotion, canonical serialization, identity and provenance, and on-demand validation.
 *
 * No UI, no network, no editor state. The host supplies the APMap contract (`setCurrentApmapVersion`, and
 * schemas to `inspectApmapText` / `configureApmapSchemaValidators`) from `@auto-pigeon/apmap-schema`; nothing
 * here names a version. Decoding and validation are separate calls, and nothing validates on its own.
 *
 * The module entry points under `@auto-pigeon/mapio/<dir>/<module>` are public too, for a consumer that wants
 * one module; this root entry is the curated surface, documented in README.md.
 */
export { reconstructBrush, BASE_WINDING_SIZE, WINDING_EPSILON, type PolygonFace } from './geometry/reconstructBrush.ts';
export { enclosedVolume, checkBrushConvexity, checkDocumentConvexity, CONVEXITY_TOLERANCE, MAX_CONVEXITY_FACES,
         type BrushConvexity, type BrushConvexityOptions, type BrushConvexityReason,
         type DocumentConvexityEntry } from './geometry/convexity.ts';
export { apmapLegalityReport, brushIsLegal, brushLegality, emissionLegality, faceProjectionLegality,
         sourceLegalityCodes,
         NO_INHERITED_ILLEGALITY,
         APMAP_LEGALITY_FINDING_LIMIT, MIN_TEXTURE_AXIS_LENGTH, THIN_BRUSH_SPAN,
         type ApmapLegalityFinding, type ApmapLegalityReport, type BrushLegality, type BrushLegalityCode,
         type BrushLegalityFinding, type BrushLegalityOptions, type BrushLegalitySeverity,
         type EmissionLegality, type FaceProjectionCode, type FaceProjectionFinding,
         type FaceProjectionLegality } from './geometry/brushLegality.ts';
export { classicAxes, classicReaderOf, faceProjectionAxes, planeNormal, rotateProjectionAxes, shiftCompensation, type ClassicReader, type Vec3 } from './geometry/projection.ts';
export { ApmapIssues, MAX_APMAP_ISSUES, MAX_ISSUES_PER_KIND, issueValue, pointer, summarizeApmapIssues,
         type ApmapIssue, type ApmapIssueSeverity, type ApmapIssueStage } from './issues.ts';
export { redactContext, redactString, redactValue } from './redact.ts';
export { inspectApmapText, apmapIsRenderable, apmapContractViolations, forgetApmapValidator,
         type ApmapInspection, type InspectOptions } from './apmap/validate.ts';
export { APMAP_AJV_OPTIONS, apmapSchemaDigest, canonicalSchemaText, configureApmapSchemaValidators,
         forgetApmapSchemaValidators, type PrecompiledApmapValidator } from './apmap/schemaValidator.ts';
export { scanApmapText, lineColumn } from './apmap/scan.ts';
export { decodeApmapDocument, promoteApmapDocument, parseApmapText, parsedMapFromApmap, parsedBrushFromApmap,
         apmapSourceIdentity, toSourceTranslation, issueFromDecodeError, faceMappingOf } from './apmap/document.ts';
export { apmapPatchCount, apmapPatches, quake3SurfaceFaults, QUAKE3_GAME, type Quake3SurfaceFault } from './apmap/quake3Surfaces.ts';
export type { ApmapAuthorship, ApmapGroup, ApmapGroupMember, ApmapGroupSource, ApmapObjectMemberKind, ApmapPatchItem,
              ApmapBrushPrimitivesProjection, ApmapQuakeProjection } from './apmap/types.ts';
export { isBrushPrimitivesProjection } from './apmap/types.ts';
export { parseQuake3Map, parseQuake3MapAsync, hasQuake3OnlySyntax, QUAKE3_PARSE_CODES,
         type Quake3ParsedMap, type Quake3Entity, type Quake3Brush, type Quake3Face, type Quake3Patch,
         type Quake3MapStats, type Quake3ParseOptions } from './map/quake3.ts';
export { apmapFromQuake3Map, apmapFromQuake3MapAsync, quake3MapShaderName, Quake3ImportError, QUAKE3_GAME_NAME, QUAKE3_MAP_DIALECT,
         QUAKE3_SHADER_PREFIX, QUAKE3_IMPORT_REFUSED_CODES,
         type Quake3ImportResult, type Quake3ImportSummary, type Quake3RejectedBrush, type Quake3EncodeOptions } from './apmap/quake3Encode.ts';
export { quake3BareToolFaces, QUAKE1_TOOL_TEXTURES, QUAKE3_MAX_SHADER_NAME, quake3ShaderKey, validQuake3LogicalPath } from './apmap/quake3Names.ts';
export { quake3PlaneNormal, quake3AxisBase, brushPrimitivesCovectors, brushPrimitivesMatrixFor, brushPrimitivesDeterminant,
         brushPrimitivesTexCoord, transportBrushPrimitives, type TextureCovectors } from './geometry/brushPrimitives.ts';
export { GROUP_MAX_DEPTH, descendantGroupIds, expandGroup, groupAncestry, groupGraphFaults, groupMemberId, groupParents,
         isObjectMember, rootGroupOf, type ApmapObjectMember, type GroupGraphFault } from './apmap/groupGraph.ts';
export { ApmapDecodeError, currentApmapVersion, setCurrentApmapVersion, forgetCurrentApmapVersion,
         apmapReadableVersions, isApmapReadable, unsupportedApmapMessage, deprecatedApmapMessage, isApmapBrush, isApmapPatch,
         type ApmapDocument, type ApmapEntity, type ApmapFace, type ApmapBrushItem, type ApmapContentItem,
         type ApmapFrame, type ApmapProjection, type ApmapRelationship, type ApmapRole, type ApmapTransform,
         type ApmapVec3, type ApmapVec4 } from './apmap/types.ts';
export { apmapFromParsedMap, apmapDocumentText, APMAP_ENCODER, type ApmapEncodeOptions, type ClassicFidelityReport } from './apmap/encode.ts';
export { CLASSIC_FIDELITY_BOUND_TEXELS, classicTexinfo, faithfulClassicNumbers, type ClassicFidelity, type ClassicFidelityOutcome } from './apmap/classicFidelity.ts';
export { applyPropertyEdits, lastOccurrenceOrder, lastWinsProperties, newPropertyIndex, propertyEditsBetween, propertyRecord, type PropertyEdits } from './apmap/propertyEdits.ts';
export { canonicalApmapText, canonicalApmapDocument, canonicalApmapNumber, exactApmapNumber, ApmapEncodeError } from './apmap/canonical.ts';
export { apmapDialectFor, isValve220Document, APMAP_DIALECT_CLASSIC, APMAP_DIALECT_VALVE220 } from './apmap/dialect.ts';
export { mapTextFromApmap, formatMapNumber, MapExportError, type MapExportOptions } from './map/writer.ts';
export { QUAKE2_CONTENTS, QUAKE2_SURFACE, QUAKE2_CLASSIC_CONTENTS_MASK, QUAKE2_CLASSIC_SURFACE_MASK, QUAKE2_VISIBLE_CONTENTS,
         QUAKE2_SURFACE_COMPILER, quake2TailNumbers, compileQuake2Face, quake2UnnamedBits, resolveQuake2Face, resolveQuake2Brush,
         quake2BitNames, quake2Hex, quake2SurfaceEditRefusal, quake2TailAfterEdit, quake2SurfaceReport,
         type Quake2ContentsName, type Quake2SurfaceName, type Quake2SurfaceNumbers, type Quake2WalHeader, type Quake2SurfaceSeverity,
         type Quake2SurfaceDiagnosticCode, type Quake2SurfaceDiagnostic, type Quake2CompiledFace, type Quake2FaceSurface,
         type Quake2BrushSurface, type Quake2SurfaceEdit, type Quake2SurfaceEditRefusal, type Quake2SurfaceReport,
         type Quake2SurfaceReportEntry } from './map/quake2Surface.ts';
export { deriveApmapId, mintApmapId, apmapIdMatchesDerivation, apmapIdentityComponents, apmapSourceListDigest,
         bareSourceMapName, sourceMapDerivedFrom, sourcePathFor,
         type ApmapDerivedFrom, type ApmapObjectKind, type DerivedFromAuthored, type DerivedFromOperation,
         type DerivedFromPackage, type DerivedFromSourceMap, type DerivedFromSynthetic } from './apmap/identity.ts';
export { analyseApmapIdentity, mandatedApmapId, repairApmapIdentity,
         APMAP_IDENTITY_REPAIR_OPERATION, IDENTITY_REPAIR_CONTRACT, IDENTITY_REPAIR_INTENT,
         IDENTITY_REPAIR_MAX_REJECTIONS,
         type IdentityRepairAnalysis, type IdentityRepairCounts, type IdentityRepairOutcome,
         type IdentityRepairRefusal, type IdentityRepairRejection, type IdentityRepairReplaced,
         type IdentityRepairRule, type IdentityRepairSubject } from './apmap/identityRepair.ts';
export { derivedIdFromPositional, positionalObjectId, POSITIONAL_OBJECT_ID } from './apmap/legacyIds.ts';
export { sha256Hex } from './hash/sha256.ts';
export { parseQuakeMap } from './map/parser.ts';
export type { FaceMapping, MapParseError, ParsedBrush, ParsedEntity, ParsedFace, ParsedMap, SourceRange } from './map/types.ts';
export { brushWindingReport, type BrushWindingReport } from './geometry/quakeWinding.ts';
export { checkPatch, isOddPatchDimension, patchNetArea, MAX_PATCH_COMPONENT, MAX_PATCH_DIMENSION, MIN_PATCH_DIMENSION,
         type PatchCheck, type PatchFault, type PatchPoint } from './geometry/quake3PatchCheck.ts';
export { COINCIDENT_NORMAL, COINCIDENT_OFFSET, MAX_FACE_TEXTURE_SCALE, MIN_FACE_TEXTURE_SCALE } from './geometry/tolerances.ts';
export { quake3SourceDigest, type Quake3RedundantPlaneBrush } from './apmap/quake3Encode.ts';
