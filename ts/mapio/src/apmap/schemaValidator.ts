/**
 * **Where the APMap schema stage gets its validator** — `NEW_267`.
 *
 * `ajv` turns a JSON Schema into JavaScript and runs it through `new Function`. That is fine in
 * Node and forbidden in the editor: AUP's Content-Security-Policy is `script-src 'self'
 * 'wasm-unsafe-eval'`, deliberately without `unsafe-eval`, so in a browser the compile throws. Until
 * `NEW_267` the kernel caught that and recorded a *warning* that the stage had not run, which meant
 * no APMap document was ever schema-validated in the editor.
 *
 * The code `ajv` would have generated at run time is generated at BUILD time instead, by the same
 * `ajv`, with the same options, from the same schema files the backend serves
 * (`frontend/build/apmapValidators.ts`), and shipped as one same-origin static module. So there are
 * two ways to obtain a validator, and they produce the same function:
 *
 * - **compiled** — `new Ajv2020(APMAP_AJV_OPTIONS).compile(schema)`. Node, the tests, the CLIs.
 * - **precompiled** — the build's standalone code for a schema whose digest matches. The browser.
 *
 * A host that has precompiled validators CONFIGURES them here, and from then on this kernel never
 * compiles: a schema with no matching validator is refused, not compiled, because in that host a
 * compile can only fail. The match is by the sha256 of the schema's canonical text, never by version
 * label alone: the backend is the authority on what the contract IS, and a build carrying the 1.4
 * validator of a different 1.4 file must not be allowed to pass a document against the wrong one.
 *
 * Nothing here is a second validator. There is no interpretation of a schema in this file and there
 * must never be; both paths run code `ajv` generated.
 */
import type { ValidateFunction } from 'ajv/dist/2020.js';
import { sha256Hex } from '../hash/sha256.ts';
import { APMAP_AJV_OPTIONS, canonicalSchemaText } from './schemaCanonical.ts';

export { APMAP_AJV_OPTIONS, canonicalSchemaText };

/** The identity a precompiled validator is matched by: sha256 of `canonicalSchemaText`. */
export const apmapSchemaDigest = (schema: unknown): string => sha256Hex(canonicalSchemaText(schema));

export type PrecompiledApmapValidator = {
  /** The contract version the schema FILE carries, for messages; matching is by digest. */
  version: string;
  schemaSha256: string;
  validate: ValidateFunction;
};

type Configured =
  | { kind: 'precompiled'; byDigest: ReadonlyMap<string, PrecompiledApmapValidator>; origin: string }
  | { kind: 'unavailable'; reason: string };

let configured: Configured | undefined;
let generation = 0;

/** Bumped by every (re)configuration, so a validator cached under the previous answer is not reused. */
export const apmapValidatorGeneration = (): number => generation;

/**
 * Tell the kernel this host validates with precompiled validators only.
 *
 * `{ unavailable }` is the answer for a host whose validator bundle could not be loaded: every APMap
 * document is then refused at the schema stage with that reason — a document that could not be
 * checked is never described as one that passed.
 */
export function configureApmapSchemaValidators(
  input: { validators: readonly PrecompiledApmapValidator[]; origin?: string } | { unavailable: string },
): void {
  generation += 1;
  configured = 'unavailable' in input
    ? { kind: 'unavailable', reason: input.unavailable }
    : { kind: 'precompiled', origin: input.origin ?? 'precompiled',
        byDigest: new Map(input.validators.map((entry) => [entry.schemaSha256, entry])) };
}

/** Back to compiling on demand. Only tests need this. */
export const forgetApmapSchemaValidators = (): void => { configured = undefined; generation += 1; };

/** What the schema stage should use for `schema`: a validator, a refusal reason, or "compile it". */
export function precompiledValidatorFor(schema: unknown, version: string | undefined):
  { validate: ValidateFunction } | { refusal: string } | undefined {
  if (!configured) return undefined;
  if (configured.kind === 'unavailable')
    return { refusal: `The APMap validator could not be loaded, so this document cannot be checked against its contract and is refused: ${configured.reason}` };
  const digest = apmapSchemaDigest(schema);
  const found = configured.byDigest.get(digest);
  if (found) return { validate: found.validate };
  const known = [...configured.byDigest.values()].map((entry) => `${entry.version} (${entry.schemaSha256.slice(0, 12)})`).join(', ');
  return { refusal: `This editor build carries no validator for the APMap ${version ?? ''} schema the backend serves (sha256 ${digest.slice(0, 12)}); `
    + `it carries ${known || 'none'}. The editor and the backend were built from different contracts, so this document cannot be checked and is refused.` };
}
