/**
 * **Repairing the identity provenance a pre-`AUP 120G` writer left behind** — `AUP 120J`.
 *
 * `AUP 120G` repaired every *current* minting operation: an object that mints an id now carries an
 * `operation` record instead of the ancestor's, so a map edited today satisfies SEM-8 and SEM-10 by
 * construction. It deliberately did not touch documents already saved, and this is the other half:
 * a **pure, deterministic** analysis of what a legacy writer left, and a repair of exactly that.
 *
 * ## What is repaired, and what is emphatically not
 *
 * Nothing here changes geometry, texture, projection, entity properties, relationships, groups,
 * extensions, `document_id`, or **any object id**. The only member this module writes is
 * `derived_from`, and it writes it only on an object whose provenance is already *false or absent*.
 * An id is never moved, because moving one would dangle every group member, relationship and
 * journal row that names it — and because a repair that rewrote ids would be a different document,
 * not the same one made honest.
 *
 * ## Why the replacement record says so little
 *
 * The lost fact is *which operation produced this object*, and it is lost: the legacy writer either
 * wrote nothing or wrote the **ancestor's** record, which names the object the operation consumed
 * rather than the one it produced. Reconstructing it is not possible and guessing it would be worse
 * than the absence — a provenance record nobody can check is exactly the thing SEM-10 exists to
 * make checkable. So the record written here says the one true thing that is left:
 *
 * ```json
 * { "kind": "operation", "operation_id": "identity_repair_…", "operation": "identity_metadata_repair",
 *   "ordinal": 7, "intent": "…", "metadata": { "rule": "sem10", "replaced": "authored" } }
 * ```
 *
 * `kind: "operation"` is what makes it legal *and* free: §3.5 mandates no derivation for it, so
 * SEM-8 is satisfied, SEM-10 does not apply, and the object's id does not move. `replaces` is
 * **absent**, deliberately — it names the objects this one superseded, and this repair does not
 * know them. `metadata` carries the audit: which rule the object broke and which record class was
 * replaced, and nothing else. No map contents, no file names, no paths.
 *
 * ## The supported signatures are the ones actually demonstrated
 *
 * Six damaged account maps were surveyed before a line of this was written (`AUP 120J`'s handoff
 * carries the table). What is in the wild is:
 *
 * - a **brush or face** with **no `derived_from` at all** under `id_policy: "derived"` — SEM-8;
 * - a **brush or face** whose `derived_from` is a well-formed `authored` or `source_map` record
 *   whose digest is not the object's id — SEM-10, the ancestor's record beside a minted id.
 *
 * **No entity is damaged in any of them**, and an entity id is the most-referenced identity a
 * document has, so an entity violation is *not* repaired: it is the refusal that keeps this narrow.
 * So is a violation on an object whose id is not shaped like one this product mints
 * ({@link MINTED_ID}) — that is somebody else's identity scheme and this module has no business
 * asserting anything about it.
 *
 * ## Agreement with AUE, which is the consumer that refuses
 *
 * A violation is decided **exactly** the way `internal/apmap/semantics.go`'s `identityProblems`
 * decides it, through {@link mandatedApmapId} — which is AUE's `MandatedID`: a record that mandates
 * nothing, and a record too malformed to derive from, both answer `""` and are therefore **not**
 * violations. That matters in a way that is easy to get backwards: `sourceMapDerivedFrom` omits
 * `source_map` and `source_sha256` when the caller has neither, and `apmapIdentityComponents`
 * digests an absent component as `''` — so AUP would compute an id for that record while AUE
 * computes nothing. Reading such an object as damaged would refuse whole documents that AUE accepts.
 * The rule is AUE's, and this module implements AUE's.
 *
 * ## Determinism
 *
 * The operation id is a digest of the contract tag, the document id and the ordered list of what is
 * being repaired. No clock, no counter, no randomness: two clients repairing the same document emit
 * the same bytes, and the id changes if — and only if — the set of repaired objects does.
 *
 * Running the repair twice is byte-identical to running it once, and it is idempotent by
 * construction rather than by comparison: every object the first pass touched now carries an
 * `operation` record, which mandates nothing, so the second pass finds it clean.
 */
import { sha256Hex } from '../hash/sha256.ts';
import { APMAP_OPERATION, apmapIdentityComponents, deriveApmapId, type ApmapObjectKind } from './identity.ts';
import { isApmapBrush, type ApmapBrushItem, type ApmapDocument, type ApmapEntity, type ApmapFace } from './types.ts';

const UNIT_SEPARATOR = String.fromCharCode(0x1f);

/** The contract tag in the operation id's digest. A later repair generation bumps it and is distinguishable. */
export const IDENTITY_REPAIR_CONTRACT = 'auto-pigeon/apmap-identity-repair/1';

/**
 * The registry name every repaired record wears.
 *
 * A `registry_name` (`^[a-z][a-z0-9_]{0,63}$`), and it names what actually happened: metadata was
 * repaired. It deliberately does **not** name a geometry operation — the object was not produced by
 * this repair, and claiming `split_brush` or `bend` would be inventing the fact that was lost.
 */
export const APMAP_IDENTITY_REPAIR_OPERATION: string = APMAP_OPERATION.identityRepair;

/**
 * The one sentence written into every repaired record's `intent`.
 *
 * A module constant rather than a per-object string: every record shares this reference, so a
 * 1 500-object repair costs one string in memory and compresses to nothing on the wire.
 *
 * It is English and not an i18n key on purpose. `intent` is *document content* — it travels with the
 * map to AUE, to another account and to a reader with a text editor — and a document that carried a
 * translation key would say nothing at all to any of them.
 */
export const IDENTITY_REPAIR_INTENT =
  'Identity metadata repaired by Auto-Pigeon. The operation that produced this object was not recorded and is not known.';

/** Which rule the object broke. */
export type IdentityRepairRule = 'sem8' | 'sem10';

/**
 * Which record class was replaced. `none` is SEM-8 — there was nothing there, which is itself the
 * fact worth recording, because it distinguishes a writer that dropped the record from one that
 * kept a stale one.
 */
export type IdentityRepairReplaced = 'none' | 'authored' | 'source_map' | 'package';

/** Why an object's identity damage is refused rather than repaired. */
export type IdentityRepairRefusal =
  /** An entity. Never demonstrated, and an entity id is the document's most-referenced identity. */
  | 'entity_identity'
  /** An id this product does not mint, so nothing here may assert what its provenance should be. */
  | 'unrecognised_id';

export type IdentityRepairSubject = {
  kind: ApmapObjectKind;
  id: string;
  /** `entities[0].content[5].faces[0]` — AUE's own address shape, so a reader can match its message. */
  where: string;
  rule: IdentityRepairRule;
  replaced: IdentityRepairReplaced;
};

export type IdentityRepairRejection = { kind: ApmapObjectKind; id: string; where: string; reason: IdentityRepairRefusal };

/** How many rejections an analysis carries. A bound, because this is rendered and reported. */
export const IDENTITY_REPAIR_MAX_REJECTIONS = 20;

export type IdentityRepairCounts = {
  total: number;
  brushes: number;
  faces: number;
  /** By the rule broken. */
  sem8: number;
  sem10: number;
  /** By the record class replaced. */
  missingRecord: number;
  authored: number;
  sourceMap: number;
  package: number;
};

export type IdentityRepairAnalysis = {
  /**
   * `clean` — nothing violates, so nothing is offered. `repairable` — every violation matches a
   * demonstrated signature. `unsupported` — at least one does not, and then **nothing** is repaired:
   * a document is repaired whole or not at all, because a half-repaired document is one nobody can
   * describe.
   */
  status: 'clean' | 'repairable' | 'unsupported';
  /** `''` unless `status` is `repairable`. */
  operationId: string;
  /** Identity-bearing objects examined — entities, brushes and faces carrying an id. */
  examined: number;
  counts: IdentityRepairCounts;
  /** Bounded by {@link IDENTITY_REPAIR_MAX_REJECTIONS}; `rejected` is the true total. */
  rejections: readonly IdentityRepairRejection[];
  rejected: number;
};

export type IdentityRepairOutcome =
  | { outcome: 'repaired'; document: ApmapDocument; analysis: IdentityRepairAnalysis }
  | { outcome: 'clean' | 'unsupported'; analysis: IdentityRepairAnalysis };

/**
 * An id shaped like one {@link mintApmapId} or {@link deriveApmapId} produced: the kind's prefix and
 * sixteen lowercase hex characters.
 *
 * The schema's own pattern is `^brs_[0-9A-Za-z]{8,64}$`, which is far wider on purpose — another
 * producer may mint ids any way it likes. This narrower shape is the discriminator between *an
 * object this product minted and then failed to describe* and *an object from an identity scheme
 * this repair knows nothing about*. The second is refused.
 */
const MINTED_ID: Record<ApmapObjectKind, RegExp> = {
  entity: /^ent_[0-9a-f]{16}$/,
  brush: /^brs_[0-9a-f]{16}$/,
  face: /^fac_[0-9a-f]{16}$/,
  patch: /^pat_[0-9a-f]{16}$/,
};

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

/**
 * The id a `derived_from` record **mandates**, or `''` when it mandates none — AUE's `MandatedID`,
 * to the letter.
 *
 * `''` covers three cases that AUE treats alike and this must too: a record that is not an object; a
 * kind the format gives no derivation (`synthetic`, `operation`, and anything a later version adds);
 * and a record of a deriving kind that is **missing or mistyping** one of its identity inputs, which
 * is AUE's `stringComponents` returning an error and `MandatedID` swallowing it.
 *
 * The integer rule for `authored.ordinal` is AUE's too: Python's `int()`, so a non-integer is not an
 * ordinal at all and the record mandates nothing rather than mandating something AUP alone can
 * compute.
 */
export function mandatedApmapId(kind: ApmapObjectKind, record: unknown): string {
  if (!isObject(record)) return '';
  const present = (...names: string[]) => names.every((name) => typeof record[name] === 'string');
  switch (record.kind) {
    case 'package':
      if (!present('package_id', 'role', 'object_path')) return '';
      break;
    case 'source_map':
      if (!present('source_map', 'source_sha256', 'source_path')) return '';
      break;
    case 'authored':
      if (!present('document_id', 'operation_id')) return '';
      if (!Number.isInteger(record.ordinal)) return '';
      break;
    default:
      return '';
  }
  const components = apmapIdentityComponents(record as never);
  if (!components) return '';
  return deriveApmapId(kind, record as never);
}

type Examined = { kind: ApmapObjectKind; id: string; where: string; record: unknown; present: boolean };

/** Every identity-bearing object, in document order — AUE's `identifiedObjects`, with its addresses. */
function* identified(document: ApmapDocument): Generator<Examined> {
  for (let e = 0; e < document.entities.length; e += 1) {
    const entity = document.entities[e];
    const at = `entities[${e}]`;
    if (typeof entity.entity_id === 'string')
      yield { kind: 'entity', id: entity.entity_id, where: at, record: entity.derived_from, present: 'derived_from' in entity };
    for (let c = 0; c < entity.content.length; c += 1) {
      const item = entity.content[c];
      if (!isApmapBrush(item)) continue;
      const brushAt = `${at}.content[${c}]`;
      if (typeof item.brush_id === 'string')
        yield { kind: 'brush', id: item.brush_id, where: brushAt, record: item.derived_from, present: 'derived_from' in item };
      for (let f = 0; f < item.faces.length; f += 1) {
        const face = item.faces[f];
        if (typeof face.face_id !== 'string') continue;
        yield { kind: 'face', id: face.face_id, where: `${brushAt}.faces[${f}]`, record: face.derived_from, present: 'derived_from' in face };
      }
    }
  }
}

const replacedClass = (record: unknown): IdentityRepairReplaced => {
  if (!isObject(record)) return 'none';
  switch (record.kind) {
    case 'authored': return 'authored';
    case 'source_map': return 'source_map';
    case 'package': return 'package';
    default: return 'none';
  }
};

const emptyCounts = (): IdentityRepairCounts => ({
  total: 0, brushes: 0, faces: 0, sem8: 0, sem10: 0,
  missingRecord: 0, authored: 0, sourceMap: 0, package: 0,
});

/**
 * **What is wrong with this document's identity provenance, and whether this module may fix it.**
 *
 * Pure: it reads the document and returns a description. Nothing is written, nothing is fetched, and
 * the same document always produces the same answer.
 *
 * A document whose `id_policy` is not `derived` is `clean` — SEM-8 and SEM-10 are conditional on
 * that claim, and a document that never made it is not violating anything.
 */
export function analyseApmapIdentity(document: ApmapDocument): IdentityRepairAnalysis {
  return scanApmapIdentity(document).analysis;
}

/**
 * The one walk both public functions make.
 *
 * `subjects` is keyed by `kind:where`, which is unique by construction, and is what the repairer
 * matches against as it rebuilds — so the set of objects analysed and the set repaired cannot drift
 * apart into two implementations of one rule.
 */
function scanApmapIdentity(document: ApmapDocument): { analysis: IdentityRepairAnalysis; subjects: Map<string, IdentityRepairSubject> } {
  const counts = emptyCounts();
  const order: IdentityRepairSubject[] = [];
  const subjects = new Map<string, IdentityRepairSubject>();
  const rejections: IdentityRepairRejection[] = [];
  let rejected = 0;
  let examined = 0;

  if (document.id_policy !== 'derived')
    return { analysis: { status: 'clean', operationId: '', examined: 0, counts, rejections: [], rejected: 0 }, subjects };

  for (const object of identified(document)) {
    examined += 1;
    // `derived_from: null` is treated as absent. AUE's `optionalMember` and this disagree about
    // nothing that matters — either way the object has no usable record, and writing one satisfies
    // both readings.
    const missing = !object.present || object.record === null || object.record === undefined;
    const mandated = missing ? '' : mandatedApmapId(object.kind, object.record);
    if (!missing && (mandated === '' || mandated === object.id)) continue;

    const rejection = (reason: IdentityRepairRefusal) => {
      rejected += 1;
      if (rejections.length < IDENTITY_REPAIR_MAX_REJECTIONS)
        rejections.push({ kind: object.kind, id: object.id, where: object.where, reason });
    };
    if (object.kind === 'entity') { rejection('entity_identity'); continue; }
    if (!MINTED_ID[object.kind].test(object.id)) { rejection('unrecognised_id'); continue; }

    const replaced = missing ? 'none' : replacedClass(object.record);
    const subject: IdentityRepairSubject = {
      kind: object.kind, id: object.id, where: object.where, rule: missing ? 'sem8' : 'sem10', replaced,
    };
    order.push(subject);
    subjects.set(`${object.kind}:${object.where}`, subject);
    counts.total += 1;
    if (object.kind === 'brush') counts.brushes += 1; else counts.faces += 1;
    if (missing) { counts.sem8 += 1; counts.missingRecord += 1; }
    else {
      counts.sem10 += 1;
      if (replaced === 'authored') counts.authored += 1;
      else if (replaced === 'source_map') counts.sourceMap += 1;
      else if (replaced === 'package') counts.package += 1;
    }
  }

  // **A document is repaired whole or not at all.** One violation this module cannot describe means
  // the whole document is refused, because a half-repaired document is one nobody can characterise
  // afterwards — and because a repair offered on damage it does not understand is exactly the
  // silent rewrite this task was told not to perform.
  //
  // The counts are ZEROED here on purpose. `counts` answers *what will be repaired*, and on an
  // unsupported document the answer is nothing — reporting the objects that happened to match
  // before the refusal was reached would put a number on a surface that is about to say the repair
  // is not available, which is the one place a number must not be aspirational.
  if (rejected)
    return { analysis: { status: 'unsupported', operationId: '', examined, counts: emptyCounts(), rejections, rejected },
             subjects: new Map() };
  if (!order.length)
    return { analysis: { status: 'clean', operationId: '', examined, counts, rejections: [], rejected: 0 }, subjects };
  return {
    analysis: { status: 'repairable', operationId: repairOperationId(document, order), examined, counts, rejections: [], rejected: 0 },
    subjects,
  };
}

/**
 * The operation id for one repair — a function of the contract, the document and exactly what is
 * being repaired, and of nothing else.
 *
 * `identity_repair_` is a readable prefix rather than a required one: `operation_id` is a free
 * string in the schema, and a person reading the document should be able to tell what wrote it
 * without a lookup table.
 */
function repairOperationId(document: ApmapDocument, subjects: readonly IdentityRepairSubject[]): string {
  const payload = [IDENTITY_REPAIR_CONTRACT, document.document_id,
                   ...subjects.map((s) => [s.kind, s.id, s.rule, s.replaced].join(','))].join(UNIT_SEPARATOR);
  return `identity_repair_${sha256Hex(payload).slice(0, 16)}`;
}

/** The record one repaired object gets. Members in the schema's declared order; `replaces` is absent. */
function repairRecord(operationId: string, ordinal: number, subject: IdentityRepairSubject): Record<string, unknown> {
  return {
    kind: 'operation',
    operation_id: operationId,
    operation: APMAP_IDENTITY_REPAIR_OPERATION,
    ordinal,
    intent: IDENTITY_REPAIR_INTENT,
    metadata: { rule: subject.rule, replaced: subject.replaced },
  };
}

/**
 * **Repair the identity provenance, or say why not.**
 *
 * Pure. The document handed in is never mutated: every object that changes is rebuilt and every
 * object that does not is returned **by reference**, which is what lets a test assert that geometry
 * was not rebuilt rather than merely that it compares equal.
 *
 * `clean` and `unsupported` both return no document, for the same reason: there is nothing to write.
 */
export function repairApmapIdentity(document: ApmapDocument): IdentityRepairOutcome {
  const { analysis, subjects } = scanApmapIdentity(document);
  if (analysis.status !== 'repairable') return { outcome: analysis.status, analysis };

  // The ordinal is a running counter over the SAME walk the analysis made, so it numbers the
  // repaired objects in document order — brush before its own faces — without a second ordering
  // rule that could disagree with the first.
  let ordinal = 0;
  const repairedFace = (face: ApmapFace, where: string): ApmapFace => {
    const subject = subjects.get(`face:${where}`);
    if (!subject) return face;
    return { ...face, derived_from: repairRecord(analysis.operationId, ordinal++, subject) };
  };
  const repairedBrush = (brush: ApmapBrushItem, where: string): ApmapBrushItem => {
    const subject = subjects.get(`brush:${where}`);
    const derived = subject ? repairRecord(analysis.operationId, ordinal++, subject) : undefined;
    let facesChanged = false;
    const faces = brush.faces.map((face, index) => {
      const next = repairedFace(face, `${where}.faces[${index}]`);
      if (next !== face) facesChanged = true;
      return next;
    });
    if (!derived && !facesChanged) return brush;
    return { ...brush, ...(derived ? { derived_from: derived } : {}), ...(facesChanged ? { faces } : {}) };
  };
  const repairedEntity = (entity: ApmapEntity, index: number): ApmapEntity => {
    let changed = false;
    const content = entity.content.map((item, position) => {
      if (!isApmapBrush(item)) return item;
      const next = repairedBrush(item, `entities[${index}].content[${position}]`);
      if (next !== item) changed = true;
      return next;
    });
    return changed ? { ...entity, content } : entity;
  };

  const entities = document.entities.map(repairedEntity);
  return { outcome: 'repaired', document: { ...document, entities }, analysis };
}
