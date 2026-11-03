/**
 * **How an entity's ordered properties change** — the one policy, `Q2_002A`.
 *
 * An entity's properties are an ORDERED list in which a key may repeat and may sit after a brush (APMap §5,
 * `Q2_002`). Every engine and compiler reads them last-wins. So an edit — "speed is 300", "remove message" —
 * says what the last-wins reading must become, and the list has to change as little as that requires:
 *
 * - **an unchanged key** keeps every occurrence, its value and its position;
 * - **a changed key** has its LAST occurrence (the effective one) take the new value; earlier occurrences, which
 *   no reader sees, keep theirs and their positions, so a diff of the document shows one value changing;
 * - **a removed key** loses EVERY occurrence, so an earlier copy cannot surface as the new last one;
 * - **a new key** goes in before the entity's first brush or patch — after its leading properties, where a
 *   `.map` writer puts a key — or at the end of an entity that holds none;
 * - an empty string is a value, never an absence.
 *
 * Brushes, patches and every other content item pass through by reference, in place.
 *
 * Two owners call it: `editApmapEntity` (the editor's property edit, through `object.replace`) and the `.map`
 * encoder's reconciliation of a parsed model whose `properties` was changed after the read
 * (`apmapFromParsedMap`). Before it, the encoder replaced the whole ordered list with `properties` in that case
 * — every key before every brush, every repeat collapsed — and the editor wrote a changed value into every
 * occurrence of a repeated key.
 */

/** Key → new value; `null` removes the key. A key not named is left exactly as it is. */
export type PropertyEdits = Readonly<Record<string, string | null>>;

type PropertyLike = { kind: 'property'; key: string; value: string };
type ContentLike = { kind: string };

const isPropertyItem = (item: ContentLike): item is PropertyLike => item.kind === 'property';

/**
 * **An empty dictionary of `.map` keys** — `Q2_005A`. No prototype, so a key spelled `__proto__`, `constructor` or
 * `toString` is an own property like any other: assigning it never changes a prototype, and reading a key the entity
 * does not carry answers `undefined`, never an inherited function. Every dictionary keyed by an arbitrary map key starts
 * here; read it with own-property checks (`Object.prototype.hasOwnProperty.call`, `Object.keys`, `Object.entries`).
 */
export const propertyRecord = <T = string>(): Record<string, T> => Object.create(null) as Record<string, T>;

/** The last-wins reading of an ordered list: what every engine sees. */
export function lastWinsProperties(items: readonly { key: string; value: string }[]): Record<string, string> {
  const reading = propertyRecord();
  for (const item of items) reading[item.key] = item.value;
  return reading;
}

/**
 * **Each key once, in the order of its LAST occurrence** — `Q2_005A`.
 *
 * The position a last-wins reading loses. A game that applies an entity's keys in map order (Quake II, Quake III)
 * answers "which of `angle` and `angles` came later" by where each one's last line sits, and a dictionary cannot say
 * that: assigning a repeated key again updates its value but keeps its FIRST insertion position. Derived from the
 * ordered list every time, never stored beside it, so it cannot go stale. Linear in the list.
 */
export function lastOccurrenceOrder(items: readonly { key: string }[]): string[] {
  const last = new Map<string, number>();
  items.forEach((item, index) => last.set(item.key, index));
  return [...last.entries()].sort((a, b) => a[1] - b[1]).map(([key]) => key);
}

/** Where a new key goes: before the first non-property item, or at the end. */
export function newPropertyIndex(content: readonly ContentLike[]): number {
  const first = content.findIndex((item) => !isPropertyItem(item));
  return first === -1 ? content.length : first;
}

/**
 * `content` with `edits` applied by the policy above, and whether anything changed. The input is never mutated;
 * an unchanged list comes back as the same array.
 */
export function applyPropertyEdits<T extends ContentLike>(content: readonly T[], edits: PropertyEdits,
  create: (key: string, value: string) => T): { content: readonly T[]; changed: boolean } {
  const last = new Map<string, number>();
  content.forEach((item, index) => { if (isPropertyItem(item)) last.set(item.key, index); });
  let changed = false;
  const next: T[] = [];
  content.forEach((item, index) => {
    if (!isPropertyItem(item) || !Object.prototype.hasOwnProperty.call(edits, item.key)) { next.push(item); return; }
    const value = edits[item.key];
    if (value === null) { changed = true; return; }
    if (last.get(item.key) !== index || value === item.value) { next.push(item); return; }
    next.push({ ...item, value } as T);
    changed = true;
  });
  const added = Object.entries(edits).filter(([key, value]) => value !== null && !last.has(key));
  if (added.length) {
    next.splice(newPropertyIndex(next), 0, ...added.map(([key, value]) => create(key, value as string)));
    changed = true;
  }
  return changed ? { content: next, changed } : { content, changed };
}

/** The edits that turn the last-wins reading `from` into `to`: changed and new keys set, missing keys removed. */
export function propertyEditsBetween(from: Readonly<Record<string, string>>, to: Readonly<Record<string, string>>): PropertyEdits {
  const edits = propertyRecord<string | null>();
  for (const [key, value] of Object.entries(to)) if (!Object.prototype.hasOwnProperty.call(from, key) || from[key] !== value) edits[key] = value;
  for (const key of Object.keys(from)) if (!Object.prototype.hasOwnProperty.call(to, key)) edits[key] = null;
  return edits;
}
