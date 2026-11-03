/**
 * The text-level pass of the `json-parse` stage: everything about an APMap file that is true of its
 * bytes and lost the moment `JSON.parse` succeeds.
 *
 * `JSON.parse` is a yes/no oracle. It says nothing about *which* two members collided when a key is
 * duplicated (it silently keeps the last), and it cannot report anything at all about a file it
 * rejects beyond one position. Both matter here: APMap's serialization rules (SER-1 … SER-9) are
 * part of the contract, and a duplicate key is a `sneaky` fault precisely because every parser
 * accepts it and they disagree about which value wins.
 *
 * This is a tolerant lexer, not a parser. It never decides a document is valid — that is
 * `JSON.parse`'s job, immediately after — and it never throws: on anything it does not recognise it
 * advances one character and keeps looking. Its container stack is best-effort, which is enough to
 * group sibling keys and to build a JSON Pointer for a duplicate.
 */
import { ApmapIssues, issueValue, pointer, type ApmapIssue } from '../issues.ts';

/** Where a character sits in the file, for a fault that has no decoded path to name. */
export function lineColumn(text: string, index: number): { line: number; column: number } {
  const clamped = Math.max(0, Math.min(index, text.length));
  const before = text.slice(0, clamped), line = before.split('\n').length;
  return { line, column: clamped - (before.lastIndexOf('\n') + 1) + 1 };
}

type Frame = { object: boolean; keys: Set<string>; index: number; path: string; key: string | null };

const isWhitespace = (code: number) => code === 0x20 || code === 0x09 || code === 0x0a || code === 0x0d;
const WORD = /[A-Za-z_$]/;

/**
 * Serialization and lexical faults. Everything reported here is independent of whether the document
 * subsequently parses, so the scan runs first and its findings survive a fatal parse failure.
 */
export function scanApmapText(text: string, issues: ApmapIssues): void {
  const add = (issue: ApmapIssue, kind: string) => issues.add(issue, `json-parse:${kind}`);
  const at = (index: number) => { const { line, column } = lineColumn(text, index); return `line ${line} column ${column}`; };

  if (text.length === 0) {
    add({ stage: 'json-parse', severity: 'fatal', message: 'The APMap file is empty', path: null, value: '', woundId: 'structural:empty-file' }, 'empty');
    return;
  }
  if (text.charCodeAt(0) === 0xfeff)
    add({ stage: 'json-parse', severity: 'fatal', message: 'The APMap file begins with a UTF-8 byte order mark, which SER-1 forbids and JSON does not admit', path: null, value: 'U+FEFF', woundId: 'json:byte-order-mark' }, 'bom');
  // Read as UTF-8 text, gzip's second magic byte (0x8b) is not valid UTF-8 and arrives as U+FFFD;
  // read as bytes it is itself. Both spellings are the same file.
  if (text.charCodeAt(0) === 0x1f && (text.charCodeAt(1) === 0x8b || text.charCodeAt(1) === 0xfffd))
    add({ stage: 'json-parse', severity: 'fatal', message: 'The APMap file begins with gzip magic bytes; APMap 1.0 defines no compressed encoding', path: null, value: '1f 8b', woundId: 'structural:gzip-without-hint' }, 'gzip');
  if (text.includes('�'))
    add({ stage: 'json-parse', severity: 'fatal', message: 'The APMap file contains bytes that are not valid UTF-8', path: null, value: 'U+FFFD replacement character', woundId: 'encoding:invalid-utf8-value' }, 'utf8');
  // Quake `.map` text under an `.apmap` name: a brace-delimited body whose faces are plane triples
  // in parentheses. Neither construct can appear at the top level of a JSON document.
  if (/^\s*(?:\/\/[^\n]*\n\s*)*\{/.test(text) && /^\s*\(\s*-?[\d.]+\s+-?[\d.]+\s+-?[\d.]+\s*\)/m.test(text))
    add({ stage: 'json-parse', severity: 'fatal', message: 'The file is Quake .map text saved under an .apmap name; APMap is never converted to or from .map', path: null, value: text.slice(0, 60), woundId: 'structural:map-file-wrong-extension' }, 'maptext');
  if (!text.endsWith('\n'))
    add({ stage: 'json-parse', severity: 'warning', message: 'The APMap file has no trailing newline, which SER-4 requires', path: null, value: null, woundId: 'structural:no-trailing-newline' }, 'newline');
  if (text.includes('\r'))
    add({ stage: 'json-parse', severity: 'warning', message: 'The APMap file contains CRLF line endings; SER-2 requires LF', path: null, value: null, woundId: 'encoding:mixed-line-endings' }, 'crlf');
  if (/^\t/m.test(text))
    add({ stage: 'json-parse', severity: 'warning', message: 'The APMap file is indented with tabs; SER-3 requires two spaces', path: null, value: null, woundId: 'structural:tab-indentation' }, 'tabs');

  const frames: Frame[] = [];
  const childPath = (): string => {
    const frame = frames[frames.length - 1];
    if (!frame) return '';
    return frame.object ? pointer(frame.path, frame.key ?? '?') : pointer(frame.path, frame.index);
  };
  let index = 0, closed = -1;
  const length = text.length;

  const readString = (start: number): { value: string; end: number } => {
    let cursor = start + 1, value = '', escapedNonAscii = false, nul = false, control = -1;
    while (cursor < length) {
      const code = text.charCodeAt(cursor);
      if (code === 0x22) break;
      if (code === 0x5c) {
        const next = text[cursor + 1];
        if (next === 'u') {
          const hex = text.slice(cursor + 2, cursor + 6), point = Number.parseInt(hex, 16);
          if (Number.isFinite(point)) {
            if (point === 0) nul = true;
            if (point > 0x7f) escapedNonAscii = true;
            value += String.fromCharCode(point);
          }
          cursor += 6;
          continue;
        }
        value += next ?? '';
        cursor += 2;
        continue;
      }
      if (code < 0x20 && control < 0) control = cursor;
      value += text[cursor];
      cursor += 1;
    }
    if (control >= 0)
      add({ stage: 'json-parse', severity: 'fatal', message: `A raw control character appears inside a string at ${at(control)}; JSON requires it escaped`, path: childPath() || null, value: `U+${text.charCodeAt(control).toString(16).padStart(4, '0').toUpperCase()}`, woundId: 'json:unescaped-control-char' }, 'control');
    if (escapedNonAscii)
      add({ stage: 'json-parse', severity: 'warning', message: `A non-ASCII character is written as a \\u escape at ${at(start)}; SER-9 requires literal UTF-8`, path: childPath() || null, value: issueValue(value), woundId: 'encoding:unicode-escaped-non-ascii' }, 'uescape');
    if (nul)
      add({ stage: 'json-parse', severity: 'error', message: `A string contains an embedded NUL at ${at(start)}, which truncates a C-string consumer`, path: childPath() || null, value: issueValue(value.replace(/\0/g, '\\u0000')), woundId: 'encoding:embedded-null-byte' }, 'nul');
    return { value, end: cursor };
  };

  while (index < length) {
    const code = text.charCodeAt(index);
    if (isWhitespace(code)) {
      if (closed >= 0) { index += 1; continue; }
      index += 1;
      continue;
    }
    if (closed >= 0) {
      add({ stage: 'json-parse', severity: 'fatal', message: `Non-JSON bytes follow the end of the document at ${at(index)}`, path: null, value: issueValue(text.slice(index, index + 60)), woundId: 'json:trailing-garbage' }, 'garbage');
      return;
    }
    const character = text[index];
    if (character === '"') {
      const { value, end } = readString(index);
      let after = end + 1;
      while (after < length && isWhitespace(text.charCodeAt(after))) after += 1;
      const frame = frames[frames.length - 1];
      if (text[after] === ':' && frame?.object) {
        if (frame.keys.has(value))
          add({ stage: 'json-parse', severity: 'error', message: `The member ${JSON.stringify(value)} appears more than once in one object at ${at(index)}; JSON parsers disagree about which value wins`, path: pointer(frame.path, value), value: issueValue(value), woundId: 'json:duplicate-keys' }, 'duplicate-key');
        frame.keys.add(value);
        frame.key = value;
      }
      index = end + 1;
      continue;
    }
    if (character === "'") {
      add({ stage: 'json-parse', severity: 'fatal', message: `A single-quoted token appears at ${at(index)}; JSON admits double quotes only`, path: childPath() || null, value: issueValue(text.slice(index, index + 40)), woundId: 'json:single-quotes' }, 'single-quote');
      const end = text.indexOf("'", index + 1);
      index = end < 0 ? length : end + 1;
      continue;
    }
    if (character === '/') {
      const block = text[index + 1] === '*';
      add({ stage: 'json-parse', severity: 'fatal', message: `A comment appears at ${at(index)}; JSON admits no comments`, path: null, value: issueValue(text.slice(index, index + 40)), woundId: 'json:comment-present' }, 'comment');
      const end = block ? text.indexOf('*/', index + 2) : text.indexOf('\n', index);
      index = end < 0 ? length : end + (block ? 2 : 1);
      continue;
    }
    if (character === '{' || character === '[') {
      frames.push({ object: character === '{', keys: new Set(), index: 0, path: childPath(), key: null });
      index += 1;
      continue;
    }
    if (character === '}' || character === ']') {
      frames.pop();
      if (frames.length === 0) closed = index;
      index += 1;
      continue;
    }
    if (character === ',') {
      const frame = frames[frames.length - 1];
      if (frame && !frame.object) frame.index += 1;
      if (frame) frame.key = null;
      index += 1;
      continue;
    }
    if (WORD.test(character)) {
      let end = index;
      while (end < length && /[A-Za-z_$0-9]/.test(text[end])) end += 1;
      const word = text.slice(index, end);
      if (word === 'NaN' || word === 'Infinity')
        add({ stage: 'json-parse', severity: 'fatal', message: `The bare token ${word} appears at ${at(index)}; JSON has no such literal and SEM-11 forbids a non-finite number`, path: childPath() || null, value: word, woundId: 'json:nan-infinity-literal' }, 'nan-literal');
      index = end;
      continue;
    }
    if (character === '-' && text.startsWith('-Infinity', index)) {
      add({ stage: 'json-parse', severity: 'fatal', message: `The bare token -Infinity appears at ${at(index)}; JSON has no such literal and SEM-11 forbids a non-finite number`, path: childPath() || null, value: '-Infinity', woundId: 'json:nan-infinity-literal' }, 'nan-literal');
      index += 9;
      continue;
    }
    if (character === '-' || (code >= 0x30 && code <= 0x39)) {
      let end = index;
      while (end < length && /[-+.eE0-9]/.test(text[end])) end += 1;
      const token = text.slice(index, end);
      if (/^-?0\d/.test(token))
        add({ stage: 'json-parse', severity: 'fatal', message: `The number ${token} at ${at(index)} has a leading zero, which JSON forbids`, path: childPath() || null, value: token, woundId: 'numeric:leading-zero-invalid-number' }, 'leading-zero');
      index = end;
      continue;
    }
    index += 1;
  }
}
