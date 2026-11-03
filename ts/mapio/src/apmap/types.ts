/**
 * APMap 1.0 — the native geometry document, as the frontend sees it.
 *
 * These types mirror the one current schema in `@auto-pigeon/apmap-schema`. The backend is the
 * validating consumer (it owns the published schema and the semantic rules); the frontend decodes
 * a document it has been served, so the checks here are the structural ones a renderer needs
 * before it touches a number — not a second schema implementation.
 *
 * An entity is an **ordered** list of content items, because a `.map` entity may repeat a key and
 * the position of a key relative to a brush is significant. Nothing here flattens that ordering.
 */
export type ApmapVec3 = [number, number, number];
export type ApmapVec4 = [number, number, number, number];

export type ApmapTransform =
  | { type: 'identity' }
  | { type: 'translation'; translation: ApmapVec3 };

export type ApmapFrame = {
  space: 'source_world' | 'prefab_local';
  units: string;
  coordinate_system?: { handedness: string; up_axis: string };
  to_source: ApmapTransform;
};

/**
 * APMap 1.5: a Quake III brushDef face's 2x3 texture matrix, kept verbatim (SEMANTICS.md §6.2a).
 *
 * `Q3_001` kept it out of {@link ApmapProjection} and refused it at the decode door; `Q3_002` made it
 * a member: the renderer draws it (`geometry/brushPrimitives.ts` owns its arithmetic), the rigid and
 * affine transports carry it exactly, and a tool that cannot carry it says so. It is still never
 * CONVERTED to or from a classic projection — that needs the image size, which a document does not hold.
 */
export type ApmapBrushPrimitivesProjection = { mode: 'brush_primitives'; matrix: [[number, number, number], [number, number, number]] };

export type ApmapProjection =
  | { mode: 'classic'; shift: [number, number]; rotation: number; scale: [number, number] }
  | { mode: 'valve220'; u_axis: ApmapVec4; v_axis: ApmapVec4; rotation: number; scale: [number, number] }
  | ApmapBrushPrimitivesProjection;

/** The two projections Quake 1 and Quake II know: every tool that authors a face writes one of these. */
export type ApmapQuakeProjection = Exclude<ApmapProjection, ApmapBrushPrimitivesProjection>;

export const isBrushPrimitivesProjection = (projection: { mode?: unknown } | undefined): projection is ApmapBrushPrimitivesProjection =>
  projection?.mode === 'brush_primitives';

export type ApmapFace = {
  face_id: string;
  derived_from?: Record<string, unknown>;
  plane: { points: [ApmapVec3, ApmapVec3, ApmapVec3] };
  texture: string;
  projection: ApmapProjection;
  tail?: number[];
  synthetic?: true;
  synthetic_reason?: string;
  extensions?: Record<string, unknown>;
};

export type ApmapBrushItem = {
  kind: 'brush';
  brush_id: string;
  derived_from?: Record<string, unknown>;
  faces: ApmapFace[];
  /**
   * APMap 1.1. The brush is known not to be a well-formed convex solid, and is kept anyway.
   *
   * Absent means false; there is no third state. **Nothing in this package reads it.** It is not a
   * validation result and it is not a gate: a level with broken brushes in it is ordinary work in
   * progress, so nothing may refuse to load, repair or drop a brush because the flag is set, and
   * `checkBrushConvexity` neither consults it nor writes it. A caller that wants both facts asks
   * for the convexity result and reads this member, and decides for itself what to do with the two.
   */
  broken?: boolean;
  extensions?: Record<string, unknown>;
};
export type ApmapPropertyItem = { kind: 'property'; key: string; value: string };
/**
 * APMap 1.5: a Quake III bezier patch (patchDef2) — a surface described by a control grid, never a
 * solid. `control_points` is `width` columns of `height` `[x, y, z, s, t]` points in patchDef2 text
 * order (SEMANTICS.md §9c).
 *
 * **The editor carries it and never changes it.** A document holding a patch opens under the Quake
 * III surface hold (`frontend/src/state/quake3SurfaceHold.ts`): every edit is refused, so the
 * patch — its grid, its place among its entity's items, its group membership — is saved, published
 * and exported exactly as it was read. Editing arrives with Q3_003.
 */
export type ApmapPatchItem = {
  kind: 'patch';
  patch_id: string;
  derived_from?: Record<string, unknown>;
  texture: string;
  width: number;
  height: number;
  control_points: number[][][];
  tail?: number[];
  extensions?: Record<string, unknown>;
};
export type ApmapContentItem = ApmapPropertyItem | ApmapBrushItem | ApmapPatchItem;

export type ApmapEntity = { entity_id: string; derived_from?: Record<string, unknown>; content: ApmapContentItem[]; extensions?: Record<string, unknown> };

export type ApmapRelationship = { type: string; from: string; to: string; payload?: Record<string, unknown> };

/**
 * One direct member of a group: a discriminated union on `kind`, each arm naming the id field for
 * that object kind. One untyped `object_id` would let a brush id sit under an entity member and
 * fail later, at resolution, instead of at the contract.
 */
export type ApmapGroupMember =
  | { kind: 'entity'; entity_id: string }
  | { kind: 'brush'; brush_id: string }
  | { kind: 'face'; face_id: string }
  /**
   * APMap 1.4: another group, by its `group_id` — how "this group plus those two loose brushes" is
   * written. A group has at most one parent, the graph is acyclic and at most 32 deep, and expanding
   * a group reaches each object once (`apmap/groupGraph.ts`).
   */
  | { kind: 'group'; group_id: string }
  /** APMap 1.5: a Quake III patch, owned by its entity like a brush. */
  | { kind: 'patch'; patch_id: string };

/** The member kinds that name an OBJECT, as opposed to a group. */
export type ApmapObjectMemberKind = 'entity' | 'brush' | 'face' | 'patch';

/**
 * **Where a group came from** — APMap 1.3's addition, and historical origin rather than a binding.
 *
 * `{ kind: 'prefab', prefab_id: X }` means exactly one thing: this group was created by placing
 * prefab X. It does not say the geometry still equals X, that later edits to either side reach the
 * other, or that deleting X invalidates the group. So an ordinary edit preserves it, a whole-group
 * copy carries it under a fresh `group_id`, and a group made any other way simply omits it.
 *
 * `prefab_id` is the prefab LIBRARY's identity, not a map object id. Nothing in the document
 * declares it, it carries no APMap prefix, and `remapGroups` must never remint it — that is what
 * makes a cloned group still remember the same prefab. The screenshot stays in the library, which
 * owns it and can re-render it; a URL copied in here would be a stale second source of truth.
 */
export type ApmapGroupSource = { kind: 'prefab'; prefab_id: string };

/**
 * A named, persistent selection set — APMap 1.2's addition, and map working state.
 *
 * It owns no geometry: every member references an object the document already declares, so
 * deleting a group deletes nothing but the grouping. `name` is a LABEL, never identity — two
 * groups may share one, and nothing may reference a group by it.
 */
export type ApmapGroup = {
  group_id: string; name: string; members: ApmapGroupMember[];
  /** 1.3, optional. Absent for every group a user made by hand — see {@link ApmapGroupSource}. */
  source?: ApmapGroupSource;
};

/**
 * **Who made the document, as the document declares it** — APMap 1.4's `authorship`.
 *
 * Every member has THREE states and they are never collapsed: absent is *unknown* (nothing was
 * recorded — never filled from the signed-in account, the uploader or a guess), `''` is
 * *deliberately blank*, and anything else is the text exactly as supplied. Nothing trims, case-folds
 * or Unicode-normalizes it. `license` is words somebody typed, never permission. An empty object is
 * not a state: "nothing recorded" is spelled by omitting `authorship` (`minProperties: 1`).
 */
export type ApmapAuthorship = { author?: string; copyright_notice?: string; license?: string };

export type ApmapRole = 'full_map' | 'prefab_core' | 'reference_context' | 'cropped_context';

export type ApmapDocument = {
  /** Optional and purely declarative; the validator is given the schema, it never fetches this. */
  $schema?: string;
  apmap_version: string;
  document_id: string;
  revision: number;
  role: ApmapRole;
  game: string;
  map_dialect: string;
  id_policy: 'derived' | 'minted';
  frame: ApmapFrame;
  provenance?: Record<string, unknown>;
  /** 1.4, optional. Absent means nobody recorded who made this — see {@link ApmapAuthorship}. */
  authorship?: ApmapAuthorship;
  entities: ApmapEntity[];
  /**
   * Required of a current writer, empty array included. Optional here only because a document
   * being DECODED may be a legacy one that predates groups; `promoteApmapDocument` supplies the
   * empty array at the decode door, so nothing downstream of that ever sees it absent.
   */
  groups?: ApmapGroup[];
  relationships?: ApmapRelationship[];
  extensions?: Record<string, unknown>;
};

/**
 * ONE WRITER, SEVERAL READERS
 *
 *     READ   the current version + every supported legacy version
 *     WRITE  the current version only
 *
 * Neither line names a version, and the parenthetical that used to — `(today: 1.1)` while the
 * bundle wrote 1.2 — is why: a comment that records the moment goes stale silently, and the
 * only place the answer is true is the directory listing.
 *
 * This kernel names neither. The contract bundle lives in `auto-pigeon-libraries` — one schema file
 * directly under `schema/` whose FILENAME carries the current version, and the supported legacy
 * contracts beside it under `schema/deprecated/`. The backend derives both halves by reading that
 * directory and serves them at `/api/apmap/schema`; whoever hosts this kernel configures it here
 * with what the backend said. So the browser and the backend cannot disagree about which formats
 * they speak — there is one derivation, and everything else is told.
 *
 * Unconfigured is a hard error, not a default. A default would be a second place a version is
 * written down, and the whole point is that there is exactly one.
 *
 * A legacy document does not stay legacy: `decodeApmapDocument` promotes it to the current version
 * at the decode door, so the editor's working state — and therefore every save, export and
 * collaboration payload downstream of it — is current by construction. See `promoteApmapDocument`.
 */
let currentVersion: string | undefined;
let readableVersions: readonly string[] | undefined;

/**
 * Configure the APMap contract. Called once, with what the backend derived.
 *
 * `readable` defaults to just the current version, which is the honest reading of a caller that
 * was told one thing: a build that has not been told it can read 1.0 must not read 1.0.
 */
export function setCurrentApmapVersion(version: string, readable: readonly string[] = [version]): void {
  if (!/^\d+\.\d+$/.test(version)) throw new ApmapDecodeError('apmap_schema_unavailable', `${version} is not an APMap version`);
  for (const candidate of readable)
    if (!/^\d+\.\d+$/.test(candidate)) throw new ApmapDecodeError('apmap_schema_unavailable', `${candidate} is not an APMap version`);
  // The current version is always readable. A build that could write a document it could not read
  // back would be a contract nobody could use, and this is cheaper than asking every caller to
  // remember to include it.
  currentVersion = version;
  readableVersions = readable.includes(version) ? [...readable] : [version, ...readable];
}

/** The version this build WRITES. Never use it to decide what may be READ — `isApmapReadable` does that. */
export function currentApmapVersion(): string {
  if (currentVersion === undefined)
    throw new ApmapDecodeError('apmap_schema_unavailable',
      'The current APMap contract was not configured. Call setCurrentApmapVersion() with the version '
      + 'the backend reports at /api/apmap/schema before reading or writing APMap.');
  return currentVersion;
}

/** Every version this build can read, current first. Throws when nothing configured the contract. */
export function apmapReadableVersions(): readonly string[] {
  currentApmapVersion();
  return readableVersions ?? [];
}

/** Whether a document declaring `version` may be read at all. The version gate's whole question. */
export const isApmapReadable = (version: string): boolean => apmapReadableVersions().includes(version);

/** Test/teardown hook. Nothing in the product calls this. */
export function forgetCurrentApmapVersion(): void { currentVersion = undefined; readableVersions = undefined; }

/**
 * The message an unreadable document earns, in the one place it is worded.
 *
 * It names the whole capability rather than only the current version, because "this build reads 1.0
 * and 1.1" is what tells someone holding a 0.9 file whether their problem is age or a typo.
 */
export const unsupportedApmapMessage = (version: string): string =>
  `APMap ${version} is not a supported APMap version; this build reads `
  + `${[...apmapReadableVersions()].sort().join(', ')} and writes ${currentApmapVersion()}. `
  + 'Migrate the document to a supported format.';

/** The former name. Kept so a caller that means "this document is too old to read" still reads well. */
export const deprecatedApmapMessage = unsupportedApmapMessage;

export class ApmapDecodeError extends Error {
  constructor(readonly code: string, message: string, readonly rule = '') {
    super(message);
    this.name = 'ApmapDecodeError';
  }
}

export const isApmapBrush = (item: ApmapContentItem): item is ApmapBrushItem => item.kind === 'brush';
export const isApmapPatch = (item: ApmapContentItem): item is ApmapPatchItem => item.kind === 'patch';
