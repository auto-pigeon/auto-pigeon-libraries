/**
 * The package's structural promises: it imports nothing but itself and `ajv`, names no host framework, and
 * its root entry and module entries are ONE implementation — the same classes and the same contract state.
 */
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import * as root from '../src/index.ts';
import * as types from '../src/apmap/types.ts';
import * as quake3 from '../src/apmap/quake3Encode.ts';
import * as writer from '../src/map/writer.ts';

const SRC = new URL('../src/', import.meta.url).pathname;
const MANIFEST = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));
const sources = (directory: string): string[] => readdirSync(directory, { withFileTypes: true })
  .flatMap((entry) => entry.isDirectory() ? sources(join(directory, entry.name)) : entry.name.endsWith('.ts') ? [join(directory, entry.name)] : []);
const importsOf = (path: string): string[] =>
  [...readFileSync(path, 'utf8').matchAll(/(?:^|\n)\s*(?:import|export)[^'"\n]*from\s*['"]([^'"]+)['"]/g)].map((match) => match[1]);

describe('package boundary', () => {
  const files = sources(SRC);

  it('imports only its own modules (with explicit .ts) and its one declared dependency', () => {
    expect(Object.keys(MANIFEST.dependencies)).toEqual(['ajv']);
    const offenders = files.flatMap((file) => importsOf(file).filter((specifier) => specifier.startsWith('.')
      ? !specifier.endsWith('.ts') || !join(file, '..', specifier).startsWith(SRC)
      : !specifier.startsWith('ajv/')).map((specifier) => `${file}: ${specifier}`));
    expect(offenders).toEqual([]);
  });

  it('names no UI framework, renderer, DOM tree or network', () => {
    const forbidden = /\bfrom\s+['"](?:three|react|react-dom|opentype\.js)['"]|\bwindow\.|\bdocument\.(?:createElement|querySelector|body)\b|\bfetch\(|\bXMLHttpRequest\b|\bWebSocket\b|\blocalStorage\b/;
    expect(files.filter((file) => forbidden.test(readFileSync(file, 'utf8')))).toEqual([]);
  });

  it('is Apache-2.0 and publishes source and build, nothing else', () => {
    expect(MANIFEST.license).toBe('Apache-2.0');
    expect(MANIFEST.files).toEqual(['src', 'dist', 'README.md', 'LICENSE', 'NOTICE']);
  });
});

describe('one implementation behind every entry point', () => {
  it('re-exports the same error classes and functions, so instanceof holds across entries', () => {
    expect(root.ApmapDecodeError).toBe(types.ApmapDecodeError);
    expect(root.Quake3ImportError).toBe(quake3.Quake3ImportError);
    expect(root.MapExportError).toBe(writer.MapExportError);
    expect(root.mapTextFromApmap).toBe(writer.mapTextFromApmap);
  });

  it('shares one contract state', () => {
    expect(types.currentApmapVersion()).toBe(root.currentApmapVersion());
  });
});
