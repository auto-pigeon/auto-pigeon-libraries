/**
 * APMap derived object identity — `@auto-pigeon/apmap-schema`'s `SEMANTICS.md`, and §3.5 of the
 * published 1.0 specification archived beside it as `deprecated/1.0/PUBLISHED-README.md`.
 *
 *     US       = U+001F
 *     payload  = "apmap/1.0" US kind US c1 US c2 US … US cN
 *     digest   = lowercase_hex(SHA-256(UTF-8(payload)))[0:16]
 *     id       = prefix(kind) + "_" + digest
 *
 * `kind` is inside the payload, so an entity and the brush at the same source path cannot collide.
 *
 * The components are **exactly** the identity inputs of the `derived_from` record and nothing else.
 * `source_entity_index`, `origin_at_extraction` and `bounds_at_extraction` are provenance: an id
 * derived from geometry would change on every move and every reference to it would dangle (§3.6).
 *
 * This is the same algorithm every conforming producer implements, and `test/apmapRoundTrip.test.ts` checks
 * it against the frozen clean APMap corpus (38 maps), an independent producer's published output, rather
 * than against a fixture of our own — a producer that agrees
 * with itself proves nothing.
 */
import { sha256Hex } from '../hash/sha256.ts';
import type { ApmapRole } from './types.ts';

/** `patch` from APMap 1.5 (SEMANTICS §3.5): `pat_`, derived exactly like a brush at path `e<n>.p<n>`. */
export type ApmapObjectKind = 'entity' | 'brush' | 'face' | 'patch';

export type DerivedFromSourceMap = {
  kind: 'source_map';
  /** A bare file name. Never a path — an absolute path in a portable document is a leak. */
  source_map?: string;
  source_sha256?: string;
  /** `e30`, `e30.b0`, `e30.b0.f0` — the object's address in the **source** map. */
  source_path: string;
  source_entity_index?: number;
  source_brush_position?: number;
};
export type DerivedFromPackage = {
  kind: 'package';
  package_id: string;
  role: ApmapRole;
  /** The object's address in **this** document. */
  object_path: string;
  source_map?: string;
  source_entity_index?: number;
  source_brush_position?: number;
};
export type DerivedFromAuthored = { kind: 'authored'; document_id: string; operation_id: string; ordinal: number };
export type DerivedFromSynthetic = { kind: 'synthetic'; generator: string; reason?: string; source_object?: string };
/**
 * APMap 1.1. An object that exists because an operation **replaced** an earlier one.
 *
 * The distinction from `authored` is the whole point of the kind, and it is easy to get backwards:
 * `authored` records an object an operation *created*, and derives its id from
 * `document_id`+`operation_id`+`ordinal`; `operation` records an object that *supersedes* one that
 * was already there, and names it in `replaces`. It exists so provenance can stay truthful after an
 * edit — a brush a carve rewrote can no longer honestly claim a source-map address, because the
 * geometry at that address is not this geometry any more.
 *
 * `intent` and `metadata` are **annotation, never authorization** (schema 1.1's words). Nothing may
 * read either to decide whether an operation was permitted, what it could touch, or how to re-apply
 * it: they are written by whoever ran the operation, nothing validates them against what the
 * operation did, and a document is not a trusted channel. They are for a human reading provenance
 * months later.
 */
export type DerivedFromOperation = {
  kind: 'operation';
  operation_id: string;
  /** A registry name, e.g. `csg_carve`. */
  operation?: string;
  ordinal?: number;
  /** The ids this object supersedes. Absent when an operation rewrote an object and kept its id. */
  replaces?: string[];
  intent?: string;
  metadata?: Record<string, unknown>;
};
export type ApmapDerivedFrom = DerivedFromSourceMap | DerivedFromPackage | DerivedFromAuthored | DerivedFromSynthetic | DerivedFromOperation;

const UNIT_SEPARATOR = String.fromCharCode(0x1f);
const PREFIX: Record<ApmapObjectKind, string> = { entity: 'ent', brush: 'brs', face: 'fac', patch: 'pat' };

/**
 * The digest components for a `derived_from` record, or `null` for `synthetic`, whose id 1.0 leaves
 * to the producer. An absent optional component contributes an empty string rather than being
 * skipped, so a document that names its source and one that does not never derive the same id.
 */
export function apmapIdentityComponents(derived: ApmapDerivedFrom): string[] | null {
  switch (derived.kind) {
    case 'source_map':
      return [derived.source_map ?? '', derived.source_sha256 ?? '', derived.source_path];
    case 'package':
      return [derived.package_id, derived.role, derived.object_path];
    case 'authored':
      return [derived.document_id, derived.operation_id, String(derived.ordinal)];
    case 'synthetic':
      return null;
    // 1.1 adds the kind but not a derivation for it. §3.5 assigns identity inputs to `source_map`,
    // `package` and `authored` and to nothing else, and `operation`'s own members do not obviously
    // supply a unique triple: `ordinal` is optional, there is no `document_id`, and `replaces` may
    // be absent because the operation kept the id it already had. Inventing one here would produce
    // ids that disagree with every other producer's, which is the one failure mode a derived id
    // exists to prevent — so this returns `null` on the same terms as `synthetic`: no derivation is
    // mandated, the producer assigns the id. Reported to AULIBS as a question for the schema.
    case 'operation':
      return null;
  }
}

/**
 * `ent_…` / `brs_…` / `fac_…` for a `derived_from` record.
 *
 * Throws for `synthetic` and for 1.1's `operation`, neither of which the format gives a derivation.
 */
export function deriveApmapId(kind: ApmapObjectKind, derived: ApmapDerivedFrom): string {
  const components = apmapIdentityComponents(derived);
  if (!components) throw new Error(`APMap mandates no id derivation for derived_from.kind '${derived.kind}'; the producer assigns it`);
  const payload = ['apmap/1.0', kind, ...components].join(UNIT_SEPARATOR);
  return `${PREFIX[kind]}_${sha256Hex(payload).slice(0, 16)}`;
}

/**
 * A fresh `ent_…` / `brs_…` / `fac_…` id for an object an **edit** created, from components the
 * editor chooses.
 *
 * Needed because APMap's derivation cannot serve here. `deriveApmapId` hashes a `derived_from`
 * record, and an operation that turns one brush into four gives all four the *same* record — the
 * provenance of the brush they came out of — so deriving would give four objects one id. §3.5 also
 * mandates no derivation at all for `synthetic` and for 1.1's `operation`: it says the producer
 * assigns the id. This is that assignment, made deterministic.
 *
 * The payload's domain tag is `auto-pigeon/mint`, deliberately **not** `apmap/1.0`, so a minted id
 * can never collide with a derived one however the components happen to line up: the two hash
 * different strings by construction rather than by luck.
 *
 * Deterministic on purpose. A random id would be simpler and would cost byte-identical convergence:
 * two clients cutting the same brush the same way must produce the same document, and a UUID makes
 * that impossible to check.
 */
export function mintApmapId(kind: ApmapObjectKind, components: readonly string[]): string {
  const payload = ['auto-pigeon/mint', kind, ...components].join(UNIT_SEPARATOR);
  return `${PREFIX[kind]}_${sha256Hex(payload).slice(0, 16)}`;
}

/**
 * A bare source-map file name from whatever the caller happens to hold — a workspace-relative path,
 * a Windows path, a plain name. `derived_from.source_map` is specified as a bare name, and an
 * absolute path in a portable document is a leak (§3.6).
 */
export const bareSourceMapName = (value: string | undefined): string | undefined =>
  value ? value.split(/[\\/]/).pop() || undefined : undefined;

/**
 * The `derived_from` record for an object at `sourcePath` in a `.map`.
 *
 * The single place this record is built. Both producers of source-map identity — the APMap encoder
 * and the `.map` render path — call it, because two implementations that must agree eventually will
 * not, and this one disagreeing is what `20260803_24` was written to fix.
 */
export const sourceMapDerivedFrom = (sourcePath: string, sourceMap?: string, sourceSha256?: string): DerivedFromSourceMap => ({
  kind: 'source_map',
  ...(sourceMap ? { source_map: sourceMap } : {}),
  ...(sourceSha256 ? { source_sha256: sourceSha256 } : {}),
  source_path: sourcePath,
});

/**
 * The `derived_from` record for an object a **minting** operation produced — a split fragment, a
 * carved piece, a shell wall, a sculpted cell, a bend cell, a component-split face.
 *
 * ## Why every minted object needs one
 *
 * `mintApmapId` exists because APMap's derivation cannot serve an operation that turns one brush
 * into four: all four share the ancestor's record and deriving would give them one id. What was
 * missing is the other half. A minted object used to keep the **ancestor's** `derived_from` —
 * `{ kind: "source_map", source_path: "e0.b0" }` — while wearing a new id, and under
 * `id_policy: "derived"` that breaks the contract twice over: SEM-10, because the id is not the
 * digest of the record, and SEM-8 wherever the record was dropped instead.
 *
 * It is also simply false. The schema says it in the `operation` kind's own words: *"an object whose
 * geometry an operation changed can no longer honestly claim a source-map location, because the
 * geometry no longer matches what is at that location."* Half a brush is not at `e0.b0`.
 *
 * ## Why `operation` and not `authored`
 *
 * `authored` records an object an operation **created** and derives its id from
 * `(document_id, operation_id, ordinal)` — which is what `insertApmapFragment` uses, correctly, for
 * Paste, Clone and the array. `operation` records an object that **supersedes** one that was already
 * there, and names it in `replaces`. §3.5 mandates no derivation for it (see
 * {@link apmapIdentityComponents}), so SEM-10 does not apply and the minted id stands — which is
 * exactly what a caller needs: the record becomes true, the id does not move, and no document that
 * round-tripped before changes shape.
 *
 * `operationId` is whatever the operation already computes as its deterministic key
 * (`csgOperationId`, a crop's `rectKey`, a bend's cell key). There is no clock and no counter here,
 * for the reason `mintApmapId` records: two clients running the same operation must emit the same
 * bytes.
 *
 * `AUP 120G` — an account map that had been split, carved, bent or sculpted was refused by AUE with
 * *"The extractor could not read the input file"*, because AUE validates the document it is handed
 * against the contract the document declares.
 */
export const operationDerivedFrom = (operationId: string, operation: string, ordinal?: number,
                                     replaces?: readonly string[]): DerivedFromOperation => ({
  kind: 'operation',
  operation_id: operationId,
  operation,
  ...(ordinal === undefined ? {} : { ordinal }),
  ...(replaces && replaces.length ? { replaces: [...replaces] } : {}),
});

/**
 * **A subject list, as a bounded key** — `AUP 120S`.
 *
 * Every deterministic operation id in this package is built from the objects it acts on, and the
 * obvious way to write that is `ids.join('+')`. It is also the way that put **82 MB** into one array
 * and 12 MB into another, because an operation id is not a transient key: `authoredDerivedFrom`
 * stores it verbatim in the `derived_from` of *every object the operation mints*, and
 * `operationDerivedFrom` does the same. So a 64-brush selection arrayed 16 times wrote its own
 * source list — a 1 250-character string — into all 1 024 outputs, and the cost was
 * `sources × outputs`: quadratic in the one edit that produces the most of both, in the document, in
 * the save, and in the collaboration envelope. `AUP 120S` measured a 256-source array at **82 MB of
 * envelope** against an 8 MiB wire cap, so the operation could not have been sent at all.
 *
 * Hashing the list keeps every property the callers' own comments claim for it and drops the one
 * nobody wanted. There is **no clock and no counter**, so two clients running the same operation on
 * the same selection still emit the same bytes; the digest is order-sensitive, because
 * `[a, b]` and `[b, a]` genuinely mint different ordinals; and 32 hex characters is well past
 * collision-relevant for a key whose whole job is to separate one gesture from another.
 *
 * The **operation name and its parameters stay in the clear** in every caller, which is what keeps
 * an id readable in a log: only the unbounded part is folded. A caller with one subject may still
 * name it directly — `sculptOperationId` does — because one id is bounded already.
 *
 * The separator is this file's own `UNIT_SEPARATOR`, and it is not decoration: joined with nothing,
 * `['ab']` and `['a', 'b']` would be one string and therefore one key, which would let two different
 * selections mint the same ids. Every other payload in this file is joined the same way.
 */
export const apmapSourceListDigest = (ids: readonly string[]): string =>
  sha256Hex(['auto-pigeon/operation-subjects', ...ids].join(UNIT_SEPARATOR)).slice(0, 32);

/**
 * The `operation` registry names this package mints under. One per kernel operation, because
 * `derived_from.operation` is a `registry_name` (`^[a-z][a-z0-9_]{0,63}$`) and a free string here
 * would be a vocabulary nobody could enumerate.
 */
export const APMAP_OPERATION = {
  crop: 'crop',
  split: 'split_brush',
  csgSubtract: 'csg_subtract',
  csgIntersect: 'csg_intersect',
  csgMerge: 'csg_merge',
  csgWrapMerge: 'csg_wrap_merge',
  csgHollow: 'csg_hollow',
  sculpt: 'sculpt',
  bend: 'bend',
  squeeze: 'squeeze',
  /** `AUP 123` — the procedural axial deform. Named for the family, not for the pattern chosen. */
  axialDeform: 'axial_deform',
  /**
   * `AUP 124` — the localized surface deform. Named for the family, not for the pattern chosen, for
   * `axial_deform`'s reason: a `bubble` and a `crater` are one operation with one parameter different,
   * and four operation names would make *"which operations does this product have"* a question you
   * answer by reading a panel.
   */
  surfaceDeform: 'surface_deform',
  component: 'component_edit',
  /**
   * Not a geometry operation and the only entry that is not: `AUP 120J`'s repair of the provenance a
   * pre-`AUP 120G` writer left behind. It lives here because this is the package's ONE registry of
   * operation names, and a second list somewhere else is how two vocabularies start.
   */
  identityRepair: 'identity_metadata_repair',
} as const;

/**
 * `e30`, `e30.b0`, `e30.b0.f0` — the `source_path` of an object at these indices, and the kind that
 * goes with it. The depth of the address *is* the kind, which is why they are returned together.
 */
export function sourcePathFor(entityIndex: number, brushIndex?: number, faceIndex?: number): { kind: ApmapObjectKind; sourcePath: string } {
  if (brushIndex === undefined) return { kind: 'entity', sourcePath: `e${entityIndex}` };
  if (faceIndex === undefined) return { kind: 'brush', sourcePath: `e${entityIndex}.b${brushIndex}` };
  return { kind: 'face', sourcePath: `e${entityIndex}.b${brushIndex}.f${faceIndex}` };
}

/**
 * True when an id is the digest its own `derived_from` record demands — SEM-10, checkable here.
 *
 * A record the format mandates **no** derivation for — `synthetic`, and 1.1's `operation` — passes
 * unconditionally, because there is nothing for the id to disagree with: §3.5 says the producer
 * assigns it. That is the same answer AUE's `MandatedID` gives by returning the empty string, and
 * the two must agree or one of them reports a violation the other does not (`AUP 120G`).
 *
 * It answers rather than throwing. `deriveApmapId` throws for those two kinds on purpose — a caller
 * asking for a digest that does not exist has made a mistake — but a caller asking *"does this id
 * hold up"* has not, and the honest answer is yes.
 */
export const apmapIdMatchesDerivation = (kind: ApmapObjectKind, id: string, derived: ApmapDerivedFrom): boolean =>
  apmapIdentityComponents(derived) === null ? true : id === deriveApmapId(kind, derived);
