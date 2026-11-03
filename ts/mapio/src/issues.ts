/**
 * The one issue shape every APMap validation stage speaks, and the bounded collector that gathers
 * them.
 *
 * The loader added in the previous task refused a document at the first thing it found wrong. That
 * is the wrong answer for a review workbench: a document with four faults reported one fault at a
 * time takes four load attempts to understand, and a fatal syntax break early in a file hides every
 * semantic fault after it. So every stage now **collects** and the pipeline runs them all, except
 * where a stage physically cannot run (nothing can be validated against a document that is not
 * JSON).
 *
 * `woundId` names an entry in **AUE's** wound catalogue
 * (`auto-pigeon-extractor/data/apmap_wounds/catalog.yaml`, reproduced per file in the sidecars of
 * this repository's frozen fixture, `testdata/apmap/wounded-corpus/`). It is the shared vocabulary
 * the two repositories use for one fault; it is a *tag*, never a requirement. An issue with no catalog
 * equivalent simply carries none, and a wound with no detector is reported by the regression suite
 * rather than silently passing.
 *
 * `value` is the offending value, passed through the same redaction the diagnostic bus uses, so no
 * absolute path, secret-shaped value or two-megabyte string ever reaches Debug, its export, or a
 * test snapshot.
 */
import { redactValue } from './redact.ts';

export type ApmapIssueStage = 'json-parse' | 'schema' | 'semantic' | 'geometry' | 'generic';
/**
 * `fatal` — no document can be produced. `error` — the document decodes but is defective.
 * `warning` — advisory; the document renders. Severity is about what the loader can still do, not
 * about how alarming the fault is: the catalog's most dangerous class, `sneaky`, mostly lands here.
 */
export type ApmapIssueSeverity = 'fatal' | 'error' | 'warning';

export interface ApmapIssue {
  stage: ApmapIssueStage;
  severity: ApmapIssueSeverity;
  message: string;
  /** JSON Pointer into the document (`/entities/3/content/0/faces/2/texture`), or null. */
  path: string | null;
  value: unknown;
  woundId?: string;
}

/** Bounded, like every other diagnostic surface in Auto-Pigeon. */
export const MAX_APMAP_ISSUES = 200;
/** One repeated fault in a 9 MB document must not crowd out the other four. */
export const MAX_ISSUES_PER_KIND = 5;
const MAX_VALUE_CHARS = 200;

/** JSON Pointer escaping, so a key containing `/` or `~` still addresses one member. */
export const pointer = (parent: string, token: string | number): string =>
  `${parent}/${String(token).replace(/~/g, '~0').replace(/\//g, '~1')}`;

/** The offending value, redacted and truncated. Never the raw member of a wounded document. */
export function issueValue(value: unknown): unknown {
  const safe = redactValue(value);
  if (typeof safe !== 'string' || safe.length <= MAX_VALUE_CHARS) return safe;
  return `${safe.slice(0, MAX_VALUE_CHARS)}…[+${safe.length - MAX_VALUE_CHARS} chars]`;
}

export class ApmapIssues {
  readonly items: ApmapIssue[] = [];
  private readonly counts = new Map<string, number>();
  private droppedTotal = 0;
  private droppedByKind = 0;

  constructor(private readonly limit: number = MAX_APMAP_ISSUES, private readonly perKind: number = MAX_ISSUES_PER_KIND) {}

  /**
   * `kind` groups repetitions of one fault. It defaults to stage + wound id + message, which keeps
   * distinct faults distinct because a message names its path; a stage that walks thousands of
   * objects passes an explicit kind so its thousandth repetition is counted, not stored.
   */
  add(issue: ApmapIssue, kind?: string): void {
    const key = kind ?? `${issue.stage}:${issue.woundId ?? issue.message}`;
    const seen = this.counts.get(key) ?? 0;
    this.counts.set(key, seen + 1);
    if (seen >= this.perKind) { this.droppedByKind += 1; return; }
    if (this.items.length >= this.limit) { this.droppedTotal += 1; return; }
    this.items.push(issue);
  }

  hasFatal(): boolean { return this.items.some((issue) => issue.severity === 'fatal'); }
  /** How many issues were counted but not kept, by either bound. */
  get suppressed(): number { return this.droppedTotal + this.droppedByKind; }
  get truncated(): boolean { return this.suppressed > 0; }
  /** Total occurrences of one kind, including suppressed ones. Used by the regression report. */
  occurrences(kind: string): number { return this.counts.get(kind) ?? 0; }
  /** Every wound id this document was tagged with, in first-seen order. */
  woundIds(): string[] { return [...new Set(this.items.map((issue) => issue.woundId).filter((id): id is string => Boolean(id)))]; }
}

/** Counts for the one summary event a load emits alongside its individual issues. */
export function summarizeApmapIssues(issues: readonly ApmapIssue[]) {
  const byStage: Record<string, number> = {}, bySeverity: Record<string, number> = {};
  for (const issue of issues) {
    byStage[issue.stage] = (byStage[issue.stage] ?? 0) + 1;
    bySeverity[issue.severity] = (bySeverity[issue.severity] ?? 0) + 1;
  }
  return { total: issues.length, by_stage: byStage, by_severity: bySeverity,
           wound_ids: [...new Set(issues.map((issue) => issue.woundId).filter(Boolean))] as string[] };
}
