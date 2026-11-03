/**
 * **The Quake III `.map` reader** — `Q3_002`.
 *
 * `parser.ts` reads Quake 1 (classic and Valve 220) one line at a time, and it is right to: a Quake
 * 1 face is one line and a Quake 1 map has nothing else. A Quake III map does. A brushDef face
 * carries a nested `( ( a b c ) ( d e f ) )` matrix, a patchDef2 block is a grid of parenthesised
 * five-number points that Radiant spreads over as many lines as it likes, and the three numbers that
 * end a face are optional. So this is a **token** reader, and the tokenizer is Q3Map2's own
 * `scriplib.c` rule, because the question this module answers is *"what will Q3Map2 read?"*:
 *
 * - whitespace is every byte ≤ 32, and a token runs to the next one;
 * - `//`, `;` and `#` at the start of a token comment out the rest of the line, `/* … *\/` a span;
 * - a `"` starts a quoted token that ends at the next `"`. **There is no escape character** — Q3Map2
 *   has none, and neither does any Radiant writer — so a backslash is an ordinary byte and a quote
 *   cannot appear inside a key or a value. A line break inside quotes is refused: no writer emits
 *   one, and APMap's `map_string` cannot hold one.
 * - parentheses and braces are tokens only when whitespace separates them, exactly as
 *   `MatchToken( "(" )` requires. `(0 0 0)` is not a point to Q3Map2, so it is not one here.
 *
 * ### What is read, and what is refused by name
 *
 * | block | here | why |
 * | --- | --- | --- |
 * | classic face, with or without the three-number tail | read | Q3Map2's `BPRIMIT_OLDBRUSHES` |
 * | `brushDef { … }` face with a 2x3 texture matrix | read, matrix verbatim | `BPRIMIT_NEWBRUSHES` (APMap 1.5 §6.2a) |
 * | `patchDef2 { … }` | read, every number verbatim | APMap 1.5 §9c; editing is `Q3_003` |
 * | `brushDef3`, `patchDef3` | **refused** | Doom 3 grammars; measured `MatchToken( "(" ) failed` in Q3Map2 (`Q3_001`) |
 * | a Valve 220 `[ … ]` face, with or without the tail | read | NetRadiant-custom's Q3Map2 compiles it (measured, `Q3_002`), and real Quake III sources are written in it |
 * | a brush mixing face syntaxes | **refused** | Q3Map2 fixes one syntax per file from the first brush (measured); inside one brush there is no reading at all |
 * | editor key/value pairs inside `brushDef` | **refused** | Q3Map2 skips them; keeping them has no APMap member and dropping them is loss |
 * | any other word where a brush was expected | **refused** | nothing is guessed |
 *
 * A refusal is an **error on the list**, never an exception and never a skip: the caller receives
 * every fault with its line and column and decides — the importer refuses the whole map if the list
 * is not empty, so nothing is ever imported as "the part that parsed".
 *
 * ### Nothing from the file reaches a message
 *
 * `SourceRange.excerpt` is left empty. The Quake 1 reader quotes 180 characters of the offending
 * line; a Quake III map is often somebody's unreleased work (`Q3_000`'s corpus rules), and a
 * diagnostic that is copied into a bug report must not carry a piece of it. The line and column are
 * enough to find it in the file, which the reader has and the report does not.
 *
 * ### Large files
 *
 * `parseQuake3MapAsync` yields to the event loop every `sliceMs` milliseconds, reports the fraction
 * of the text consumed, and stops at the first check after its `AbortSignal` fires — throwing the
 * DOM's `AbortError` so a superseded open is not reported as a failure. The synchronous
 * `parseQuake3Map` is the same generator run to completion.
 */
import type { MapParseError, SourceRange } from './types.ts';

export type Quake3Vec3 = [number, number, number];

/** A classic face's five texture numbers — Q3Map2's `QuakeTextureVecs` inputs. */
export type Quake3ClassicTexture = { shift: [number, number]; rotation: number; scale: [number, number] };

export type Quake3Face = {
  points: [Quake3Vec3, Quake3Vec3, Quake3Vec3];
  /** The shader name as written — Q3Map2 prefixes `textures/` itself. */
  texture: string;
  /** Present on a classic face. */
  classic?: Quake3ClassicTexture;
  /** Present on a Valve 220 face: `[x y z shift]` axes, the decorative rotation and the scale. */
  valve?: { u: [number, number, number, number]; v: [number, number, number, number]; rotation: number; scale: [number, number] };
  /** Present on a brushDef face: the 2x3 texture matrix, verbatim. */
  matrix?: [[number, number, number], [number, number, number]];
  /** Content flags, surface flags, value — present only when the file wrote them. */
  tail: number[];
  line: number;
  column: number;
};

export type Quake3Brush = { kind: 'brush'; syntax: 'classic' | 'valve220' | 'brush_primitives'; faces: Quake3Face[]; line: number; column: number };
export type Quake3Patch = {
  kind: 'patch'; texture: string; width: number; height: number;
  /** The three header numbers after width and height, verbatim. */
  tail: number[];
  /** `width` columns of `height` `[x, y, z, s, t]` — patchDef2 text order. */
  controlPoints: number[][][];
  line: number; column: number;
};
export type Quake3Property = { kind: 'property'; key: string; value: string; line: number; column: number };
export type Quake3ContentItem = Quake3Property | Quake3Brush | Quake3Patch;
export type Quake3Entity = { index: number; content: Quake3ContentItem[]; line: number; column: number };

export type Quake3MapStats = {
  bytes: number; entities: number; properties: number; brushes: number;
  classicBrushes: number; valve220Brushes: number; brushPrimitiveBrushes: number; faces: number; patches: number;
};
export type Quake3ParsedMap = { entities: Quake3Entity[]; errors: MapParseError[]; stats: Quake3MapStats };

/** The refusal codes, one per grammar the reader declines. Stable: tests and the editor read them. */
export const QUAKE3_PARSE_CODES = {
  unterminatedString: 'Q3_UNTERMINATED_STRING',
  unterminatedComment: 'Q3_UNTERMINATED_COMMENT',
  contentOutsideEntity: 'Q3_CONTENT_OUTSIDE_ENTITY',
  invalidProperty: 'Q3_INVALID_PROPERTY',
  unexpectedToken: 'Q3_UNEXPECTED_TOKEN',
  unexpectedEnd: 'Q3_UNEXPECTED_END',
  invalidNumber: 'Q3_INVALID_NUMBER',
  mixedSyntax: 'Q3_MIXED_BRUSH_SYNTAX',
  doom3Grammar: 'Q3_DOOM3_GRAMMAR_UNSUPPORTED',
  unknownBlock: 'Q3_UNKNOWN_BLOCK',
  brushEpairs: 'Q3_BRUSH_EPAIRS_UNSUPPORTED',
  patchSize: 'Q3_PATCH_SIZE_UNSUPPORTED',
  tooFewFaces: 'Q3_BRUSH_TOO_FEW_FACES',
} as const;

type Token = { text: string; quoted: boolean; line: number; column: number; offset: number };

class Quake3ParseAbort extends Error {}

/**
 * Q3Map2's tokenizer (`scriplib.c` `GetToken`), with positions. `next(crossLine)` mirrors its
 * `crossline` argument: a face's optional tail is read only while tokens remain on the SAME line,
 * because that is how Q3Map2's `TokenAvailable()` decides there is one.
 */
class Lexer {
  offset = 0;
  line = 1;
  column = 1;
  private peeked: Token | undefined;
  private peekedLine = 0;
  constructor(private readonly text: string, private readonly errors: MapParseError[]) {}

  private advance(count = 1): void {
    for (let index = 0; index < count && this.offset < this.text.length; index += 1) {
      if (this.text.charCodeAt(this.offset) === 10) { this.line += 1; this.column = 1; } else this.column += 1;
      this.offset += 1;
    }
  }

  /** Skip whitespace and comments. Returns false at the end of the text. */
  private skip(): boolean {
    const text = this.text;
    for (;;) {
      while (this.offset < text.length && text.charCodeAt(this.offset) <= 32) this.advance();
      if (this.offset >= text.length) return false;
      const c = text[this.offset];
      if (c === ';' || c === '#' || (c === '/' && text[this.offset + 1] === '/')) {
        while (this.offset < text.length && text.charCodeAt(this.offset) !== 10) this.advance();
        continue;
      }
      if (c === '/' && text[this.offset + 1] === '*') {
        const line = this.line, column = this.column;
        const end = text.indexOf('*/', this.offset + 2);
        if (end < 0) {
          this.errors.push(error(QUAKE3_PARSE_CODES.unterminatedComment, 'a /* comment is never closed', line, column));
          this.advance(text.length - this.offset);
          return false;
        }
        this.advance(end + 2 - this.offset);
        continue;
      }
      return true;
    }
  }

  private read(): Token | undefined {
    if (!this.skip()) return undefined;
    const text = this.text, line = this.line, column = this.column, offset = this.offset;
    if (text[this.offset] === '"') {
      const close = text.indexOf('"', this.offset + 1);
      const newline = text.indexOf('\n', this.offset + 1);
      if (close < 0 || (newline >= 0 && newline < close)) {
        this.errors.push(error(QUAKE3_PARSE_CODES.unterminatedString, 'a quoted string is not closed on the line it starts on', line, column));
        // Resynchronise at the end of the line rather than swallowing the rest of the file.
        this.advance((newline < 0 ? text.length : newline) - this.offset);
        return { text: '', quoted: true, line, column, offset };
      }
      const value = text.slice(this.offset + 1, close);
      this.advance(close + 1 - this.offset);
      return { text: value, quoted: true, line, column, offset };
    }
    let end = this.offset;
    while (end < text.length && text.charCodeAt(end) > 32 && text[end] !== ';') end += 1;
    const value = text.slice(this.offset, end);
    this.advance(end - this.offset);
    return { text: value, quoted: false, line, column, offset };
  }

  /** The next token, or `undefined` at the end. `sameLine` refuses to cross a line break. */
  next(sameLine = false): Token | undefined {
    const token = sameLine ? this.peekOnLine() : this.peek();
    if (token) this.peeked = undefined;
    return token;
  }

  peek(): Token | undefined {
    if (!this.peeked) {
      this.peekedLine = this.line;
      this.peeked = this.read();
    }
    return this.peeked;
  }

  /** The next token only when no line break separates it from the last one consumed. */
  peekOnLine(): Token | undefined {
    const token = this.peek();
    return token && token.line === this.peekedLine ? token : undefined;
  }
}

const range = (line: number, column: number): SourceRange => ({ line, column, excerpt: '' });
const error = (code: string, message: string, line: number, column: number, entityIndex?: number, brushIndex?: number): MapParseError =>
  ({ code, message, range: range(line, column), ...(entityIndex === undefined ? {} : { entityIndex }), ...(brushIndex === undefined ? {} : { brushIndex }) });

/** Q3Map2 reads numbers with `atof`; this is stricter — a token that is not wholly a finite number is refused. */
const NUMBER = /^[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?$/;

const DOOM3_BLOCKS = new Set(['brushDef3', 'patchDef3', 'patchDef2WS', 'patchDef3WS']);

/** Whether a text holds Quake III-only brush syntax. What a non-Quake III open asks before it fails generically. */
export function hasQuake3OnlySyntax(text: string): boolean {
  return /(^|\s)(brushDef|patchDef2|brushDef3|patchDef3)(\s|$)/m.test(text);
}

type Context = { entityIndex: number; brushIndex: number };

/**
 * The parser proper, as a generator so the async form can yield between entities. It yields the
 * text offset it has reached, never a partial result.
 */
function* parse(text: string, out: Quake3ParsedMap): Generator<number, void, void> {
  const errors = out.errors;
  const lexer = new Lexer(text.replace(/\r\n?/g, '\n'), errors);
  const stats = out.stats;

  /** Consume one token and demand it is `expected`. Records and returns false otherwise. */
  const expect = (expected: string, context: Context, what: string): boolean => {
    const token = lexer.peek();
    if (!token) { errors.push(error(QUAKE3_PARSE_CODES.unexpectedEnd, `the file ends where ${what} expected "${expected}"`, lexer.line, lexer.column, context.entityIndex, context.brushIndex)); return false; }
    if (token.quoted || token.text !== expected) {
      errors.push(error(QUAKE3_PARSE_CODES.unexpectedToken, `${what} expected "${expected}"`, token.line, token.column, context.entityIndex, context.brushIndex));
      // A brace is left for `recover` to count: consuming one here would close the wrong block.
      if (token.quoted || (token.text !== '{' && token.text !== '}')) lexer.next();
      return false;
    }
    lexer.next();
    return true;
  };
  const number = (context: Context, what: string, sameLine = false): number | undefined => {
    const token = lexer.next(sameLine);
    if (!token) {
      errors.push(error(QUAKE3_PARSE_CODES.unexpectedEnd, `the file ends where ${what} expected a number`, lexer.line, lexer.column, context.entityIndex, context.brushIndex));
      return undefined;
    }
    const value = NUMBER.test(token.text) && !token.quoted ? Number(token.text) : Number.NaN;
    if (!Number.isFinite(value)) {
      errors.push(error(QUAKE3_PARSE_CODES.invalidNumber, `${what} is not a finite number`, token.line, token.column, context.entityIndex, context.brushIndex));
      return undefined;
    }
    return value;
  };
  /** `( n n … )` with exactly `count` numbers. */
  const vector = (count: number, context: Context, what: string): number[] | undefined => {
    if (!expect('(', context, what)) return undefined;
    const values: number[] = [];
    for (let index = 0; index < count; index += 1) {
      const value = number(context, what);
      if (value === undefined) return undefined;
      values.push(value);
    }
    return expect(')', context, what) ? values : undefined;
  };
  /** Skip to the brace that closes the block we are in, so one fault does not cascade into fifty. */
  const recover = (depth: number): void => {
    let open = depth;
    for (let token = lexer.next(); token; token = lexer.next()) {
      if (token.quoted) continue;
      if (token.text === '{') open += 1;
      else if (token.text === '}') { open -= 1; if (open <= 0) return; }
    }
  };
  /** Up to three tail numbers, only while they stay on the line the face ended on (`TokenAvailable()`). */
  const tail = (context: Context, face: string): number[] | undefined => {
    const values: number[] = [];
    for (let token = lexer.peekOnLine(); token; token = lexer.peekOnLine()) {
      if (values.length === 3) {
        errors.push(error(QUAKE3_PARSE_CODES.unexpectedToken, `${face} carries more than three trailing values`, token.line, token.column, context.entityIndex, context.brushIndex));
        return undefined;
      }
      const value = number(context, `${face}'s trailing value`, true);
      if (value === undefined) return undefined;
      values.push(value);
    }
    return values;
  };
  /** A face's shader is on the face's own line (`GetToken( qfalse )`); a patch's may start one (`qtrue`). */
  const shaderName = (context: Context, what: string, crossLine = false): string | undefined => {
    const token = crossLine ? lexer.peek() : lexer.peekOnLine();
    if (!token || (!token.quoted && ['(', ')', '{', '}', '['].includes(token.text))) {
      errors.push(error(QUAKE3_PARSE_CODES.unexpectedToken, `${what} expected a shader name`, token?.line ?? lexer.line, token?.column ?? lexer.column, context.entityIndex, context.brushIndex));
      return undefined;
    }
    lexer.next();
    return token.text;
  };
  const points = (context: Context, what: string): [Quake3Vec3, Quake3Vec3, Quake3Vec3] | undefined => {
    const result: Quake3Vec3[] = [];
    for (let index = 0; index < 3; index += 1) {
      const point = vector(3, context, what);
      if (!point) return undefined;
      result.push(point as Quake3Vec3);
    }
    return result as [Quake3Vec3, Quake3Vec3, Quake3Vec3];
  };

  const classicFace = (context: Context, faceIndex: number, start: Token): Quake3Face | undefined => {
    const what = `face ${faceIndex}`;
    const plane = points(context, what);
    if (!plane) return undefined;
    const texture = shaderName(context, what);
    if (texture === undefined) return undefined;
    const bracket = lexer.peekOnLine();
    if (bracket && !bracket.quoted && bracket.text === '[') {
      const axis = (): [number, number, number, number] | undefined => {
        if (!expect('[', context, `${what}'s Valve 220 axis`)) return undefined;
        const values: number[] = [];
        for (let index = 0; index < 4; index += 1) {
          const value = number(context, `${what}'s Valve 220 axis`, true);
          if (value === undefined) return undefined;
          values.push(value);
        }
        return expect(']', context, `${what}'s Valve 220 axis`) ? values as [number, number, number, number] : undefined;
      };
      const u = axis(); if (!u) return undefined;
      const v = axis(); if (!v) return undefined;
      const rest: number[] = [];
      for (let index = 0; index < 3; index += 1) {
        const value = number(context, `${what}'s rotation and scale`, true);
        if (value === undefined) return undefined;
        rest.push(value);
      }
      const trailing = tail(context, what);
      if (!trailing) return undefined;
      return { points: plane, texture, valve: { u, v, rotation: rest[0], scale: [rest[1], rest[2]] }, tail: trailing, line: start.line, column: start.column };
    }
    const values: number[] = [];
    for (let index = 0; index < 5; index += 1) {
      const value = number(context, `${what}'s texture shift, rotation and scale`, true);
      if (value === undefined) return undefined;
      values.push(value);
    }
    const trailing = tail(context, what);
    if (!trailing) return undefined;
    return { points: plane, texture, classic: { shift: [values[0], values[1]], rotation: values[2], scale: [values[3], values[4]] },
             tail: trailing, line: start.line, column: start.column };
  };

  const primitiveFace = (context: Context, faceIndex: number, start: Token): Quake3Face | undefined => {
    const what = `brushDef face ${faceIndex}`;
    const plane = points(context, what);
    if (!plane) return undefined;
    if (!expect('(', context, `${what}'s texture matrix`)) return undefined;
    const row0 = vector(3, context, `${what}'s texture matrix`);
    if (!row0) return undefined;
    const row1 = vector(3, context, `${what}'s texture matrix`);
    if (!row1) return undefined;
    if (!expect(')', context, `${what}'s texture matrix`)) return undefined;
    const texture = shaderName(context, what);
    if (texture === undefined) return undefined;
    const trailing = tail(context, what);
    if (!trailing) return undefined;
    return { points: plane, texture, matrix: [row0 as [number, number, number], row1 as [number, number, number]],
             tail: trailing, line: start.line, column: start.column };
  };

  /** After the brush's own `{`. Consumes through its closing `}`. */
  const brushBlock = (entity: Quake3Entity, context: Context, open: Token): void => {
    const first = lexer.peek();
    if (!first) { errors.push(error(QUAKE3_PARSE_CODES.unexpectedEnd, 'the file ends inside a brush', open.line, open.column, context.entityIndex, context.brushIndex)); return; }
    if (!first.quoted && DOOM3_BLOCKS.has(first.text)) {
      errors.push(error(QUAKE3_PARSE_CODES.doom3Grammar, `${first.text} is a Doom 3 / Quake 4 grammar that Q3Map2 does not read`, first.line, first.column, context.entityIndex, context.brushIndex));
      recover(1);
      return;
    }
    if (!first.quoted && first.text === 'patchDef2') {
      lexer.next();
      const patch = patchBlock(context, first);
      if (!patch) { recover(patchDepth); patchDepth = 1; return; }
      if (!expect('}', context, 'the patch block')) { recover(1); return; }
      entity.content.push(patch);
      stats.patches += 1;
      return;
    }
    if (!first.quoted && first.text === 'brushDef') {
      lexer.next();
      if (!expect('{', context, 'brushDef')) { recover(1); return; }
      const brush: Quake3Brush = { kind: 'brush', syntax: 'brush_primitives', faces: [], line: open.line, column: open.column };
      for (;;) {
        const token = lexer.peek();
        if (!token) { errors.push(error(QUAKE3_PARSE_CODES.unexpectedEnd, 'the file ends inside a brushDef', open.line, open.column, context.entityIndex, context.brushIndex)); return; }
        if (token.quoted) {
          errors.push(error(QUAKE3_PARSE_CODES.brushEpairs, 'a brushDef carries editor key/value pairs, which APMap has no place for', token.line, token.column, context.entityIndex, context.brushIndex));
          recover(2);
          return;
        }
        if (token.text === '}') { lexer.next(); break; }
        if (token.text !== '(') {
          errors.push(error(QUAKE3_PARSE_CODES.unexpectedToken, 'a brushDef face must start with "("', token.line, token.column, context.entityIndex, context.brushIndex));
          recover(2);
          return;
        }
        const face = primitiveFace(context, brush.faces.length, token);
        if (!face) { recover(2); return; }
        brush.faces.push(face);
      }
      if (!expect('}', context, 'the brush holding a brushDef')) { recover(1); return; }
      pushBrush(entity, context, brush);
      return;
    }
    if (first.quoted || first.text !== '(') {
      if (!first.quoted && /^[A-Za-z_]\w*$/.test(first.text))
        errors.push(error(QUAKE3_PARSE_CODES.unknownBlock, `"${first.text.slice(0, 32)}" is not a brush block this editor reads`, first.line, first.column, context.entityIndex, context.brushIndex));
      else if (first.text !== '}')
        errors.push(error(QUAKE3_PARSE_CODES.unexpectedToken, 'a brush must start with "(", brushDef or patchDef2', first.line, first.column, context.entityIndex, context.brushIndex));
      else errors.push(error(QUAKE3_PARSE_CODES.tooFewFaces, 'a brush has no faces', first.line, first.column, context.entityIndex, context.brushIndex));
      recover(1);
      return;
    }
    const brush: Quake3Brush = { kind: 'brush', syntax: 'classic', faces: [], line: open.line, column: open.column };
    let mixed = false;
    for (;;) {
      const token = lexer.peek();
      if (!token) { errors.push(error(QUAKE3_PARSE_CODES.unexpectedEnd, 'the file ends inside a brush', open.line, open.column, context.entityIndex, context.brushIndex)); return; }
      if (!token.quoted && token.text === '}') { lexer.next(); break; }
      if (token.quoted || token.text !== '(') {
        errors.push(error(QUAKE3_PARSE_CODES.unexpectedToken, token.text === '{' && !token.quoted ? 'a brace opens inside a brush' : 'a face must start with "("',
                          token.line, token.column, context.entityIndex, context.brushIndex));
        recover(1);
        return;
      }
      const face = classicFace(context, brush.faces.length, token);
      if (!face) { recover(1); return; }
      // The first face decides the brush's syntax; a later face in the other one is a brush Q3Map2
      // cannot read at all (it fixes the syntax per FILE, measured), so it is an error, not a choice.
      const syntax = face.valve ? 'valve220' : 'classic';
      if (!brush.faces.length) brush.syntax = syntax;
      else if (syntax !== brush.syntax && !mixed) {
        mixed = true;
        errors.push(error(QUAKE3_PARSE_CODES.mixedSyntax, `face ${brush.faces.length} is ${syntax === 'valve220' ? 'Valve 220' : 'classic'} in a ${brush.syntax === 'valve220' ? 'Valve 220' : 'classic'} brush`,
                          face.line, face.column, context.entityIndex, context.brushIndex));
      }
      brush.faces.push(face);
    }
    if (mixed) return;
    pushBrush(entity, context, brush);
  };

  let patchDepth = 1;
  const patchBlock = (context: Context, start: Token): Quake3Patch | undefined => {
    patchDepth = 1;
    if (!expect('{', context, 'patchDef2')) return undefined;
    patchDepth = 2;
    const name = shaderName(context, 'patchDef2', true);
    if (name === undefined) return undefined;
    const header = vector(5, context, 'the patchDef2 header');
    if (!header) return undefined;
    const [width, height] = header;
    if (!Number.isInteger(width) || !Number.isInteger(height) || width < 3 || height < 3 || width > 31 || height > 31 || width % 2 === 0 || height % 2 === 0) {
      errors.push(error(QUAKE3_PARSE_CODES.patchSize,
                        `a ${width}x${height} patch is outside APMap 1.5's grid: each dimension must be odd, 3 to 31`, start.line, start.column, context.entityIndex, context.brushIndex));
      return undefined;
    }
    if (!expect('(', context, 'the patchDef2 control grid')) return undefined;
    const columns: number[][][] = [];
    for (let c = 0; c < width; c += 1) {
      if (!expect('(', context, `patch column ${c}`)) return undefined;
      const rows: number[][] = [];
      for (let r = 0; r < height; r += 1) {
        const point = vector(5, context, `patch point ${c},${r}`);
        if (!point) return undefined;
        rows.push(point);
      }
      if (!expect(')', context, `patch column ${c}`)) return undefined;
      columns.push(rows);
    }
    if (!expect(')', context, 'the patchDef2 control grid')) return undefined;
    if (!expect('}', context, 'patchDef2')) return undefined;
    patchDepth = 1;
    return { kind: 'patch', texture: name, width, height, tail: header.slice(2), controlPoints: columns, line: start.line, column: start.column };
  };

  const pushBrush = (entity: Quake3Entity, context: Context, brush: Quake3Brush): void => {
    if (brush.faces.length < 4) {
      errors.push(error(QUAKE3_PARSE_CODES.tooFewFaces, `a brush needs at least four faces, this one has ${brush.faces.length}`, brush.line, brush.column, context.entityIndex, context.brushIndex));
      return;
    }
    entity.content.push(brush);
    stats.brushes += 1;
    stats.faces += brush.faces.length;
    if (brush.syntax === 'classic') stats.classicBrushes += 1;
    else if (brush.syntax === 'valve220') stats.valve220Brushes += 1;
    else stats.brushPrimitiveBrushes += 1;
  };

  for (;;) {
    const token = lexer.next();
    if (!token) break;
    if (token.quoted || token.text !== '{') {
      errors.push(error(QUAKE3_PARSE_CODES.contentOutsideEntity, 'content appears outside an entity', token.line, token.column));
      // Resynchronise on the next top-level brace.
      for (let peek = lexer.peek(); peek && !(peek.text === '{' && !peek.quoted); peek = lexer.peek()) lexer.next();
      continue;
    }
    const entity: Quake3Entity = { index: out.entities.length, content: [], line: token.line, column: token.column };
    out.entities.push(entity);
    stats.entities += 1;
    let brushIndex = 0;
    for (;;) {
      const inner = lexer.next();
      if (!inner) { errors.push(error(QUAKE3_PARSE_CODES.unexpectedEnd, 'an entity is not closed', entity.line, entity.column, entity.index)); break; }
      if (!inner.quoted && inner.text === '}') break;
      if (!inner.quoted && inner.text === '{') {
        brushBlock(entity, { entityIndex: entity.index, brushIndex }, inner);
        brushIndex += 1;
        continue;
      }
      if (!inner.quoted) {
        errors.push(error(QUAKE3_PARSE_CODES.invalidProperty, 'expected a quoted entity key or a brush brace', inner.line, inner.column, entity.index));
        continue;
      }
      const value = lexer.next(true);
      if (!value || !value.quoted) {
        errors.push(error(QUAKE3_PARSE_CODES.invalidProperty, 'an entity key must be followed by a quoted value on the same line', inner.line, inner.column, entity.index));
        if (value && !value.quoted && value.text === '}') break;
        continue;
      }
      entity.content.push({ kind: 'property', key: inner.text, value: value.text, line: inner.line, column: inner.column });
      stats.properties += 1;
    }
    yield lexer.offset;
  }
}

const emptyStats = (bytes: number): Quake3MapStats =>
  ({ bytes, entities: 0, properties: 0, brushes: 0, classicBrushes: 0, valve220Brushes: 0, brushPrimitiveBrushes: 0, faces: 0, patches: 0 });

/** Read a whole Quake III map synchronously. */
export function parseQuake3Map(text: string): Quake3ParsedMap {
  const out: Quake3ParsedMap = { entities: [], errors: [], stats: emptyStats(text.length) };
  for (const _ of parse(text, out)) { /* run to completion */ }
  return out;
}

export type Quake3ParseOptions = {
  signal?: AbortSignal;
  /** `consumed` of `total` characters. Called after each slice. */
  onProgress?: (consumed: number, total: number) => void;
  /** Work between yields, in milliseconds. Default 12 — under one frame. */
  sliceMs?: number;
  /** The yield. Injected by tests; `setTimeout(0)` by default so a pending abort can land. */
  yieldTo?: () => Promise<void>;
};

const abortError = () => (typeof DOMException === 'function' ? new DOMException('Quake III map import cancelled', 'AbortError')
  : Object.assign(new Quake3ParseAbort('Quake III map import cancelled'), { name: 'AbortError' }));

/** Read a Quake III map without holding the thread for more than a slice at a time. */
export async function parseQuake3MapAsync(text: string, options: Quake3ParseOptions = {}): Promise<Quake3ParsedMap> {
  const out: Quake3ParsedMap = { entities: [], errors: [], stats: emptyStats(text.length) };
  const slice = options.sliceMs ?? 12;
  const pause = options.yieldTo ?? (() => new Promise<void>((resolve) => setTimeout(resolve, 0)));
  const now = () => (typeof performance !== 'undefined' ? performance.now() : Date.now());
  if (options.signal?.aborted) throw abortError();
  let started = now();
  for (const offset of parse(text, out)) {
    if (now() - started < slice) continue;
    options.onProgress?.(offset, text.length);
    await pause();
    if (options.signal?.aborted) throw abortError();
    started = now();
  }
  options.onProgress?.(text.length, text.length);
  return out;
}
