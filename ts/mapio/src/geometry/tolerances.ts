/**
 * **The two tolerances the codec's brush admission shares with the editor** — `NEW_388`.
 *
 * They were defined beside the editor code that first needed them (`solidFromPlanes.ts`, `faceUv.ts`)
 * and read by `brushLegality.ts`, the admission every imported brush crosses. They live here now so the
 * codec reads no editor module; the editor imports the same constants back, so there is one number.
 */

/**
 * How nearly identical two declared planes must be to count as one.
 *
 * `1e-9` on the normals and `1e-6` on the offset: a generated plane that lands *on* an existing face
 * is arithmetically identical, not merely close, so the tolerance only has to absorb the round-trip
 * through `canonicalApmapNumber`.
 */
export const COINCIDENT_NORMAL = 1 - 1e-9, COINCIDENT_OFFSET = 1e-6;

/**
 * The smallest texture scale this editor will author, in world units per texel.
 *
 * `quakeProjection` refuses `0` outright; everything between is representable but a face at 1e-9
 * units per texel tiles a 128-pixel texture 10¹¹ times across a wall, which is a float32 UV buffer
 * with no precision left in it. The bound is stated rather than felt: `scale.ts`'s
 * `MIN_SCALE_FACTOR` is about *geometry* at the clipper's resolution and is a different number for
 * a different reason, so this one is its own.
 */
export const MIN_FACE_TEXTURE_SCALE = 1e-4;

/** The largest, for the same reason from the other end: past this a texel is wider than any map. */
export const MAX_FACE_TEXTURE_SCALE = 1e6;
