/**
 * The positional id space, and the one-way bridge off it.
 *
 * Before `20260803_24` the `.map` load path minted `e0`, `e0-b1`, `e0-b1-f2` while the APMap path
 * minted `ent_…`, `brs_…`, `fac_…` for the same objects. Two participants who opened the same map by
 * different routes addressed different id spaces, so locking between them silently did nothing and
 * annotations did not survive a change of load path.
 *
 * The derived id won — it is the shared versioned format's, two independent implementations agree on
 * it, and COL addresses objects by it. Positional ids survive here for exactly two reasons:
 *
 * 1. a scene with no source `.map` identity (a prefab preview) has nothing to derive from;
 * 2. annotations and groups already saved key on them, and a positional id is *self-describing* —
 *    it is the source address, in a different punctuation — so it can be migrated rather than lost.
 */
import { deriveApmapId, sourceMapDerivedFrom, sourcePathFor } from './identity.ts';

/** `e0`, `e0-b1`, `e0-b1-f2`. */
export const positionalObjectId = (entityIndex: number, brushIndex?: number, faceIndex?: number): string =>
  `e${entityIndex}${brushIndex === undefined ? '' : `-b${brushIndex}`}${faceIndex === undefined ? '' : `-f${faceIndex}`}`;

/** Matches a positional id and nothing else. A derived `ent_…` id cannot match it, which is what makes migration idempotent. */
export const POSITIONAL_OBJECT_ID = /^e(\d+)(?:-b(\d+)(?:-f(\d+))?)?$/;

/**
 * The derived id for a legacy positional id, or `undefined` when `id` is not one.
 *
 * `undefined` for an already-derived id is the whole point: running a migration twice must be the
 * same as running it once, and callers rewrite only what this answers for.
 */
export function derivedIdFromPositional(id: string, sourceMap: string | undefined, sourceSha256: string | undefined): string | undefined {
  const match = POSITIONAL_OBJECT_ID.exec(id);
  if (!match) return undefined;
  const [, entity, brush, face] = match;
  const { kind, sourcePath } = sourcePathFor(Number(entity), brush === undefined ? undefined : Number(brush), face === undefined ? undefined : Number(face));
  return deriveApmapId(kind, sourceMapDerivedFrom(sourcePath, sourceMap, sourceSha256));
}
