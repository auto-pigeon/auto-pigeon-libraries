/**
 * The APMap contract for these tests, derived from `@auto-pigeon/apmap-schema` exactly as every consumer
 * derives it: the one schema directly under `schema/` is CURRENT, each one under `schema/deprecated/` is a
 * readable legacy contract. Nothing here names a version.
 */
import { readFileSync, readdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { setCurrentApmapVersion } from '../../src/index.ts';

const require = createRequire(import.meta.url);
export const SCHEMA_DIR = dirname(require.resolve('@auto-pigeon/apmap-schema'));
export const PACKAGE_DIR = dirname(SCHEMA_DIR);
const NAME = /^apmap-(\d+\.\d+)\.schema\.json$/;
const current = readdirSync(SCHEMA_DIR).filter((name) => NAME.test(name));
if (current.length !== 1) throw new Error(`expected one current APMap schema in ${SCHEMA_DIR}, found ${current.length}`);
const legacy = readdirSync(join(SCHEMA_DIR, 'deprecated')).filter((name) => NAME.test(name));

export const CURRENT_APMAP_VERSION = NAME.exec(current[0])![1];
export const READABLE_SCHEMAS: Record<string, unknown> = Object.fromEntries(
  [...current.map((name) => join(SCHEMA_DIR, name)), ...legacy.map((name) => join(SCHEMA_DIR, 'deprecated', name))]
    .map((path) => [NAME.exec(path.split('/').pop()!)![1], JSON.parse(readFileSync(path, 'utf8'))]));
export const CURRENT_SCHEMA = READABLE_SCHEMAS[CURRENT_APMAP_VERSION];
export const LEGACY_VERSIONS = Object.keys(READABLE_SCHEMAS).filter((version) => version !== CURRENT_APMAP_VERSION);

setCurrentApmapVersion(CURRENT_APMAP_VERSION, Object.keys(READABLE_SCHEMAS));

const FIXTURES = new URL('../fixtures/', import.meta.url);
export const fixture = (name: string): string => readFileSync(new URL(name, FIXTURES), 'utf8');
export const vector = (relative: string): string => readFileSync(join(PACKAGE_DIR, relative), 'utf8');
