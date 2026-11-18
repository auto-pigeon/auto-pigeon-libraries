// Types for @auto-pigeon/incident-contract.
//
// Hand-written rather than generated, because the runtime is plain ESM with no build step: this
// package has to be importable by node's test runner, by a Vite bundle and by tooling without any
// of them compiling anything first. The shapes below mirror
// schema/incident-envelope-1.0.schema.json; that file remains the authority, and the tests validate
// against it rather than against these declarations.

export type IncidentSeverity = "warning" | "error" | "fatal";
export type IncidentComponent = "AUP" | "AUB" | "AUC" | "AUE" | "AUG" | "AUT" | "AUCOM";
export type IncidentEnvironment = "development" | "production" | "test";

export interface IncidentEvidence {
  brush_count?: number;
  face_count?: number;
  entity_count?: number;
  triangle_count?: number;
  selection_count?: number;
  http_status?: number;
  http_reason?: string;
  queue_depth?: number;
  worker_attempt?: number;
  client_count?: number;
  retry_after_ms?: number;
  event_loop_lag_ms?: number;
  memory_mb?: number;
  /** Observed WebSocket close code, 1000–4999 inclusive. Since 1.5.0. */
  close_code?: number;
  /** Measured connected duration, 0–86400000 ms inclusive. Since 1.5.0. */
  connected_ms?: number;
  /** Outstanding observed send-buffer bytes at departure, 0–2147483647. Since 1.5.0. */
  bytes_ahead?: number;
  /** One documented observed frame size, 0–2147483647 bytes. Since 1.5.0. */
  frame_bytes?: number;
  /** Measured outstanding ping age, 0–3600000 ms inclusive. Since 1.5.0. */
  unanswered_ms?: number;
}

export interface Incident {
  schema_version: "1.0";
  incident_id: string;
  occurred_at: string;
  severity: IncidentSeverity;
  component: IncidentComponent;
  subsystem?: string;
  code: string;
  operation?: string;
  message: string;
  duration_ms?: number;
  correlation_id?: string;
  release?: string;
  environment?: IncidentEnvironment;
  recoverable: boolean;
  user_action?: string;
  evidence?: IncidentEvidence;
}

export type IncidentDraft = Omit<Incident, "schema_version" | "incident_id" | "occurred_at"> &
  Partial<Pick<Incident, "incident_id" | "occurred_at">>;

export interface IncidentCodeEntry {
  code: string;
  component: IncidentComponent;
  recoverable: boolean;
  summary: string;
}

export interface ValidationError {
  path: string;
  message: string;
}

export interface ValidationResult {
  valid: boolean;
  errors: ValidationError[];
}

export interface SentryEventOptions {
  platform?: string;
  release?: string;
  environment?: IncidentEnvironment;
  /** Sanitised frames from `stackFrames`; anything else is dropped. Since 1.2.0. */
  exception?: { type?: string; frames: SentryFrame[] };
}

export const SCHEMA_VERSION: "1.0";
export const BUG_REPORT_URL: string;
export const envelopeSchema: Record<string, unknown>;
export const codesDocument: { schema_version: string; codes: IncidentCodeEntry[] };
export const redactionDocument: Record<string, unknown>;

export const INCIDENT_CODES: readonly IncidentCodeEntry[];
export function isIncidentCode(code: unknown): boolean;
export function incidentCode(code: string): IncidentCodeEntry | undefined;
export function codesForComponent(component: IncidentComponent): IncidentCodeEntry[];

export const CORRELATION_HEADER: string;
export const CORRELATION_HEADER_LOWER: string;
export const CORRELATION_FIELD: string;
export const CORRELATION_TAG: string;
export function isCorrelationId(value: unknown): boolean;
export function newCorrelationId(): string;
export function correlationIdFromHeaders(
  headers: Headers | Record<string, string | string[] | undefined> | null | undefined,
): string | undefined;
export function correlationHeaders(existing?: string): Record<string, string>;

export function redactText<T>(value: T): T;
export function redactValue<T>(value: T): T;
export function redactIncident(incident: Incident): Incident;
/**
 * The closed evidence object with only schema-declared members, each of the declared kind and in
 * range; anything else is omitted, never coerced. `undefined` for a non-object. Since 1.5.0.
 */
export function redactEvidence(evidence: unknown): IncidentEvidence | undefined;
export function isDroppedKey(key: string): boolean;
export function isDeniedKey(key: string): boolean;

export function validateIncident(incident: unknown): ValidationResult;

export function newIncidentId(): string;
export function newTransportEventId(): string;
export function formatOccurredAt(when?: Date | string | number): string;
export function createIncident(fields: IncidentDraft): Incident;
export function toSentryEvent(incident: Incident, options?: SentryEventOptions): Record<string, unknown>;
export function toDiagnosticText(incident: Incident): string;

// ---- The user bug report (auto-pigeon-bug-report/1.1) -------------------------------------------

export type BugReportComponent = "AUP" | "AUG" | "AUCOM";
export type BugReportType = "bug" | "feature_request";
export type BugReportArea =
  | "editor" | "transform" | "textures" | "import_export" | "prefabs_extract" | "collaboration"
  | "compile_run" | "gallery" | "companion" | "account_access" | "documentation" | "other";
/** The areas each application offers — bug-report-rules.json `applications`, as a type. */
export interface BugReportAreasByComponent {
  AUP: "editor" | "transform" | "textures" | "import_export" | "prefabs_extract" | "collaboration" | "compile_run" | "account_access" | "documentation" | "other";
  AUG: "gallery" | "account_access" | "documentation" | "other";
  AUCOM: "companion" | "compile_run" | "import_export" | "documentation" | "other";
}
export type BugReportAreaOf<C extends BugReportComponent> = BugReportAreasByComponent[C];
export type BugReportApplicationLabel = BugReportComponent;
export type BugReportTypeLabel = "Bug" | "Feature request";
export type BugReportAreaLabel =
  | "Area: Editor" | "Area: Transform" | "Area: Textures" | "Area: Import / Export" | "Area: Prefabs / Extract"
  | "Area: Collaboration" | "Area: Compile / Run" | "Area: Gallery" | "Area: Companion"
  | "Area: Account / Access" | "Area: Documentation" | "Area: Other";
/** Exactly three labels, in this order: application, report type, area. */
export type BugReportLabels = readonly [BugReportApplicationLabel, BugReportTypeLabel, BugReportAreaLabel];
export type BugReportSchemaStatus = "current" | "previous" | "expired" | "unsupported";
export interface BugReportHeadings { summary: string; steps: string; expected: string; actual: string }
export type BugReportRoute = "prefilled" | "server";

export interface BugReportRecent {
  at: string;
  kind: "incident" | "breadcrumb" | "operation";
  name: string;
  duration_ms?: number;
  outcome?: string;
  correlation_id?: string;
}

/** A document of one application: its `area` can only be one that application offers. */
export interface BugReportDocumentOf<C extends BugReportComponent> {
  schema: "auto-pigeon-bug-report/1.1";
  report_id: string;
  created_at: string;
  component: C;
  release: string;
  environment: string;
  kind: "cold" | "incident";
  report_type: BugReportType;
  area: BugReportAreaOf<C>;
  user: { summary: string; steps: string; expected: string; actual: string };
  incident?: {
    incident_id: string;
    code: string;
    severity: IncidentSeverity;
    subsystem?: string;
    operation?: string;
    occurred_at?: string;
    recoverable: boolean;
  };
  correlation_id?: string;
  session_correlation_id?: string;
  client: {
    browser?: string;
    os?: string;
    viewport_width?: number;
    viewport_height?: number;
    pixel_ratio_pct?: number;
    cores?: number;
    memory_mb?: number;
    language?: string;
    webgl2?: boolean;
  };
  recent: BugReportRecent[];
  omitted_recent: number;
}

export type BugReportDocument = BugReportDocumentOf<"AUP"> | BugReportDocumentOf<"AUG"> | BugReportDocumentOf<"AUCOM">;

export interface BugReportInputOf<C extends BugReportComponent> {
  component: C;
  /** Required for a cold report; an incident-triggered report defaults to "bug". */
  reportType?: BugReportType;
  /** Required for a cold report; an incident-triggered report defaults to `suggestBugReportArea`. */
  area?: BugReportAreaOf<C>;
  release?: string;
  environment?: string;
  reportId?: string;
  now?: Date | string | number;
  user: { summary?: string; steps?: string; expected?: string; actual?: string };
  incident?: Partial<Incident>;
  correlationId?: string;
  sessionCorrelationId?: string;
  client?: {
    userAgent?: string;
    viewportWidth?: number;
    viewportHeight?: number;
    pixelRatio?: number;
    cores?: number;
    memoryGb?: number;
    language?: string;
    webgl2?: boolean;
  };
  recent?: Array<Partial<BugReportRecent> & { at?: string | number | Date }>;
}

export type BugReportInput = BugReportInputOf<"AUP"> | BugReportInputOf<"AUG"> | BugReportInputOf<"AUCOM">;

export type BugReportBuild =
  | { ok: true; document: BugReportDocument; prefill: { fits: boolean } }
  | { ok: false; errors: string[] };

export const BUG_REPORT_SCHEMA: "auto-pigeon-bug-report/1.1";
export const BUG_REPORT_REPOSITORY: string;
export const BUG_REPORT_LIMITS: Readonly<Record<string, number>>;
export const BUG_REPORT_TYPES: readonly BugReportType[];
export const BUG_REPORT_AREAS: readonly BugReportArea[];
export const bugReportRules: Record<string, unknown>;
export const bugReportSchema: Record<string, unknown>;
export const bugReportSchemaPrevious: Record<string, unknown>;
export function bugReportAreasFor<C extends BugReportComponent>(component: C): BugReportAreaOf<C>[];
export function bugReportHeadings(reportType: BugReportType): BugReportHeadings;
export function suggestBugReportArea<C extends BugReportComponent>(component: C, incident: Partial<Incident> | undefined): BugReportAreaOf<C> | undefined;
export function bugReportSchemaStatus(schema: unknown, now?: Date | string | number): BugReportSchemaStatus;
/** `null` when the document cannot be classified exactly (never a partial or a fourth label). */
export function bugReportLabels(document: unknown): BugReportLabels | null;
export function sanitizeReportText(value: unknown, max: number, options?: { multiline?: boolean }): string;
export function coarseBrowser(userAgent: unknown): string | undefined;
export function coarseOs(userAgent: unknown): string | undefined;
export function buildBugReport(input: BugReportInput): BugReportBuild;
export function canonicalReportJson(document: BugReportDocument): string;
export function reportJsonDownload(document: BugReportDocument): string;
export function validateBugReport(document: unknown, options?: { acceptPrevious?: boolean }): ValidationResult;
export function renderReportText(document: BugReportDocument): string;
export function renderIssue(document: BugReportDocument, options?: { route?: BugReportRoute }): { title: string; body: string };
export function prefilledIssueUrl(document: BugReportDocument): string;
export function reportDownloadNames(document: BugReportDocument): { text: string; json: string };

// ---- Stack frames for source-map symbolication (since 1.2.0) -----------------------------------

export interface SentryFrame {
  abs_path: string;
  filename: string;
  function: string;
  lineno: number;
  colno: number;
  in_app: true;
}
export function stackFrames(stack: unknown, options: { origin: string }): SentryFrame[];
export function exceptionType(error: unknown): string;
