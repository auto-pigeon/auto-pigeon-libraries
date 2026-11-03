/**
 * **The group graph** — APMap 1.4's nesting, in one place.
 *
 * 1.4 lets a group name another group (`{ kind: 'group', group_id }`). The graph that makes is a
 * forest: each group has at most ONE parent, there is no cycle, and no chain from a root to its
 * deepest descendant holds more than {@link GROUP_MAX_DEPTH} groups. `SEMANTICS.md` §9a of
 * `@auto-pigeon/apmap-schema` is the authority; this module is the kernel's reading of it, and the
 * editor asks it every structural question about nesting rather than walking members itself.
 *
 * ### Everything here is iterative, with a visited set
 *
 * A document is not a trusted channel. A hostile file may declare twenty thousand groups in one
 * chain or a cycle of any length, and the answer must be a named rule — SEM-G-8 or SEM-G-9 — never
 * a stack overflow in a Web Worker. So no function below recurses, every walk carries a visited set,
 * and the fault pass is linear in the number of members.
 *
 * ### Expansion is deterministic and de-duplicated
 *
 * The objects a group stands for are its object members plus the expansion of each child, in member
 * order, children expanded depth-first where they are listed. An object reached twice is reached
 * once, so a transform applied to an expanded group moves each brush exactly once.
 */
import type { ApmapDocument, ApmapGroup, ApmapGroupMember } from './types.ts';

/** The deepest a document may nest groups, counted in groups from a root to its deepest descendant. */
export const GROUP_MAX_DEPTH = 32;

/** The id a member names — an object id for an object member, a `group_id` for a group member. */
export const groupMemberId = (member: ApmapGroupMember): string =>
  member.kind === 'entity' ? member.entity_id : member.kind === 'brush' ? member.brush_id
    : member.kind === 'face' ? member.face_id : member.kind === 'patch' ? member.patch_id : member.group_id;

export type ApmapObjectMember = Exclude<ApmapGroupMember, { kind: 'group' }>;
export const isObjectMember = (member: ApmapGroupMember): member is ApmapObjectMember => member.kind !== 'group';

/**
 * The one parent of every group and every directly grouped object, keyed by the member's id.
 *
 * In a valid document each id appears at most once (SEM-G-4). When one appears twice the FIRST
 * listing wins here, so a caller walking a faulty document still terminates; `groupGraphFaults`
 * is what reports the fault.
 */
export function groupParents(groups: readonly ApmapGroup[]): Map<string, string> {
  const parent = new Map<string, string>();
  for (const group of groups)
    for (const member of group.members) {
      const id = groupMemberId(member);
      if (!parent.has(id)) parent.set(id, group.group_id);
    }
  return parent;
}

/**
 * A group and every group above it, innermost first — `[C, P, Root]`. Bounded by a visited set, so
 * a cycle ends the walk instead of spinning.
 */
export function groupAncestry(parents: ReadonlyMap<string, string>, groupId: string): string[] {
  const chain: string[] = [];
  const seen = new Set<string>();
  for (let at: string | undefined = groupId; at !== undefined && !seen.has(at); at = parents.get(at)) {
    seen.add(at);
    chain.push(at);
  }
  return chain;
}

/** The root of the tree `groupId` is in: the last entry of its ancestry. */
export const rootGroupOf = (parents: ReadonlyMap<string, string>, groupId: string): string => {
  const chain = groupAncestry(parents, groupId);
  return chain[chain.length - 1];
};

/**
 * The OBJECT members a group stands for — direct and through every descendant group — in
 * deterministic order, each object once. A child group that does not exist, or one already visited
 * (a cycle), contributes nothing rather than looping.
 */
export function expandGroup(groups: readonly ApmapGroup[] | ReadonlyMap<string, ApmapGroup>, groupId: string): ApmapObjectMember[] {
  const byId = groups instanceof Map ? groups : new Map((groups as readonly ApmapGroup[]).map((group) => [group.group_id, group]));
  const out: ApmapObjectMember[] = [];
  const seenObjects = new Set<string>();
  const visited = new Set<string>();
  // An explicit stack of (group, next member index) frames: depth-first, in member order, iterative.
  const stack: Array<{ group: ApmapGroup; at: number }> = [];
  const enter = (id: string) => {
    const group = byId.get(id);
    if (!group || visited.has(id)) return;
    visited.add(id);
    stack.push({ group, at: 0 });
  };
  enter(groupId);
  while (stack.length) {
    const frame = stack[stack.length - 1];
    if (frame.at >= frame.group.members.length) { stack.pop(); continue; }
    const member = frame.group.members[frame.at++];
    if (member.kind === 'group') { enter(member.group_id); continue; }
    const id = groupMemberId(member);
    if (seenObjects.has(id)) continue;
    seenObjects.add(id);
    out.push(member);
  }
  return out;
}

/** Every group below `groupId` (not including it), in the same deterministic order. */
export function descendantGroupIds(groups: readonly ApmapGroup[] | ReadonlyMap<string, ApmapGroup>, groupId: string): string[] {
  const byId = groups instanceof Map ? groups : new Map((groups as readonly ApmapGroup[]).map((group) => [group.group_id, group]));
  const out: string[] = [];
  const visited = new Set<string>([groupId]);
  const stack = [groupId];
  while (stack.length) {
    const group = byId.get(stack.pop()!);
    if (!group) continue;
    const children = group.members.filter((member: ApmapGroupMember): member is { kind: 'group'; group_id: string } => member.kind === 'group');
    for (let i = children.length - 1; i >= 0; i -= 1) {
      const id = children[i].group_id;
      if (visited.has(id)) continue;
      visited.add(id);
      stack.push(id);
    }
    if (group.group_id !== groupId) out.push(group.group_id);
  }
  return out;
}

export type GroupGraphFault = { rule: string; message: string; path: string };

/**
 * Every group-graph and authorship fault a document has — SEM-G-1 … SEM-G-9 and SEM-A-1, as
 * `SEMANTICS.md` §11 numbers them, with a JSON Pointer to where each one is.
 *
 * `objectKinds` answers "is this id declared, and as what" for object members; when it is omitted
 * the document's entities are indexed here. Schema validation is assumed to have run already —
 * this is the part the schema cannot see. Linear, iterative, and bounded by the caller's own issue
 * limit rather than here.
 */
export function groupGraphFaults(
  document: Pick<ApmapDocument, 'groups' | 'entities' | 'authorship'>,
  objectKinds?: ReadonlyMap<string, { kind: 'entity' | 'brush' | 'face' | 'patch'; owner?: string }>,
): GroupGraphFault[] {
  const faults: GroupGraphFault[] = [];
  const objects = objectKinds ?? indexObjects(document);
  const groups = Array.isArray(document.groups) ? document.groups : [];
  const byId = new Map<string, ApmapGroup>();
  groups.forEach((group, index) => {
    if (byId.has(group.group_id))
      faults.push({ rule: 'SEM-G-3', message: `SEM-G-3: group_id ${group.group_id} is declared twice`, path: `/groups/${index}/group_id` });
    else byId.set(group.group_id, group);
  });

  const parentOf = new Map<string, string>();
  groups.forEach((group, groupIndex) => {
    (Array.isArray(group.members) ? group.members : []).forEach((member, memberIndex) => {
      const id = groupMemberId(member);
      const path = `/groups/${groupIndex}/members/${memberIndex}`;
      if (member.kind === 'group') {
        if (!byId.has(id)) faults.push({ rule: 'SEM-G-1', message: `SEM-G-1: ${group.group_id} names group ${id}, which is not declared`, path });
        if (id === group.group_id) faults.push({ rule: 'SEM-G-8', message: `SEM-G-8: ${id} is a member of itself`, path });
      } else {
        const object = objects.get(id);
        if (!object) faults.push({ rule: 'SEM-G-1', message: `SEM-G-1: ${group.group_id} names ${member.kind} ${id}, which is not declared`, path });
        else if (object.kind !== member.kind)
          faults.push({ rule: 'SEM-G-2', message: `SEM-G-2: ${group.group_id} names ${id} as ${member.kind}, but it is a ${object.kind}`, path });
      }
      const previous = parentOf.get(id);
      if (previous !== undefined && previous !== group.group_id)
        faults.push({ rule: 'SEM-G-4', message: `SEM-G-4: ${id} is a direct member of both ${previous} and ${group.group_id}`, path });
      else if (previous === undefined) parentOf.set(id, group.group_id);
    });
  });

  // Depth and cycles: walk each group's parent chain, memoized so the whole pass is linear.
  const depthOf = new Map<string, number>();
  const rootOf = new Map<string, string | null>();
  for (const start of byId.keys()) {
    const chain: string[] = [];
    const seen = new Set<string>();
    let at: string | undefined = start;
    let cyclic = false;
    while (at !== undefined && !depthOf.has(at)) {
      if (seen.has(at)) {
        cyclic = true;
        if (at !== start || chain.length > 1)
          faults.push({ rule: 'SEM-G-8', message: `SEM-G-8: the group graph has a cycle through ${at}`, path: '/groups' });
        break;
      }
      seen.add(at);
      chain.push(at);
      at = parentOf.get(at);
    }
    let depth = at !== undefined && depthOf.has(at) ? depthOf.get(at)! : 0;
    const root = cyclic ? null : at !== undefined ? rootOf.get(at) ?? null : chain[chain.length - 1];
    for (let i = chain.length - 1; i >= 0; i -= 1) {
      depthOf.set(chain[i], ++depth);
      rootOf.set(chain[i], root);
    }
  }
  let deepest = 0;
  for (const depth of depthOf.values()) if (depth > deepest) deepest = depth;
  if (deepest > GROUP_MAX_DEPTH)
    faults.push({ rule: 'SEM-G-9', message: `SEM-G-9: groups nest ${deepest} deep; at most ${GROUP_MAX_DEPTH} is allowed`, path: '/groups' });

  // Containment across a whole tree: an object and one of its own ancestor objects must not both be
  // direct members anywhere in one tree, because the root's expansion would name them twice.
  for (const [id, group] of parentOf) {
    const object = objects.get(id);
    if (!object) continue;
    for (let up = object.owner; up; up = objects.get(up)?.owner) {
      const other = parentOf.get(up);
      if (other === undefined) continue;
      const a = rootOf.get(group), b = rootOf.get(other);
      if (a && a === b)
        faults.push({ rule: 'SEM-G-5', message: `SEM-G-5: ${a} reaches both ${up} and its descendant ${id}`, path: '/groups' });
    }
  }

  // SEM-A-1: authorship text is Unicode. A lone surrogate survives JSON.parse unnoticed.
  const authorship = document.authorship;
  if (authorship && typeof authorship === 'object')
    for (const [field, value] of Object.entries(authorship))
      if (typeof value === 'string' && !isWellFormed(value))
        faults.push({ rule: 'SEM-A-1', message: `SEM-A-1: authorship.${field} contains an unpaired surrogate`, path: `/authorship/${field}` });
  return faults;
}

const LONE_SURROGATE = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/;
const isWellFormed = (value: string): boolean => !LONE_SURROGATE.test(value);

function indexObjects(document: Pick<ApmapDocument, 'entities'>): Map<string, { kind: 'entity' | 'brush' | 'face' | 'patch'; owner?: string }> {
  const objects = new Map<string, { kind: 'entity' | 'brush' | 'face' | 'patch'; owner?: string }>();
  for (const entity of Array.isArray(document.entities) ? document.entities : []) {
    objects.set(entity.entity_id, { kind: 'entity' });
    for (const item of Array.isArray(entity.content) ? entity.content : []) {
      // APMap 1.5: a patch is an object its entity owns, exactly as a brush is (SEM-G-5 reaches it).
      if (item?.kind === 'patch') { objects.set(item.patch_id, { kind: 'patch', owner: entity.entity_id }); continue; }
      if (item?.kind !== 'brush') continue;
      objects.set(item.brush_id, { kind: 'brush', owner: entity.entity_id });
      for (const face of Array.isArray(item.faces) ? item.faces : []) objects.set(face.face_id, { kind: 'face', owner: item.brush_id });
    }
  }
  return objects;
}
