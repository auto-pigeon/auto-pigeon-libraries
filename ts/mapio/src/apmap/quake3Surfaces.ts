/**
 * **The Quake III surfaces of APMap 1.5, as this kernel answers for them** — `Q3_001`.
 *
 * APMap 1.5 (`auto-pigeon-libraries/ts/apmap-schema/SEMANTICS.md` §6.2a, §9c) gives a document two
 * things this editor cannot draw or edit yet:
 *
 * - a **patch** — an entity content item holding a patchDef2 bezier control grid, a surface and
 *   never a solid;
 * - a **brush-primitives face** — a brushDef face whose texture is a 2x3 matrix rather than
 *   shift/rotation/scale.
 *
 * What the kernel does with each is decided here and nowhere else:
 *
 * | surface | read | edit | write |
 * | --- | --- | --- | --- |
 * | patch | decoded, **not drawn**, carried verbatim | refused by the editor's surface hold | canonical, SER-7Q |
 * | brush-primitives face | **refused at the decode door, by name** | — | canonical, SER-7Q |
 *
 * A patch can be carried because it is self-contained: nothing else in the document depends on its
 * numbers, so a document can be opened, saved and exported with every patch exactly as it was read.
 * A brush-primitives face cannot, because the brush it belongs to is drawn, picked and edited like
 * any other and every one of those paths would have to invent a classic projection for it — which
 * `test/quake3.test.mjs` in AULIBS proves is not possible without the image size, and impossible
 * with shear. Q3_002 lifts the second refusal, Q3_003 the first hold.
 *
 * The semantic rules SEM-Q-1..5 are checked here too, and report `{ rule, message, path }` in the
 * shape `groupGraph.ts` uses, so `validate.ts` reports both the same way.
 */
import type { ApmapDocument, ApmapEntity, ApmapPatchItem } from './types.ts';

export type Quake3SurfaceFault = { rule: string; message: string; path: string };

/** The one game in which a patch or a brush-primitives face may appear (SEM-Q-4). */
export const QUAKE3_GAME = 'quake3';

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);
const asArray = (value: unknown): unknown[] => (Array.isArray(value) ? value : []);

/** Every patch in the document, in document order, with the entity that owns it. */
export function apmapPatches(document: Pick<ApmapDocument, 'entities'>): { entity: ApmapEntity; patch: ApmapPatchItem }[] {
  const found: { entity: ApmapEntity; patch: ApmapPatchItem }[] = [];
  for (const entity of asArray(document.entities) as ApmapEntity[])
    for (const item of asArray(entity?.content))
      if (isObject(item) && item.kind === 'patch') found.push({ entity, patch: item as unknown as ApmapPatchItem });
  return found;
}

/** How many patches the document holds. What the editor's surface hold counts. */
export const apmapPatchCount = (document: Pick<ApmapDocument, 'entities'>): number => apmapPatches(document).length;

/**
 * SEM-Q-1 … SEM-Q-5 over a decoded document, schema validation assumed to have run.
 *
 * Defensive about shape — a wounded document is an input — and linear: one walk of the entities.
 */
export function quake3SurfaceFaults(document: Record<string, unknown> | Pick<ApmapDocument, 'entities' | 'game'>): Quake3SurfaceFault[] {
  const faults: Quake3SurfaceFault[] = [];
  const game = (document as { game?: unknown }).game;
  const quake3 = game === QUAKE3_GAME;
  const patchIds = new Set<string>();
  asArray((document as { entities?: unknown }).entities).forEach((entity, entityIndex) => {
    if (!isObject(entity)) return;
    asArray(entity.content).forEach((item, position) => {
      if (!isObject(item)) return;
      const path = `/entities/${entityIndex}/content/${position}`;
      if (item.kind === 'patch') {
        const id = typeof item.patch_id === 'string' ? item.patch_id : '';
        if (id && patchIds.has(id))
          faults.push({ rule: 'SEM-Q-1', message: `SEM-Q-1: patch_id ${id} is declared more than once`, path: `${path}/patch_id` });
        if (id) patchIds.add(id);
        const columns = asArray(item.control_points);
        if (columns.length !== item.width || columns.some((column) => asArray(column).length !== item.height))
          faults.push({ rule: 'SEM-Q-2', message: `SEM-Q-2: patch ${id} declares a ${String(item.width)}x${String(item.height)} grid `
            + `but holds ${columns.length} columns of ${[...new Set(columns.map((column) => asArray(column).length))].join('/')} points`,
                        path: `${path}/control_points` });
        if (!quake3)
          faults.push({ rule: 'SEM-Q-4', message: `SEM-Q-4: patch ${id} is a Quake III surface in a ${String(game)} document`, path });
        return;
      }
      if (item.kind !== 'brush') return;
      const faces = asArray(item.faces);
      let matrices = 0;
      faces.forEach((face, faceIndex) => {
        if (!isObject(face) || !isObject(face.projection) || face.projection.mode !== 'brush_primitives') return;
        matrices += 1;
        const facePath = `${path}/faces/${faceIndex}`;
        const matrix = asArray(face.projection.matrix).map(asArray);
        const [a, b] = (matrix[0] ?? []) as number[], [d, e] = (matrix[1] ?? []) as number[];
        if (typeof a === 'number' && typeof b === 'number' && typeof d === 'number' && typeof e === 'number' && a * e - b * d === 0)
          faults.push({ rule: 'SEM-Q-3', message: `SEM-Q-3: face ${String(face.face_id)} has a degenerate texture matrix`, path: `${facePath}/projection/matrix` });
        if (!quake3)
          faults.push({ rule: 'SEM-Q-4', message: `SEM-Q-4: face ${String(face.face_id)} has a brush_primitives projection in a ${String(game)} document`, path: `${facePath}/projection` });
      });
      if (matrices > 0 && matrices < faces.length)
        faults.push({ rule: 'SEM-Q-5', message: `SEM-Q-5: brush ${String(item.brush_id)} mixes brush_primitives faces with another projection mode`, path });
    });
  });
  return faults;
}
