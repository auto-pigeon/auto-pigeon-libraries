/**
 * The two facts the APMap schema stage and the build that precompiles it must share — `NEW_267`.
 *
 * Deliberately imports nothing: `frontend/build/apmapValidators.ts` runs inside Vite's config loader
 * and imports this file directly, so the option set and the canonical form have ONE definition for
 * the run-time compile, the build-time standalone code and the digest they are matched by.
 * `schemaValidator.ts` is where they are used.
 */
/**
 * The ONE option set the APMap schema is compiled with, at run time and at build time.
 *
 * `allErrors` so every violation is reported; `verbose` so each error carries the offending `data`,
 * which the wound classification reads; `strict: false` and `allowUnionTypes` because the published
 * schema uses both constructs. Changing any of these changes the validator's answer, which is why
 * the build imports this object rather than restating it.
 */
export const APMAP_AJV_OPTIONS = Object.freeze({ allErrors: true, strict: false, verbose: true, allowUnionTypes: true });

/**
 * A schema as text that does not depend on key order or whitespace: keys sorted at every depth,
 * `JSON.stringify`'s own number and string spelling. The backend re-serialises the schema it read,
 * so the bytes the browser receives are not the file's bytes; this is what both sides agree on.
 */
export function canonicalSchemaText(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalSchemaText).join(',')}]`;
  if (value !== null && typeof value === 'object') {
    const object = value as Record<string, unknown>;
    return `{${Object.keys(object).sort().filter((key) => object[key] !== undefined)
      .map((key) => `${JSON.stringify(key)}:${canonicalSchemaText(object[key])}`).join(',')}}`;
  }
  return JSON.stringify(value);
}
