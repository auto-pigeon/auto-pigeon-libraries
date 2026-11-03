/**
 * **What a Quake II face's contents, surface flags and value ARE** — `Q2_004`, `DESIGN.md` §225.
 *
 * One resolver, and every Quake II consumer asks it: the TEXTURE UV pane's surface section, the
 * light preview's casting world, the export's readiness warning. It takes the face's stored `tail`
 * (APMap 1.5 `SEMANTICS.md` §6.3a: `CONTENTS SURFACE_FLAGS VALUE`, absent is not zero), the header
 * of the WAL the face's texture resolves to, and answers with three separate things:
 *
 * - **stored** — the three numbers the face means, and where they came from (`tail` or `wal`);
 * - **compiled** — what the pinned compiler builds from them (contents after its own rules, texinfo
 *   flags, value, whether the side is drawn), and the brush-level answer in {@link resolveQuake2Brush};
 * - **diagnostics** — every rule the compiler would apply behind the reader's back, named.
 *
 * ### The precedence is MEASURED, not read
 *
 * `testdata/ericw-q2/surfaceMatrix.json` is what EricW 1.4518 (`qbsp -q2bsp`, the AUCOM profile's
 * argv) built from 51 authored cells, every byte written for `Q2_004`. Its rules, each one a row:
 *
 * | the face line | the compiler reads | rows |
 * | --- | --- | --- |
 * | no tail | the WAL header's contents, flags and value | `s1-light-omit`, `s1-water-omit`, `s8-combo-wal` |
 * | any tail, `0 0 0` included | the tail, all three numbers; the WAL header is not consulted | `s1-light-zero`, `s1-light-value`, `s1-water-zero` |
 * | no tail, no WAL anywhere | zeros, with only a "Couldn't locate texture" warning | `s9-missing-wal` |
 *
 * So a zero tail is never "inherit": it is an override to nothing, and two faces that look the same
 * on screen compile differently. Nothing here substitutes zero for an absent header — a face whose
 * WAL is not known is `unresolved`, by name, and the readiness report says the build would default it.
 *
 * ### The compiler's own rewrites, reproduced so they can be SAID
 *
 * `qbsp` rewrites some combinations silently or with one line in a log (`map.cc` `ParseTextureDef`,
 * `bspfile.cc` `gamedef_q2_t::face_get_contents`, `Brush_GetContents`). The document is never
 * rewritten to match — that would be normalising behind the reader's back — so each rewrite is a
 * diagnostic and the `compiled` half shows its effect:
 *
 * - an author-set `CONTENTS_TRANSLUCENT` is removed, and becomes `DETAIL` when no TRANS flag is set;
 * - `SKY | NODRAW` loses `NODRAW`;
 * - no visible or clip contents → `SOLID`; TRANS33/66 (and the KMQ2 `ALPHATEST`) turn `SOLID` into
 *   `WINDOW` and add `TRANSLUCENT`; translucent, mist, aux and clip contents are `DETAIL`;
 * - `HINT`/`SKIP` faces carry no contents at all;
 * - two visible contents on one face, or `MONSTER`/`DEADMONSTER`, stop the compile;
 * - a brush takes the contents of its FIRST face that has any; a later face of another kind is a
 *   warning and is ignored (`s1-mixed-liquid-first` versus `s1-mixed-solid-first`).
 *
 * **What seals is structural `SOLID`.** Measured with one plug in a hole (`s4-plug-*`): structural
 * solid, sky and nodraw seal; detail, `SOLID|DETAIL`, water, both clips, window, TRANS33, areaportal,
 * mist and hint all leak. A portal, a clip or a detail brush is never a world-sealing wall.
 *
 * ### The bit names are id's, and nothing else is erased
 *
 * The classic names are id's `qfiles.h` (yquake2 8.70 `src/common/header/files.h`). Every other bit
 * is kept exactly — an edit of one flag changes that flag and nothing else — and reported as one this
 * build does not name, because a mod or a later compiler may mean something by it.
 */

/** Contents bits with a classic Quake II name (`qfiles.h`). */
export const QUAKE2_CONTENTS = {
  SOLID: 0x1, WINDOW: 0x2, AUX: 0x4, LAVA: 0x8, SLIME: 0x10, WATER: 0x20, MIST: 0x40,
  AREAPORTAL: 0x8000, PLAYERCLIP: 0x10000, MONSTERCLIP: 0x20000,
  CURRENT_0: 0x40000, CURRENT_90: 0x80000, CURRENT_180: 0x100000, CURRENT_270: 0x200000,
  CURRENT_UP: 0x400000, CURRENT_DOWN: 0x800000, ORIGIN: 0x1000000, MONSTER: 0x2000000,
  DEADMONSTER: 0x4000000, DETAIL: 0x8000000, TRANSLUCENT: 0x10000000, LADDER: 0x20000000,
} as const;
export type Quake2ContentsName = keyof typeof QUAKE2_CONTENTS;

/** Surface-flag bits with a classic Quake II name (`qfiles.h`). */
export const QUAKE2_SURFACE = {
  LIGHT: 0x1, SLICK: 0x2, SKY: 0x4, WARP: 0x8, TRANS33: 0x10, TRANS66: 0x20, FLOWING: 0x40, NODRAW: 0x80,
  HINT: 0x100, SKIP: 0x200,
} as const;
export type Quake2SurfaceName = keyof typeof QUAKE2_SURFACE;

const mask = (table: Record<string, number>) => Object.values(table).reduce((all, bit) => all | bit, 0);
/** Every contents bit id's game names. Anything outside it is preserved and reported. */
export const QUAKE2_CLASSIC_CONTENTS_MASK = mask(QUAKE2_CONTENTS);
/** Every surface bit id's game names. */
export const QUAKE2_CLASSIC_SURFACE_MASK = mask(QUAKE2_SURFACE);

const C = QUAKE2_CONTENTS, S = QUAKE2_SURFACE;
/** The seven visible contents, in priority order — exactly one may be on a face. */
export const QUAKE2_VISIBLE_CONTENTS = [C.SOLID, C.WINDOW, C.AUX, C.LAVA, C.SLIME, C.WATER, C.MIST] as const;
const VISIBLE = QUAKE2_VISIBLE_CONTENTS.reduce<number>((all, bit) => all | bit, 0);
/** EricW's extensions the pinned compiler honours: bit 13 `NO_WATERJUMP`, bit 14 `PROJECTILECLIP`, surface bit 25 `ALPHATEST`. */
const EWT_PROJECTILECLIP = 1 << 14;
const EWT_ALPHATEST = 1 << 25;
/** A contents "type" in `face_get_contents`' sense: visible, or one of the invisible kinds. Without one, a face is SOLID. */
const CONTENT_TYPE = VISIBLE | C.ORIGIN | C.PLAYERCLIP | C.MONSTERCLIP | C.AREAPORTAL | EWT_PROJECTILECLIP;

/** The compiler whose behaviour {@link resolveQuake2Face} reproduces, and the fixture that proves it. */
export const QUAKE2_SURFACE_COMPILER = {
  id: 'ericw-tools-auto-pigeon-1.4518',
  release: '2.0.0-alpha11-32-g0c787d7a+auto-pigeon.1.4518',
  fixture: 'packages/quake-geometry/testdata/ericw-q2/surfaceMatrix.json',
} as const;

export type Quake2SurfaceNumbers = { contents: number; flags: number; value: number };

/**
 * What the face's texture's WAL says, as the texture runtime knows it.
 *
 * `missing` — no pack holds the name, so the compiler will not find it either; `contested` — packs
 * disagree about its bytes (`DESIGN.md` §224) and no header is chosen; `unknown` — nothing is loaded
 * that could say (no pack resolver yet, or a manifest that omits the fields). Only `resolved` is a
 * header, and a header is never invented from any of the other three.
 */
export type Quake2WalHeader =
  | ({ status: 'resolved' } & Quake2SurfaceNumbers)
  | { status: 'missing' | 'contested' | 'unknown' };

export type Quake2SurfaceSeverity = 'error' | 'warning' | 'info';
export type Quake2SurfaceDiagnosticCode =
  | 'quake2_tail_invalid' | 'quake2_wal_missing' | 'quake2_wal_contested' | 'quake2_wal_unknown'
  | 'quake2_contents_unnamed_bits' | 'quake2_surface_unnamed_bits'
  | 'quake2_translucency_conflict' | 'quake2_translucent_contents_removed' | 'quake2_sky_nodraw'
  | 'quake2_mixed_visible_contents' | 'quake2_illegal_contents'
  | 'quake2_light_without_value' | 'quake2_value_without_light'
  | 'quake2_brush_mixed_contents' | 'quake2_brush_contents_ignored';

export type Quake2SurfaceDiagnostic = {
  code: Quake2SurfaceDiagnosticCode;
  severity: Quake2SurfaceSeverity;
  /** The bits the diagnostic is about, when it is about bits. */
  bits?: number;
  /** For a brush diagnostic: the face (by position in the brush) that decided, and the one that lost. */
  face?: number;
  against?: number;
  /** For `quake2_tail_invalid`: the AULIBS rule the tail breaks. */
  rule?: 'ADM-Q2-1' | 'ADM-Q2-2' | 'ADM-Q2-3';
};

/** What the compiler builds from one face. `contents === 0` is a face with no contents (HINT/SKIP). */
export type Quake2CompiledFace = Quake2SurfaceNumbers & {
  /** A side the engine draws. A NODRAW side has no texinfo; a clip-only or portal brush draws nothing. */
  drawn: boolean;
  /** SURF_LIGHT with a positive value: a surface light the LIGHT stage emits from. */
  emits: boolean;
  /** One of the compiler's stopping errors, when the face trips one. */
  fatal?: 'quake2_mixed_visible_contents' | 'quake2_illegal_contents';
};

export type Quake2FaceSurface =
  | { status: 'resolved'; source: 'tail' | 'wal'; stored: Quake2SurfaceNumbers; compiled: Quake2CompiledFace;
      wal: Quake2WalHeader; diagnostics: Quake2SurfaceDiagnostic[] }
  /** No tail and no WAL header. `compilerDefault` is what qbsp would silently use — said, never stored. */
  | { status: 'unresolved'; reason: 'wal_missing' | 'wal_contested' | 'wal_unknown'; compilerDefault: Quake2CompiledFace;
      wal: Quake2WalHeader; diagnostics: Quake2SurfaceDiagnostic[] }
  | { status: 'invalid'; rule: 'ADM-Q2-1' | 'ADM-Q2-2' | 'ADM-Q2-3'; wal: Quake2WalHeader; diagnostics: Quake2SurfaceDiagnostic[] };

const INT32_MIN = -2147483648, INT32_MAX = 2147483647;

/** A tail's three numbers, or the ADM-Q2 rule it breaks. Absent is `undefined`, never zeros. */
export function quake2TailNumbers(tail: unknown): Quake2SurfaceNumbers | undefined | { rule: 'ADM-Q2-1' | 'ADM-Q2-2' | 'ADM-Q2-3' } {
  if (tail === undefined || tail === null) return undefined;
  const values = Array.isArray(tail) ? tail.map((value) => (typeof value === 'number' ? value : Number(value))) : [];
  if (!Array.isArray(tail) || values.length !== 3) return { rule: 'ADM-Q2-1' };
  if (values.some((value) => !Number.isInteger(value))) return { rule: 'ADM-Q2-2' };
  if (values.some((value) => value < INT32_MIN || value > INT32_MAX)) return { rule: 'ADM-Q2-3' };
  return { contents: values[0], flags: values[1], value: values[2] };
}

const bitsSet = (value: number): number => {
  let count = 0;
  for (let rest = value >>> 0; rest; rest &= rest - 1) count += 1;
  return count;
};

/**
 * The compiler's face rules on three stored numbers — `ParseTextureDef` then `face_get_contents`.
 * Pure arithmetic on bits; the order is the compiler's.
 */
export function compileQuake2Face(stored: Quake2SurfaceNumbers, diagnostics: Quake2SurfaceDiagnostic[] = []): Quake2CompiledFace {
  let contents = stored.contents | 0, flags = stored.flags | 0;
  const value = stored.value | 0;
  const trans = S.TRANS33 | S.TRANS66;
  if (contents & C.TRANSLUCENT) {
    contents &= ~C.TRANSLUCENT;
    if (!(flags & trans)) contents |= C.DETAIL;
    diagnostics.push({ code: 'quake2_translucent_contents_removed', severity: 'warning', bits: C.TRANSLUCENT });
  }
  if ((flags & (S.SKY | S.NODRAW)) === (S.SKY | S.NODRAW)) {
    flags &= ~S.NODRAW;
    diagnostics.push({ code: 'quake2_sky_nodraw', severity: 'warning', bits: S.NODRAW });
  }
  let fatal: Quake2CompiledFace['fatal'];
  if (bitsSet(contents & VISIBLE) > 1) {
    fatal = 'quake2_mixed_visible_contents';
    diagnostics.push({ code: 'quake2_mixed_visible_contents', severity: 'error', bits: contents & VISIBLE });
  }
  if (contents & (C.MONSTER | C.DEADMONSTER)) {
    fatal ??= 'quake2_illegal_contents';
    diagnostics.push({ code: 'quake2_illegal_contents', severity: 'error', bits: contents & (C.MONSTER | C.DEADMONSTER) });
  }
  if (flags & (S.HINT | S.SKIP)) {
    contents = 0;
  } else {
    if (!(contents & CONTENT_TYPE)) contents |= C.SOLID;
    if (flags & (trans | EWT_ALPHATEST)) {
      contents |= C.TRANSLUCENT;
      if (contents & C.SOLID) contents = (contents & ~C.SOLID) | C.WINDOW;
    }
    if (contents & C.TRANSLUCENT) contents |= C.DETAIL;
    if (contents & (C.MIST | C.AUX)) contents |= C.DETAIL;
    if (contents & (C.PLAYERCLIP | C.MONSTERCLIP)) contents |= C.DETAIL;
  }
  const drawn = !(flags & (S.NODRAW | S.HINT | S.SKIP)) && (contents & VISIBLE) !== 0;
  return { contents, flags, value, drawn, emits: (flags & S.LIGHT) !== 0 && value > 0, ...(fatal ? { fatal } : {}) };
}

/** The bits a classic Quake II game does not name, in each field — kept, and said. */
export function quake2UnnamedBits(numbers: Pick<Quake2SurfaceNumbers, 'contents' | 'flags'>): { contents: number; flags: number } {
  return { contents: (numbers.contents & ~QUAKE2_CLASSIC_CONTENTS_MASK) >>> 0, flags: (numbers.flags & ~QUAKE2_CLASSIC_SURFACE_MASK) >>> 0 };
}

function describeStored(stored: Quake2SurfaceNumbers, diagnostics: Quake2SurfaceDiagnostic[]): void {
  const unnamed = quake2UnnamedBits(stored);
  if (unnamed.contents) diagnostics.push({ code: 'quake2_contents_unnamed_bits', severity: 'warning', bits: unnamed.contents });
  if (unnamed.flags) diagnostics.push({ code: 'quake2_surface_unnamed_bits', severity: 'warning', bits: unnamed.flags });
  if ((stored.flags & (S.TRANS33 | S.TRANS66)) === (S.TRANS33 | S.TRANS66))
    diagnostics.push({ code: 'quake2_translucency_conflict', severity: 'warning', bits: S.TRANS33 | S.TRANS66 });
  if ((stored.flags & S.LIGHT) && stored.value <= 0) diagnostics.push({ code: 'quake2_light_without_value', severity: 'info', bits: S.LIGHT });
  // EricW reads a value on a face without SURF_LIGHT as a phong angle (`map.cc`: `wants_phong`).
  if (!(stored.flags & S.LIGHT) && stored.value !== 0) diagnostics.push({ code: 'quake2_value_without_light', severity: 'info' });
}

/**
 * One face. `tail` is the APMap member as stored (`undefined` when absent); `wal` is the header the
 * texture resolves to. The answer never reads a WAL header when a tail is present — measured.
 */
export function resolveQuake2Face(tail: unknown, wal: Quake2WalHeader): Quake2FaceSurface {
  const diagnostics: Quake2SurfaceDiagnostic[] = [];
  const numbers = quake2TailNumbers(tail);
  if (numbers && 'rule' in numbers) {
    diagnostics.push({ code: 'quake2_tail_invalid', severity: 'error', rule: numbers.rule });
    return { status: 'invalid', rule: numbers.rule, wal, diagnostics };
  }
  // A face whose texture no pack holds is a blocked asset for the build whether or not it has a tail.
  if (wal.status === 'missing') diagnostics.push({ code: 'quake2_wal_missing', severity: 'error' });
  if (wal.status === 'contested') diagnostics.push({ code: 'quake2_wal_contested', severity: 'error' });
  if (numbers) {
    describeStored(numbers, diagnostics);
    return { status: 'resolved', source: 'tail', stored: numbers, compiled: compileQuake2Face(numbers, diagnostics), wal, diagnostics };
  }
  if (wal.status === 'resolved') {
    const stored = { contents: wal.contents | 0, flags: wal.flags | 0, value: wal.value | 0 };
    describeStored(stored, diagnostics);
    return { status: 'resolved', source: 'wal', stored, compiled: compileQuake2Face(stored, diagnostics), wal, diagnostics };
  }
  if (wal.status === 'unknown') diagnostics.push({ code: 'quake2_wal_unknown', severity: 'warning' });
  const reason = wal.status === 'missing' ? 'wal_missing' : wal.status === 'contested' ? 'wal_contested' : 'wal_unknown';
  return { status: 'unresolved', reason, compilerDefault: compileQuake2Face({ contents: 0, flags: 0, value: 0 }), wal, diagnostics };
}

export type Quake2BrushSurface = {
  /** The native contents the BSP brush gets: the first face with any contents decides. `0` — no brush is emitted. */
  contents: number;
  /** Which face decided, by position; `undefined` when no face has contents. */
  decidingFace?: number;
  /** A structural solid: the only brush that stops a leak (measured: `s4-plug-*`). Worldspawn only — an entity brush never seals. */
  seals: boolean;
  /** Whether the answer rests on a face that is unresolved or invalid (then it is the compiler's DEFAULT, not the map's meaning). */
  certain: boolean;
  fatal?: Quake2CompiledFace['fatal'];
  diagnostics: Quake2SurfaceDiagnostic[];
};

/** A contents "kind" in `Brush_GetContents`' comparison: visible plus invisible kinds, flags ignored. */
const kindOf = (contents: number): number => contents & CONTENT_TYPE;

/**
 * A brush from its faces' answers, in the brush's face order. The compiler's own rule: the first
 * face whose compiled contents are not empty decides; a later face of another KIND is the warning it
 * prints, and a later face of the same kind whose other bits differ (a `DETAIL` on face 3 only) is
 * silently ignored — which this says, because the reader set that bit and the build drops it.
 */
export function resolveQuake2Brush(faces: readonly Quake2FaceSurface[], options: { entityBrush?: boolean } = {}): Quake2BrushSurface {
  const diagnostics: Quake2SurfaceDiagnostic[] = [];
  let contents = 0, decidingFace: number | undefined, fatal: Quake2CompiledFace['fatal'], certain = true;
  faces.forEach((face, index) => {
    if (face.status === 'invalid') { certain = false; return; }
    const compiled = face.status === 'resolved' ? face.compiled : face.compilerDefault;
    if (face.status !== 'resolved') certain = false;
    if (compiled.fatal) fatal ??= compiled.fatal;
    if (!compiled.contents) return;
    if (decidingFace === undefined) { decidingFace = index; contents = compiled.contents; return; }
    if (kindOf(compiled.contents) !== kindOf(contents))
      diagnostics.push({ code: 'quake2_brush_mixed_contents', severity: 'warning', face: decidingFace, against: index, bits: compiled.contents });
    else if (compiled.contents !== contents)
      diagnostics.push({ code: 'quake2_brush_contents_ignored', severity: 'info', face: decidingFace, against: index, bits: compiled.contents ^ contents });
  });
  const seals = !options.entityBrush && !fatal && (contents & C.SOLID) !== 0 && (contents & C.DETAIL) === 0;
  return { contents, ...(decidingFace === undefined ? {} : { decidingFace }), seals, certain, ...(fatal ? { fatal } : {}), diagnostics };
}

/** `SOLID|WATER|0x400` — the names a reader recognises, then any bit nothing names, in hex. */
export function quake2BitNames(value: number, field: 'contents' | 'flags'): string[] {
  const table = field === 'contents' ? QUAKE2_CONTENTS : QUAKE2_SURFACE;
  const names = Object.entries(table).filter(([, bit]) => (value & bit) !== 0).map(([name]) => name);
  const rest = (value & ~(field === 'contents' ? QUAKE2_CLASSIC_CONTENTS_MASK : QUAKE2_CLASSIC_SURFACE_MASK)) >>> 0;
  for (let bit = 0; bit < 32; bit += 1) if (rest & (2 ** bit)) names.push(`0x${(2 ** bit).toString(16)}`);
  return names;
}

/** A signed 32-bit tail number as the hex a reader of `qfiles.h` reads: `-2147483647` is `0x80000001`. */
export const quake2Hex = (value: number): string => `0x${(value >>> 0).toString(16).toUpperCase()}`;

// ---- editing one face's three numbers ------------------------------------------------------------

/**
 * One reader edit to a face's Quake II numbers. Each changes exactly what it names:
 *
 * - `bit` sets or clears one bit of one field — every other bit, known or not, is kept;
 * - `visible` replaces the visible contents (one of {@link QUAKE2_VISIBLE_CONTENTS}, or `0`) and
 *   keeps every invisible and flag bit;
 * - `set` writes one whole field (the advanced masks and the value);
 * - `override` writes the WAL header's three numbers as the face's own tail;
 * - `inherit` removes the tail, so the face reads its WAL header again.
 */
export type Quake2SurfaceEdit =
  | { kind: 'bit'; field: 'contents' | 'flags'; bit: number; on: boolean }
  | { kind: 'visible'; contents: number }
  | { kind: 'set'; field: 'contents' | 'flags' | 'value'; value: number }
  | { kind: 'override' }
  | { kind: 'inherit' };

export type Quake2SurfaceEditRefusal = {
  refusal: 'tail_invalid' | 'wal_unresolved' | 'value_not_integer' | 'value_out_of_range' | 'bit_invalid' | 'visible_invalid';
  value?: number;
};

const ALL_VISIBLE = QUAKE2_VISIBLE_CONTENTS.reduce<number>((all, bit) => all | bit, 0);

/** Whether an edit is well-formed before any face is read — the whole selection is refused on a bad number. */
export function quake2SurfaceEditRefusal(edit: Quake2SurfaceEdit): Quake2SurfaceEditRefusal | undefined {
  if (edit.kind === 'bit' && (!Number.isInteger(edit.bit) || bitsSet(edit.bit) !== 1)) return { refusal: 'bit_invalid', value: edit.bit };
  if (edit.kind === 'visible' && edit.contents !== 0 && !QUAKE2_VISIBLE_CONTENTS.includes(edit.contents as never))
    return { refusal: 'visible_invalid', value: edit.contents };
  if (edit.kind === 'set') {
    if (!Number.isInteger(edit.value)) return { refusal: 'value_not_integer', value: edit.value };
    if (edit.value < INT32_MIN || edit.value > INT32_MAX) return { refusal: 'value_out_of_range', value: edit.value };
  }
  return undefined;
}

/**
 * The face's tail after one edit: three numbers, `undefined` for "no tail" (inherit), the same array
 * when nothing changed, or a refusal. A field edit on a face that inherits starts from the WAL
 * header's numbers — the face then OVERRIDES, which is what any explicit tail does (measured) — and
 * is refused by name when that header is not known, because writing zeros there would silently
 * replace what the face actually compiles to.
 */
export function quake2TailAfterEdit(tail: unknown, wal: Quake2WalHeader, edit: Quake2SurfaceEdit):
  number[] | undefined | Quake2SurfaceEditRefusal {
  const invalid = quake2SurfaceEditRefusal(edit);
  if (invalid) return invalid;
  const numbers = quake2TailNumbers(tail);
  if (numbers && 'rule' in numbers) return { refusal: 'tail_invalid' };
  if (edit.kind === 'inherit') return undefined;
  const base = numbers ?? (wal.status === 'resolved' ? { contents: wal.contents | 0, flags: wal.flags | 0, value: wal.value | 0 } : undefined);
  if (!base) return { refusal: 'wal_unresolved' };
  if (edit.kind === 'override') return numbers ? (tail as number[]) : [base.contents, base.flags, base.value];
  const next = { ...base };
  if (edit.kind === 'bit') next[edit.field] = edit.on ? (next[edit.field] | edit.bit) : (next[edit.field] & ~edit.bit);
  else if (edit.kind === 'visible') next.contents = (next.contents & ~ALL_VISIBLE) | edit.contents;
  else next[edit.field] = edit.value | 0;
  // A face that already has what was asked keeps what it had — an inheriting face is not turned
  // into an overriding one by an edit that changes none of its numbers.
  if (next.contents === base.contents && next.flags === base.flags && next.value === base.value) return tail as number[] | undefined;
  return [next.contents | 0, next.flags | 0, next.value | 0];
}

// ---- the whole document: what a build of it would do ---------------------------------------------

export type Quake2SurfaceReportEntry = { code: Quake2SurfaceDiagnosticCode; severity: Quake2SurfaceSeverity; count: number;
                                         /** The first face (or brush) it was found on, so a reader can go there. */
                                         firstFaceId?: string; firstBrushId?: string; bits?: number };

export type Quake2SurfaceReport = {
  faces: number;
  /** Faces whose numbers come from their WAL, from their own tail, from nobody (unresolved), or are unreadable. */
  inherited: number; explicit: number; unresolved: number; invalid: number;
  brushes: number;
  /** Brushes whose compile would stop qbsp (`quake2_mixed_visible_contents`, `quake2_illegal_contents`). */
  fatalBrushes: number;
  /** Every diagnostic, counted per code (and per bit pattern for the bit diagnostics), most severe first. */
  entries: Quake2SurfaceReportEntry[];
};

const SEVERITY_ORDER: Record<Quake2SurfaceSeverity, number> = { error: 0, warning: 1, info: 2 };
const BRUSH_CLASSES_IN_WORLD = new Set(['worldspawn', 'func_group']);

/**
 * **What building this document would do to its surfaces** — the readiness half of `DESIGN.md` §225.
 * Never a refusal and never a rewrite: the `.map` writer still writes every face exactly (ADM-Q2-1..3
 * remain its only refusals), and this is what the export says beside it — a face whose WAL no pack
 * holds is a blocked asset the compiler would build as zeros, two visible contents stop qbsp, and so on.
 */
export function quake2SurfaceReport(document: { entities?: unknown }, headerOf: (texture: string) => Quake2WalHeader): Quake2SurfaceReport {
  const report: Quake2SurfaceReport = { faces: 0, inherited: 0, explicit: 0, unresolved: 0, invalid: 0, brushes: 0, fatalBrushes: 0, entries: [] };
  const entries = new Map<string, Quake2SurfaceReportEntry>();
  const note = (diagnostic: Quake2SurfaceDiagnostic, at: { faceId?: string; brushId?: string }) => {
    const key = `${diagnostic.code}:${diagnostic.bits ?? ''}:${diagnostic.rule ?? ''}`;
    const held = entries.get(key);
    if (held) { held.count += 1; return; }
    entries.set(key, { code: diagnostic.code, severity: diagnostic.severity, count: 1, ...(diagnostic.bits === undefined ? {} : { bits: diagnostic.bits }),
                       ...(at.faceId ? { firstFaceId: at.faceId } : {}), ...(at.brushId ? { firstBrushId: at.brushId } : {}) });
  };
  const entities = Array.isArray(document.entities) ? document.entities as Array<{ content?: unknown }> : [];
  for (const entity of entities) {
    const content = Array.isArray(entity?.content) ? entity.content as Array<Record<string, unknown>> : [];
    const classname = String((content.find((item) => item?.kind === 'property' && item?.key === 'classname') as { value?: unknown } | undefined)?.value ?? '');
    const entityBrush = !BRUSH_CLASSES_IN_WORLD.has(classname);
    for (const item of content) {
      if (item?.kind !== 'brush' || !Array.isArray(item.faces)) continue;
      const faces = item.faces as Array<{ face_id?: string; texture?: string; tail?: unknown }>;
      const surfaces = faces.map((face) => resolveQuake2Face(face.tail, headerOf(String(face.texture ?? ''))));
      surfaces.forEach((surface, index) => {
        report.faces += 1;
        if (surface.status === 'invalid') report.invalid += 1;
        else if (surface.status === 'unresolved') report.unresolved += 1;
        else if (surface.source === 'tail') report.explicit += 1;
        else report.inherited += 1;
        for (const diagnostic of surface.diagnostics) note(diagnostic, { faceId: faces[index]?.face_id, brushId: String(item.brush_id ?? '') });
      });
      const brush = resolveQuake2Brush(surfaces, { entityBrush });
      report.brushes += 1;
      if (brush.fatal) report.fatalBrushes += 1;
      for (const diagnostic of brush.diagnostics) note(diagnostic, { brushId: String(item.brush_id ?? '') });
    }
  }
  report.entries = [...entries.values()].sort((a, b) => SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity] || b.count - a.count || a.code.localeCompare(b.code));
  return report;
}
