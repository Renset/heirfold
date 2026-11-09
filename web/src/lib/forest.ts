import type { GraphIndex } from "./graph";
import { type Chart, type ChartLink, type ChartNode, NODE_H, NODE_W } from "./layout";

// The layout of a whole tree on one canvas: a person and everybody who is
// linked to that person through parents, children or partners, with all the
// links. The generations stand in rows, with partners side by side.
// The functions are pure, so tests check them without a browser.

const UNIT_GAP = 28;
const SPOUSE_GAP = 12;
const ROW_GAP = 96;
/** The partners that are not neighbours get a line below the cards. */
const BRACKET = 14;
const SWEEPS = 5;
const PASSES = 8;

/** A whole tree, and the number of people outside it. */
export interface WholeTree extends Chart {
  others: number;
}

interface Family {
  id: string;
  parents: string[];
  children: string[];
}

interface Unit {
  members: string[];
  /** The middle of the unit. */
  x: number;
  layer: number;
}

class Sets {
  private parent = new Map<string, string>();

  find(id: string): string {
    let root = id;
    for (
      let next = this.parent.get(root);
      next !== undefined && next !== root;
      next = this.parent.get(root)
    ) {
      root = next;
    }
    this.parent.set(id, root);
    return root;
  }

  join(a: string, b: string): void {
    const x = this.find(a);
    const y = this.find(b);
    if (x !== y) {
      this.parent.set(y, x);
    }
  }
}

function mean(values: number[]): number {
  return values.reduce((sum, value) => sum + value, 0) / values.length;
}

function unitWidth(unit: Unit): number {
  return unit.members.length * NODE_W + (unit.members.length - 1) * SPOUSE_GAP;
}

function memberX(unit: Unit, position: number): number {
  return unit.x - unitWidth(unit) / 2 + NODE_W / 2 + position * (NODE_W + SPOUSE_GAP);
}

/** Gives each person a row. Partners share a row and children stand below their parents. */
function generations(people: string[], families: Family[]): Map<string, number> {
  const row = new Map(people.map((id) => [id, 0]));
  const at = (id: string): number => row.get(id) ?? 0;
  const rounds = people.length + 2;
  for (let round = 0; round < rounds; round++) {
    let changed = false;
    for (const family of families) {
      const top =
        family.parents.length > 0
          ? Math.max(...family.parents.map(at))
          : Math.max(...family.children.map(at)) - 1;
      for (const parent of family.parents) {
        if (at(parent) < top) {
          row.set(parent, top);
          changed = true;
        }
      }
      for (const child of family.children) {
        if (at(child) < top + 1) {
          row.set(child, top + 1);
          changed = true;
        }
      }
    }
    if (!changed) {
      break;
    }
  }
  // A marriage can push a child down. The parents then follow, so the line to the child stays short.
  const couples = new Sets();
  for (const family of families) {
    for (const parent of family.parents) {
      couples.join(family.parents[0] as string, parent);
    }
  }
  const groups = new Map<string, { members: string[]; children: string[] }>();
  for (const id of people) {
    const key = couples.find(id);
    const group = groups.get(key) ?? { members: [], children: [] };
    group.members.push(id);
    groups.set(key, group);
  }
  for (const family of families) {
    const first = family.parents[0];
    if (first) {
      groups.get(couples.find(first))?.children.push(...family.children);
    }
  }
  for (let round = 0; round < rounds; round++) {
    let changed = false;
    for (const group of groups.values()) {
      if (group.children.length === 0) {
        continue;
      }
      const target = Math.min(...group.children.map(at)) - 1;
      if (target > at(group.members[0] as string)) {
        for (const member of group.members) {
          row.set(member, target);
        }
        changed = true;
      }
    }
    if (!changed) {
      break;
    }
  }
  const lowest = Math.min(...people.map(at));
  for (const id of people) {
    row.set(id, at(id) - lowest);
  }
  return row;
}

/** Puts the partners of one unit in a row, so that most couples are neighbours. */
function orderMembers(index: GraphIndex, members: string[], pairs: [string, string][]): string[] {
  if (members.length < 2) {
    return members;
  }
  const next = new Map<string, string[]>(members.map((id) => [id, []]));
  for (const [a, b] of pairs) {
    next.get(a)?.push(b);
    next.get(b)?.push(a);
  }
  // A walk from a person with one partner gives a chain. A man starts, as in the other charts.
  const start = [...members].sort((a, b) => {
    const byLinks = (next.get(a)?.length ?? 0) - (next.get(b)?.length ?? 0);
    const man = (id: string): number => (index.byId.get(id)?.gender === "M" ? 0 : 1);
    return byLinks || man(a) - man(b);
  })[0] as string;
  const out: string[] = [];
  const seen = new Set<string>();
  const walk = (id: string): void => {
    seen.add(id);
    out.push(id);
    for (const partner of next.get(id) ?? []) {
      if (!seen.has(partner)) {
        walk(partner);
      }
    }
  };
  walk(start);
  for (const id of members) {
    if (!seen.has(id)) {
      walk(id);
    }
  }
  return out;
}

/** Moves the units of one row to the wanted places and keeps their order and their distance. */
function settle(row: Unit[], wanted: number[]): void {
  const count = row.length;
  if (count === 0) {
    return;
  }
  const space = (i: number): number =>
    (unitWidth(row[i - 1] as Unit) + unitWidth(row[i] as Unit)) / 2 + UNIT_GAP;
  const forward = new Array<number>(count);
  const backward = new Array<number>(count);
  for (let i = 0; i < count; i++) {
    const want = wanted[i] as number;
    forward[i] = i === 0 ? want : Math.max(want, (forward[i - 1] as number) + space(i));
  }
  for (let i = count - 1; i >= 0; i--) {
    const want = wanted[i] as number;
    backward[i] = i === count - 1 ? want : Math.min(want, (backward[i + 1] as number) - space(i + 1));
  }
  // Both rows of places keep the distances, so their middle keeps them too.
  for (let i = 0; i < count; i++) {
    (row[i] as Unit).x = ((forward[i] as number) + (backward[i] as number)) / 2;
  }
}

function layoutBlock(index: GraphIndex, people: string[], families: Family[]): Chart {
  const row = generations(people, families);
  const order = new Map(people.map((id, position) => [id, position]));

  // Units: a person with all partners, in one row.
  const couples = new Sets();
  const pairs = new Map<string, [string, string][]>();
  for (const family of families) {
    const [a, b] = family.parents;
    if (a && b) {
      couples.join(a, b);
    }
  }
  for (const family of families) {
    const [a, b] = family.parents;
    if (a && b) {
      const root = couples.find(a);
      pairs.set(root, [...(pairs.get(root) ?? []), [a, b]]);
    }
  }
  const membersOf = new Map<string, string[]>();
  for (const id of people) {
    const root = couples.find(id);
    membersOf.set(root, [...(membersOf.get(root) ?? []), id]);
  }
  const units: Unit[] = [];
  const unitOf = new Map<string, Unit>();
  for (const [root, members] of membersOf) {
    const unit: Unit = {
      members: orderMembers(index, members, pairs.get(root) ?? []),
      x: 0,
      layer: row.get(members[0] as string) ?? 0,
    };
    units.push(unit);
    for (const member of members) {
      unitOf.set(member, unit);
    }
  }

  const childFamilies = new Map<string, Family[]>();
  const ownFamilies = new Map<Unit, Family[]>();
  for (const family of families) {
    for (const child of family.children) {
      if (family.parents.length > 0) {
        childFamilies.set(child, [...(childFamilies.get(child) ?? []), family]);
      }
    }
    const first = family.parents[0];
    const unit = first ? unitOf.get(first) : undefined;
    if (unit) {
      ownFamilies.set(unit, [...(ownFamilies.get(unit) ?? []), family]);
    }
  }
  const parentUnits = (unit: Unit): Unit[] =>
    unit.members.flatMap((member) =>
      (childFamilies.get(member) ?? []).map((family) => unitOf.get(family.parents[0] as string) as Unit),
    );
  const childUnits = (unit: Unit): Unit[] =>
    (ownFamilies.get(unit) ?? []).flatMap((family) =>
      family.children.map((child) => unitOf.get(child) as Unit),
    );

  // The first order of each row: a walk from the people without parents down to their children.
  const layerCount = Math.max(...units.map((unit) => unit.layer)) + 1;
  const layers: Unit[][] = Array.from({ length: layerCount }, () => []);
  const visited = new Set<Unit>();
  const visit = (unit: Unit): void => {
    if (visited.has(unit)) {
      return;
    }
    visited.add(unit);
    (layers[unit.layer] as Unit[]).push(unit);
    for (const child of childUnits(unit)) {
      visit(child);
    }
  };
  const firstOf = (unit: Unit): number => Math.min(...unit.members.map((id) => order.get(id) ?? 0));
  const roots = units
    .filter((unit) => parentUnits(unit).length === 0)
    .sort((a, b) => a.layer - b.layer || firstOf(a) - firstOf(b));
  for (const unit of [...roots, ...units]) {
    visit(unit);
  }

  // The order of each row: a unit goes near its parents, then near its children, a few times.
  const slots = (): void => {
    for (const layer of layers) {
      let cursor = 0;
      for (const unit of layer) {
        unit.x = cursor + unit.members.length / 2;
        cursor += unit.members.length;
      }
    }
  };
  const sortBy = (layer: Unit[], near: (unit: Unit) => Unit[]): void => {
    const keys = new Map(
      layer.map((unit) => {
        const others = near(unit);
        return [unit, others.length > 0 ? mean(others.map((other) => other.x)) : unit.x];
      }),
    );
    const before = new Map(layer.map((unit, position) => [unit, position]));
    layer.sort(
      (a, b) => (keys.get(a) ?? 0) - (keys.get(b) ?? 0) || (before.get(a) ?? 0) - (before.get(b) ?? 0),
    );
  };
  slots();
  for (let sweep = 0; sweep < SWEEPS; sweep++) {
    for (let i = 1; i < layerCount; i++) {
      sortBy(layers[i] as Unit[], parentUnits);
      slots();
    }
    for (let i = layerCount - 2; i >= 0; i--) {
      sortBy(layers[i] as Unit[], childUnits);
      slots();
    }
  }
  for (let i = 1; i < layerCount; i++) {
    sortBy(layers[i] as Unit[], parentUnits);
    slots();
  }
  // In a couple, each partner stands on the side of the own parents.
  for (const unit of units) {
    const [left, right] = unit.members;
    if (unit.members.length !== 2 || !left || !right) {
      continue;
    }
    const side = (id: string): number | undefined => {
      const family = childFamilies.get(id)?.[0];
      return family ? unitOf.get(family.parents[0] as string)?.x : undefined;
    };
    const a = side(left);
    const b = side(right);
    if (a !== undefined && b !== undefined && a > b) {
      unit.members.reverse();
    }
  }

  // Places in pixels. First each row packs to the left, then the rows pull on each other.
  for (const layer of layers) {
    let cursor = 0;
    for (const unit of layer) {
      unit.x = cursor + unitWidth(unit) / 2;
      cursor += unitWidth(unit) + UNIT_GAP;
    }
  }
  const xOf = (id: string): number => {
    const unit = unitOf.get(id) as Unit;
    return memberX(unit, unit.members.indexOf(id));
  };
  const anchorX = (family: Family): number => mean(family.parents.map(xOf));
  for (let pass = 0; pass < PASSES; pass++) {
    // Children move under their parents.
    for (let i = 1; i < layerCount; i++) {
      const layer = layers[i] as Unit[];
      settle(
        layer,
        layer.map((unit) => {
          const shifts = unit.members.flatMap((member) =>
            (childFamilies.get(member) ?? []).map(
              (family) => anchorX(family) - mean(family.children.map(xOf)),
            ),
          );
          return unit.x + (shifts.length > 0 ? mean(shifts) : 0);
        }),
      );
    }
    // Parents move above their children.
    for (let i = layerCount - 2; i >= 0; i--) {
      const layer = layers[i] as Unit[];
      settle(
        layer,
        layer.map((unit) => {
          const shifts = (ownFamilies.get(unit) ?? [])
            .filter((family) => family.children.length > 0)
            .map((family) => mean(family.children.map(xOf)) - anchorX(family));
          return unit.x + (shifts.length > 0 ? mean(shifts) : 0);
        }),
      );
    }
  }

  const rowY = (layer: number): number => layer * (NODE_H + ROW_GAP);
  const nodes: ChartNode[] = [];
  for (const unit of units) {
    unit.members.forEach((id, position) => {
      nodes.push({
        id,
        x: memberX(unit, position),
        y: rowY(unit.layer),
        side: "descendant",
        generation: -unit.layer,
      });
    });
  }

  const links: ChartLink[] = [];
  // Families of one row take turns between three heights, so their lines to the children do not merge.
  const turn = new Map<number, number>();
  const sortedFamilies = [...families].sort((a, b) => {
    const ax = a.parents.length > 0 ? anchorX(a) : 0;
    const bx = b.parents.length > 0 ? anchorX(b) : 0;
    return ax - bx;
  });
  for (const family of sortedFamilies) {
    const [first, second] = family.parents;
    if (!first) {
      // Brothers and sisters without known parents share a line above them.
      const xs = family.children.map(xOf).sort((a, b) => a - b);
      const top = rowY(row.get(family.children[0] as string) ?? 0) - NODE_H / 2;
      // The line stays below the title of the tree.
      const bus = top - 18;
      family.children.forEach((child, position) => {
        const x = xOf(child);
        const childTop = rowY(row.get(child) ?? 0) - NODE_H / 2;
        links.push({
          key: `c:${family.id}:${position}`,
          kind: "parent",
          points: [
            [xs[0] as number, bus],
            [xs[0] as number, bus],
            [x, bus],
            [x, childTop],
          ],
        });
      });
      continue;
    }
    const unit = unitOf.get(first) as Unit;
    const y = rowY(unit.layer);
    let anchor: [number, number] = [xOf(first), y + NODE_H / 2];
    if (second) {
      const left = Math.min(xOf(first), xOf(second));
      const right = Math.max(xOf(first), xOf(second));
      const neighbours = Math.abs(unit.members.indexOf(first) - unit.members.indexOf(second)) === 1;
      if (neighbours) {
        links.push({
          key: `s:${family.id}`,
          kind: "spouse",
          points: [
            [left + NODE_W / 2, y],
            [right - NODE_W / 2, y],
          ],
        });
        anchor = [(left + right) / 2, y];
      } else {
        const low = y + NODE_H / 2 + BRACKET;
        links.push({
          key: `s:${family.id}`,
          kind: "spouse",
          points: [
            [left, y + NODE_H / 2],
            [left, low],
            [right, low],
            [right, y + NODE_H / 2],
          ],
        });
        anchor = [(left + right) / 2, low];
      }
    }
    if (family.children.length === 0) {
      continue;
    }
    const step = turn.get(unit.layer) ?? 0;
    turn.set(unit.layer, step + 1);
    const bus = y + NODE_H / 2 + ROW_GAP * (0.4 + 0.2 * (step % 3));
    family.children.forEach((child, position) => {
      links.push({
        key: `c:${family.id}:${position}`,
        kind: "parent",
        points: [
          anchor,
          [anchor[0], bus],
          [xOf(child), bus],
          [xOf(child), rowY(row.get(child) ?? 0) - NODE_H / 2],
        ],
      });
    });
  }

  const minX = Math.min(...nodes.map((node) => node.x)) - NODE_W / 2;
  const maxX = Math.max(...nodes.map((node) => node.x)) + NODE_W / 2;
  return {
    nodes,
    links,
    bounds: { minX, maxX, minY: -NODE_H / 2, maxY: rowY(layerCount - 1) + NODE_H / 2 },
  };
}

/**
 * Lays out the whole tree of one person: everybody who is linked to the
 * person through any chain of parents, children and partners.
 */
export function layoutWholeTree(index: GraphIndex, rootId: string): WholeTree {
  const known = new Set(index.graph.people.map((card) => card.id));
  const families: Family[] = [];
  const sets = new Sets();
  for (const family of index.graph.families) {
    const parents = [...new Set([family.father, family.mother])].filter(
      (id): id is string => id !== undefined && known.has(id),
    );
    const children = [...new Set(family.children ?? [])].filter(
      (id) => known.has(id) && !parents.includes(id),
    );
    if (parents.length + children.length < 2) {
      continue;
    }
    families.push({ id: family.id, parents, children });
    const members = [...parents, ...children];
    for (const member of members) {
      sets.join(members[0] as string, member);
    }
  }
  if (!known.has(rootId)) {
    return { nodes: [], links: [], bounds: { minX: 0, minY: 0, maxX: 1, maxY: 1 }, others: known.size };
  }
  const root = sets.find(rootId);
  const people = index.graph.people.map((card) => card.id).filter((id) => sets.find(id) === root);
  const own = families.filter(
    (family) => sets.find((family.parents[0] ?? family.children[0]) as string) === root,
  );
  const chart = layoutBlock(index, people, own);
  for (const node of chart.nodes) {
    if (node.id === rootId) {
      node.side = "root";
    }
  }
  return { ...chart, others: known.size - people.length };
}
