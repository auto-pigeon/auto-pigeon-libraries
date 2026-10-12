/**
 * The canonical workspace manifest, checked against its own schema and its own invariants.
 *
 * It lives in this package's test run for one boring reason: this is the only package in the
 * repository, so it owns the only `npm test` script `run.sh` can reach.
 */
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { PACKAGE_ROOT, compile, currentSchemaFiles, describeErrors, readJson } from './helpers.mjs';

const WORKSPACE_DIR = path.resolve(PACKAGE_ROOT, '../../workspace');
const manifest = readJson(path.join(WORKSPACE_DIR, 'auto-pigeon-workspace.json'));
const schema = readJson(path.join(WORKSPACE_DIR, 'auto-pigeon-workspace.schema.json'));

/**
 * Every repository the workspace is defined to contain. Changing this is a product decision.
 *
 * TEL joined at `TEL-02`, which made `auto-pigeon-telemetry` the canonical home of the third-party
 * observability deployment — the GlitchTip, PostgreSQL and Gatus compose definitions and their
 * image pins. The launcher resolves it as an ordinary sibling checkout, so a workspace cloned from
 * this manifest without it would come up with observability permanently degraded: the manifest is
 * what `clone-auto-pigeon-stack.sh` and `pull-auto-pigeon-stack.sh` work from, and a repository the
 * tooling requires but the list omits is a workspace nobody can reproduce.
 *
 * AUCOM joined at `20260906_120V`, by product decision: `auto-pigeon-companion` is a canonical
 * Auto-Pigeon repository with its own queue, and `auto-pigeon-launcher` (AUL) is being merged into
 * and superseded by it. This list is what a bare whole-workspace drain and every unfiltered
 * topology report mean by "the repositories this project is MADE of" — so AUL stays OUT of it
 * deliberately, while remaining an explicitly addressable local checkout for as long as it is on
 * disk. Membership here is a product decision; a directory
 * merely existing beside the others is not.
 *
 * ERICW joined at `NEW_323F`, by HITL instruction (2026-10-12): `ericw-tools` is the Auto-Pigeon
 * fork the pinned compiler releases are built from, and a prompt may now declare it as its
 * `execution_repository`. The handoff tooling verifies a checkout against this list before it will
 * write a handoff for it, so a fork that is worked in but not named here cannot be checkpointed.
 */
const CANONICAL = {
  AUP: 'auto-pigeon', AUB: 'auto-pigeon-backend', AUC: 'auto-pigeon-collaboration',
  AUCOM: 'auto-pigeon-companion', AUE: 'auto-pigeon-extractor', AUG: 'auto-pigeon-gallery',
  AUT: 'auto-pigeon-tools', AULIBS: 'auto-pigeon-libraries', TEL: 'auto-pigeon-telemetry',
  ERICW: 'ericw-tools',
};

test('the manifest validates against its schema', () => {
  const validate = compile(schema);
  assert.ok(validate(manifest), `the workspace manifest is invalid:\n  ${describeErrors(validate.errors)}`);
});

test('aliases, directories and clone URLs are each unique', () => {
  for (const key of ['alias', 'directory', 'clone_url']) {
    const values = manifest.repositories.map((entry) => entry[key]);
    assert.deepEqual([...new Set(values)].sort(), [...values].sort(), `two repositories share a ${key}`);
  }
});

test('the manifest names exactly the canonical repositories', () => {
  assert.deepEqual(Object.fromEntries(manifest.repositories.map((e) => [e.alias, e.directory])), CANONICAL);
});

/**
 * Order is presentational — the schema says so — but it is also STABLE, and stability is what makes
 * a drain plan, a generated alias table and a `--repos` listing the same on every machine. So the
 * order is asserted rather than left to whoever edits the file next.
 */
test('the manifest keeps its declared order', () => {
  assert.deepEqual(manifest.repositories.map((entry) => entry.alias), Object.keys(CANONICAL));
});

/**
 * The manifest answers "what is this project MADE of", never "what is checked out here". A
 * repository that is being retired (AUL, superseded by AUCOM) is addressable by the agent tooling
 * while its checkout exists, and must not be in the shared topology: adding it
 * would put it in every clone, every pull and every bare drain.
 */
test('a retiring checkout is not canonical topology', () => {
  const directories = new Set(manifest.repositories.map((entry) => entry.directory));
  assert.equal(directories.has('auto-pigeon-launcher'), false,
    'auto-pigeon-launcher is not a canonical workspace repository');
});

test('every clone URL ends in the repository directory name, or in its declared repository_name', () => {
  // A mismatch would clone a repository into a directory named after a different one, which the
  // clone script cannot detect: it derives the destination from `directory`, not from the URL.
  // A deliberate difference is declared, never inferred: `repository_name` names it.
  for (const entry of manifest.repositories)
    assert.equal(entry.clone_url.split('/').pop(), `${entry.repository_name ?? entry.directory}.git`,
      `${entry.alias}: clone_url and ${entry.repository_name ? 'repository_name' : 'directory'} disagree`);
});

test('repository_name is declared only where it differs from directory', () => {
  for (const entry of manifest.repositories)
    if ('repository_name' in entry)
      assert.notEqual(entry.repository_name, entry.directory,
        `${entry.alias}: repository_name repeats directory; omit it`);
});

test('the repositories live in the auto-pigeon GitHub organization, and AUP is auto-pigeon-editor', () => {
  // The repositories moved from a personal namespace to the organization on 2026-09-24. AUP's
  // checkout keeps the directory `auto-pigeon`: queues, handoffs and runners key on it.
  for (const entry of manifest.repositories)
    assert.match(entry.clone_url, /^https:\/\/github\.com\/auto-pigeon\/[a-z0-9-]+\.git$/, entry.alias);
  const aup = manifest.repositories.find((entry) => entry.alias === 'AUP');
  assert.equal(aup.directory, 'auto-pigeon');
  assert.equal(aup.repository_name, 'auto-pigeon-editor');
});

/**
 * The exclusions are the manifest's whole design, so they are asserted rather than left to the
 * schema's `additionalProperties: false` — that stops a new KEY, and this stops a bad VALUE.
 *
 * Only the structural fields are scanned: `description` and `role` are prose, and prose about the
 * rule trips the rule. A leak would be in a URL or a directory, not in a sentence saying there are
 * none.
 */
test('no structural field carries a machine path, data root, port or credential', () => {
  const structural = manifest.repositories
    .flatMap((entry) => [entry.alias, entry.directory, entry.clone_url])
    .concat(manifest.schema_version).join(' ');
  const FORBIDDEN = [
    [/\/home\//, 'an absolute machine path'],
    [/MAPPER_ROOT/, 'the $MAPPER_ROOT data root'],
    [/localhost|127\.0\.0\.1|192\.168\./, 'a host address'],
    [/:\d{4,5}\b/, 'a port number'],
    [/password|secret|token|api[_-]?key/i, 'a credential'],
    [/@/, 'userinfo in a URL'],
  ];
  for (const [pattern, what] of FORBIDDEN)
    assert.equal(pattern.test(structural), false, `a structural field contains ${what}; this manifest is topology only`);
});

test('directories are bare names, never paths', () => {
  for (const entry of manifest.repositories) {
    assert.equal(path.basename(entry.directory), entry.directory, `${entry.alias}: directory is a path`);
    assert.ok(!entry.directory.startsWith('.'), `${entry.alias}: directory is relative`);
  }
});

/**
 * The manifest carries no schema path, and must not grow one: AUT DERIVES the APMap schema
 * directory from the AULIBS entry, and services are handed it explicitly. A path in the manifest
 * would be a second answer to where the contract lives.
 */
test('the manifest names no APMap schema path', () => {
  assert.ok(!JSON.stringify(manifest).includes('apmap-schema'),
    'the manifest names a schema path; AUT derives it from the AULIBS repository entry instead');
  const aulibs = manifest.repositories.find((entry) => entry.alias === 'AULIBS');
  assert.ok(fs.existsSync(path.resolve(PACKAGE_ROOT, '..', '..')), 'AULIBS entry must describe this repository');
  assert.equal(aulibs.directory, 'auto-pigeon-libraries');
  // And the directory AUT derives from it is the one that actually holds the current schema.
  assert.equal(currentSchemaFiles().length, 1);
});
