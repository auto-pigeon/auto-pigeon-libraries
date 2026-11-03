/**
 * How a document says which `.map` dialect its faces are projected in — and the one place AUB's
 * profile vocabulary meets APMap's own (`AUP 86` §4).
 *
 * ### Two registers, one meeting point
 *
 * APMap has always recorded the **face-projection** dialect: `apmapFromParsedMap` writes
 * `valve_220` when any parsed face carries Valve 220 axes and `quake_standard` otherwise, and
 * nothing in the kernel has ever branched on the value — it is a statement about the document's own
 * faces.
 *
 * A Game Profile answers a different question: *which dialects may this project author in?* AUB's
 * family table is the authority (`quake1 → ["quake1","valve220"]`, `quake2 → ["quake2"]`,
 * `quake3 → ["quake3"]`), served at `GET /api/game-profiles/capabilities`, and its words are family
 * names rather than projection forms. Only `valve220` changes how a face is projected; the rest are
 * the classic shift/rotation/scale form, which is also exactly what AUP's importer writes for a
 * `.map` of those games whose faces carry no Valve axes.
 *
 * So the translation is one function and it lives here, in a module with no imports, so that both
 * the document writer and the geometry that authors new faces read the same answer.
 */

/** Classic Quake projection: shift, rotation, scale. What every non-Valve dialect means. */
export const APMAP_DIALECT_CLASSIC = 'quake_standard';
/** Valve 220: explicit u/v axes on every face. */
export const APMAP_DIALECT_VALVE220 = 'valve_220';

/**
 * The APMap dialect a Game Profile's dialect id means.
 *
 * Punctuation and case are folded before comparing, so `valve220`, `valve_220` and `Valve220` are
 * one answer: a profile is a user-editable document and the vocabulary it declares is checked by
 * AUB against a family table, not by spelling here.
 */
export const apmapDialectFor = (profileDialect: string): string =>
  (String(profileDialect ?? '').toLowerCase().replace(/[^a-z0-9]/g, '') === 'valve220'
    ? APMAP_DIALECT_VALVE220
    : APMAP_DIALECT_CLASSIC);

/**
 * Whether a document authors its faces in Valve 220.
 *
 * Asked of the **document**, never carried as a parameter beside it: the document is the authority
 * on its own format, and a second copy of that fact travelling through a call chain is how a map
 * ends up declaring one dialect while its newest faces are written in the other.
 */
export const isValve220Document = (document: { map_dialect?: string } | undefined): boolean =>
  document?.map_dialect === APMAP_DIALECT_VALVE220;
