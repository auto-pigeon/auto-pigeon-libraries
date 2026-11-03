/**
 * Recursive redaction for everything that reaches the diagnostic bus, the Debug console,
 * copy and export. Absolute paths and secret-shaped values are removed; numeric counts and
 * durations are preserved because they are the point of the telemetry.
 */
const PATH = /(?:file:\/\/)?(?:[A-Za-z]:)?[\\/](?:[\w.@-]+[\\/])+[\w.@-]*/g;
const SECRET_KEY = /(token|secret|password|passwd|api[_-]?key|authorization|auth|cookie|credential|bearer)/i;
const SECRET_VALUE = /\b(?:sk|pk|ghp|gho|xox[abp])[-_][A-Za-z0-9_-]{12,}\b|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b/g;
const MAX_STRING = 500, MAX_ARRAY = 32, MAX_KEYS = 64, MAX_DEPTH = 5;
/**
 * A JSON Pointer is not a filesystem path, but `/entities/0/plane` is indistinguishable from one to
 * `PATH` above — and redacting it destroys the only thing that makes a validation issue actionable.
 * One key is therefore exempt, and only for a value that is structurally a pointer: every token of
 * a pointer this codebase emits is RFC 6901 escaped, so an absolute path smuggled in as an object
 * key arrives as `~1home~1secret` and carries no separator of its own.
 */
const POINTER_KEY = 'json_pointer';
const POINTER = /^(?:\/(?:[^/~]|~[01])*)*$/;
/**
 * The same exemption, for the same reason, for one more key.
 *
 * An `http(s)` URL a diagnostic *is about* — a shareable collaboration-session address — has a path
 * component that `PATH` cannot tell from `/home/someone/secret`, and redacting it leaves an event
 * whose entire content was the address. The exemption is as narrow as the pointer one: this key
 * only, and only for a value that is structurally an absolute HTTP URL and nothing else. A
 * filesystem path does not match, because it has no scheme and no authority.
 */
const URL_KEY = 'session_url';
const HTTP_URL = /^https?:\/\/[^\s/]+(?:\/[^\s]*)?$/;

export const redactString = (value: string): string =>
  value.replace(PATH, '[path]').replace(SECRET_VALUE, '[redacted]').slice(0, MAX_STRING);

export function redactValue(value: unknown, depth = 0): unknown {
  if (value === null || value === undefined) return value;
  if (typeof value === 'number' || typeof value === 'boolean') return value;
  if (typeof value === 'string') return redactString(value);
  if (typeof value === 'bigint') return String(value);
  if (typeof value === 'function' || typeof value === 'symbol') return `[${typeof value}]`;
  if (value instanceof Error) return redactString(`${value.name}: ${value.message}`);
  if (depth >= MAX_DEPTH) return '[depth limit]';
  if (Array.isArray(value)) {
    const items = value.slice(0, MAX_ARRAY).map((item) => redactValue(item, depth + 1));
    return value.length > MAX_ARRAY ? [...items, `[+${value.length - MAX_ARRAY} more]`] : items;
  }
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>).slice(0, MAX_KEYS);
    const result: Record<string, unknown> = {};
    for (const [key, item] of entries)
      result[key] = SECRET_KEY.test(key) ? '[redacted]'
        : key === POINTER_KEY && typeof item === 'string' && POINTER.test(item) ? item.slice(0, MAX_STRING)
        : key === URL_KEY && typeof item === 'string' && HTTP_URL.test(item) ? item.slice(0, MAX_STRING)
        : redactValue(item, depth + 1);
    if (Object.keys(value as object).length > MAX_KEYS) result['[truncated]'] = true;
    return result;
  }
  return redactString(String(value));
}

export const redactContext = (context: Record<string, unknown> | undefined): Record<string, unknown> | undefined =>
  context ? (redactValue(context) as Record<string, unknown>) : undefined;
