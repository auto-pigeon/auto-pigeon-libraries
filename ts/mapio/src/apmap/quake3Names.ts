/**
 * The Quake III names the codec and the writer share — `Q3_002`. A module with no imports, so the
 * `.map` writer can read them without pulling the importer (and the legality checker behind it) in.
 */

/** SEMANTICS §2: the one `game` and `map_dialect` pair that means the Quake III grammar. */
export const QUAKE3_GAME_NAME = 'quake3';
export const QUAKE3_MAP_DIALECT = 'quake3_extended';

/** The prefix Q3Map2 adds to every shader name it reads from a `.map`. Written into none. */
export const QUAKE3_SHADER_PREFIX = 'textures/';

/** A shader name as a Quake III `.map` must spell it: one leading `textures/` removed, case kept. */
export const quake3MapShaderName = (name: string): string =>
  name.toLowerCase().startsWith(QUAKE3_SHADER_PREFIX) ? name.slice(QUAKE3_SHADER_PREFIX.length) : name;

/** The longest shader name Quake III holds (`MAX_QPATH` 64, with its terminator). */
export const QUAKE3_MAX_SHADER_NAME = 63;

/**
 * **A logical path a script or a face may name** — relative, forward or back slashes, no empty, `.`
 * or `..` segment, no drive, no control character, no space, no quote, at most
 * {@link QUAKE3_MAX_SHADER_NAME} characters. `Q3_006`'s rule (*validate names at ingestion*), stated
 * once here so the shader reader (`domain/q3Shaders.ts`) and model import's material resolution
 * (`mesh/materials.ts`, `NEW_320`) cannot come to disagree about what a shader name is.
 */
export function validQuake3LogicalPath(path: string): boolean {
  if (!path || path.length > QUAKE3_MAX_SHADER_NAME) return false;
  if (/[\u0000- "\u007f]/.test(path) || /^[a-zA-Z]:/.test(path)) return false;
  const normal = path.replace(/\\/g, '/');
  if (normal.startsWith('/')) return false;
  return normal.split('/').every((segment) => segment !== '' && segment !== '.' && segment !== '..');
}

/** Lower-case, forward slashes, no leading `textures/` — how two spellings of one shader are compared. */
export const quake3ShaderKey = (name: string): string =>
  quake3MapShaderName(name.trim().replace(/\\/g, '/')).toLowerCase();

/**
 * **Special face names are per game** (`Q3_002`). Quake 1 names its compiler tool textures bare —
 * `skip`, `clip`, `trigger`, `hint`, `origin` — and Quake III names them as shaders in `common/`
 * (`common/caulk`, `common/clip`, `common/trigger`, `common/hint`, `common/origin`), which Q3Map2
 * reads as `textures/common/…`. A bare Quake 1 name in a Quake III map is therefore not a tool at
 * all: Q3Map2 looks for `textures/skip`, finds nothing, and draws the face.
 *
 * The writer does not rename — the Quake 1 and Quake III tools do not mean the same things (`skip`
 * deletes a face, `common/caulk` seals it unseen) and a silent replacement is the loss this product
 * refuses. This names the faces, so the editor can say so before the file leaves.
 */
export const QUAKE1_TOOL_TEXTURES: ReadonlySet<string> = new Set(['skip', 'clip', 'trigger', 'hint', 'hintskip', 'origin', 'null', 'aaatrigger']);

export function quake3BareToolFaces(document: { entities?: unknown }): { count: number; names: string[] } {
  const names = new Set<string>();
  let count = 0;
  for (const entity of Array.isArray(document.entities) ? document.entities : [])
    for (const item of Array.isArray((entity as { content?: unknown })?.content) ? (entity as { content: unknown[] }).content : [])
      if ((item as { kind?: unknown })?.kind === 'brush')
        for (const face of (item as { faces?: { texture?: unknown }[] }).faces ?? []) {
          const texture = typeof face?.texture === 'string' ? face.texture : '';
          if (QUAKE1_TOOL_TEXTURES.has(texture.toLowerCase())) { count += 1; names.add(texture.toLowerCase()); }
        }
  return { count, names: [...names].sort() };
}
