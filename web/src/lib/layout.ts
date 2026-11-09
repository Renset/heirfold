import type { GraphIndex } from "./graph";
import { parentsOf, unitsOf } from "./graph";

// Pure layout functions for the tree charts. They return positions only, so
// tests can check them without a browser.

export const NODE_W = 204;
export const NODE_H = 76;
const H_GAP = 20;
const V_GAP = 72;
const SPOUSE_GAP = 12;

export type Side = "root" | "paternal" | "maternal" | "descendant" | "spouse";

export interface ChartNode {
  id: string;
  x: number;
  y: number;
  side: Side;
  generation: number;
}

export interface ChartLink {
  key: string;
  points: [number, number][];
  kind: "parent" | "spouse";
}

export interface Chart {
  nodes: ChartNode[];
  links: ChartLink[];
  bounds: { minX: number; minY: number; maxX: number; maxY: number };
}

interface AncestorNode {
  id: string;
  x: number;
  depth: number;
  side: Side;
  father?: AncestorNode;
  mother?: AncestorNode;
}

function buildAncestors(
  index: GraphIndex,
  id: string,
  depth: number,
  limit: number,
  side: Side,
  seen: Set<string>,
): AncestorNode {
  const node: AncestorNode = { id, x: 0, depth, side };
  if (depth >= limit) {
    return node;
  }
  const { father, mother } = parentsOf(index, id);
  if (father && !seen.has(father.id)) {
    seen.add(father.id);
    node.father = buildAncestors(index, father.id, depth + 1, limit, depth === 0 ? "paternal" : side, seen);
  }
  if (mother && !seen.has(mother.id)) {
    seen.add(mother.id);
    node.mother = buildAncestors(index, mother.id, depth + 1, limit, depth === 0 ? "maternal" : side, seen);
  }
  return node;
}

/** Places leaves from left to right and centres each child under its parents. */
function placeAncestors(node: AncestorNode, cursor: { x: number }): void {
  if (node.father) {
    placeAncestors(node.father, cursor);
  }
  if (node.mother) {
    placeAncestors(node.mother, cursor);
  }
  if (node.father && node.mother) {
    node.x = (node.father.x + node.mother.x) / 2;
  } else if (node.father ?? node.mother) {
    node.x = (node.father ?? node.mother)?.x ?? 0;
  } else {
    node.x = cursor.x;
    cursor.x += NODE_W + H_GAP;
  }
}

function shiftAncestors(node: AncestorNode, delta: number): void {
  node.x += delta;
  if (node.father) {
    shiftAncestors(node.father, delta);
  }
  if (node.mother) {
    shiftAncestors(node.mother, delta);
  }
}

function rowY(generation: number): number {
  return -generation * (NODE_H + V_GAP);
}

function elbow(fromX: number, fromY: number, toX: number, toY: number): [number, number][] {
  const midY = (fromY + toY) / 2;
  return [
    [fromX, fromY],
    [fromX, midY],
    [toX, midY],
    [toX, toY],
  ];
}

function collectAncestors(node: AncestorNode, nodes: ChartNode[], links: ChartLink[]): void {
  const y = rowY(node.depth);
  if (node.depth > 0) {
    nodes.push({ id: node.id, x: node.x, y, side: node.side, generation: node.depth });
  }
  for (const parent of [node.father, node.mother]) {
    if (!parent) {
      continue;
    }
    links.push({
      key: `a:${node.id}:${parent.id}`,
      kind: "parent",
      points: elbow(node.x, y - NODE_H / 2, parent.x, rowY(parent.depth) + NODE_H / 2),
    });
    collectAncestors(parent, nodes, links);
  }
}

interface DescendantBlock {
  id: string;
  spouses: (string | undefined)[];
  families: { spouse?: string; children: DescendantBlock[] }[];
  width: number;
  anchor: number;
}

function buildDescendants(
  index: GraphIndex,
  id: string,
  depth: number,
  limit: number,
  seen: Set<string>,
): DescendantBlock {
  const units = unitsOf(index, id);
  const block: DescendantBlock = { id, spouses: [], families: [], width: NODE_W, anchor: 0 };
  for (const unit of units) {
    block.spouses.push(unit.spouse?.id);
    const children: DescendantBlock[] = [];
    if (depth < limit) {
      for (const child of unit.children) {
        if (!seen.has(child.id)) {
          seen.add(child.id);
          children.push(buildDescendants(index, child.id, depth + 1, limit, seen));
        }
      }
    }
    block.families.push({ spouse: unit.spouse?.id, children });
  }
  const spouseCount = block.spouses.filter(Boolean).length;
  const coupleWidth = NODE_W + spouseCount * (NODE_W + SPOUSE_GAP);
  const childrenWidth = block.families.reduce((sum, family) => {
    const width = family.children.reduce((total, child) => total + child.width, 0);
    return sum + width + Math.max(0, family.children.length - 1) * H_GAP;
  }, 0);
  const gaps = Math.max(0, block.families.filter((f) => f.children.length > 0).length - 1) * H_GAP;
  block.width = Math.max(coupleWidth, childrenWidth + gaps);
  return block;
}

function placeDescendants(
  block: DescendantBlock,
  left: number,
  generation: number,
  nodes: ChartNode[],
  links: ChartLink[],
  isRoot: boolean,
): number {
  const y = -rowY(generation);
  const spouseCount = block.spouses.filter(Boolean).length;
  const coupleWidth = NODE_W + spouseCount * (NODE_W + SPOUSE_GAP);
  const allChildren = block.families.flatMap((family) => family.children);
  const childSpan =
    allChildren.reduce((sum, child) => sum + child.width, 0) + Math.max(0, allChildren.length - 1) * H_GAP;
  const childLeft = left + (block.width - childSpan) / 2;
  const coupleLeft = left + (block.width - coupleWidth) / 2;
  const x = coupleLeft + NODE_W / 2;
  block.anchor = x;
  if (!isRoot) {
    nodes.push({ id: block.id, x, y, side: "descendant", generation: -generation });
  }

  let spouseX = x;
  let cursor = childLeft;
  for (const family of block.families) {
    let familyX = x;
    if (family.spouse) {
      spouseX += NODE_W + SPOUSE_GAP;
      nodes.push({ id: family.spouse, x: spouseX, y, side: "spouse", generation: -generation });
      links.push({
        key: `s:${block.id}:${family.spouse}`,
        kind: "spouse",
        points: [
          [spouseX - NODE_W - SPOUSE_GAP + NODE_W / 2, y],
          [spouseX - NODE_W / 2, y],
        ],
      });
      familyX = spouseX - (NODE_W + SPOUSE_GAP) / 2;
    }
    for (const child of family.children) {
      const childX = placeDescendants(child, cursor, generation + 1, nodes, links, false);
      links.push({
        key: `d:${block.id}:${family.spouse ?? ""}:${child.id}`,
        kind: "parent",
        points: elbow(familyX, y + NODE_H / 2, childX, -rowY(generation + 1) - NODE_H / 2),
      });
      cursor += child.width + H_GAP;
    }
  }
  return x;
}

function measure(nodes: ChartNode[]): Chart["bounds"] {
  const xs = nodes.map((node) => node.x);
  const ys = nodes.map((node) => node.y);
  return {
    minX: Math.min(...xs) - NODE_W / 2,
    maxX: Math.max(...xs) + NODE_W / 2,
    minY: Math.min(...ys) - NODE_H / 2,
    maxY: Math.max(...ys) + NODE_H / 2,
  };
}

/**
 * Builds a chart around one person. Ancestors go up and descendants go down.
 * With `down` at zero it is a pedigree. With `up` at zero it is a descendant
 * tree. With both it is an hourglass.
 */
export function layoutTree(index: GraphIndex, rootId: string, up: number, down: number): Chart {
  const seen = new Set([rootId]);
  const ancestors = buildAncestors(index, rootId, 0, up, "root", seen);
  placeAncestors(ancestors, { x: 0 });

  const nodes: ChartNode[] = [];
  const links: ChartLink[] = [];

  const descendantSeen = new Set([rootId]);
  const descendants = buildDescendants(index, rootId, 0, down, descendantSeen);
  placeDescendants(descendants, 0, 0, nodes, links, true);
  shiftAncestors(ancestors, descendants.anchor - ancestors.x);
  collectAncestors(ancestors, nodes, links);

  nodes.push({ id: rootId, x: descendants.anchor, y: 0, side: "root", generation: 0 });
  return { nodes, links, bounds: measure(nodes) };
}

export interface FanSegment {
  id: string;
  generation: number;
  start: number;
  end: number;
  side: Side;
}

export interface FanChart {
  rootId: string;
  segments: FanSegment[];
  generations: number;
}

/** Sweep of the fan chart, in degrees. */
export const FAN_SWEEP = 240;

/** Places ancestors on arcs. Each generation splits the arc of the previous one in two. */
export function layoutFan(index: GraphIndex, rootId: string, generations: number): FanChart {
  const segments: FanSegment[] = [];
  const seen = new Set([rootId]);
  const walk = (id: string, generation: number, start: number, end: number, side: Side): void => {
    if (generation > generations) {
      return;
    }
    const { father, mother } = parentsOf(index, id);
    const middle = (start + end) / 2;
    const parents: [typeof father, number, number, Side][] = [
      [father, start, middle, generation === 1 ? "paternal" : side],
      [mother, middle, end, generation === 1 ? "maternal" : side],
    ];
    for (const [parent, from, to, parentSide] of parents) {
      if (!parent || seen.has(parent.id)) {
        continue;
      }
      seen.add(parent.id);
      segments.push({ id: parent.id, generation, start: from, end: to, side: parentSide });
      walk(parent.id, generation + 1, from, to, parentSide);
    }
  };
  const half = FAN_SWEEP / 2;
  walk(rootId, 1, -half, half, "root");
  return { rootId, segments, generations };
}

/** Returns the SVG path of one fan segment. Angle 0 points up. */
export function fanPath(start: number, end: number, inner: number, outer: number, gapDegrees = 0.5): string {
  const a0 = ((start + gapDegrees - 90) * Math.PI) / 180;
  const a1 = ((end - gapDegrees - 90) * Math.PI) / 180;
  const large = end - start - 2 * gapDegrees > 180 ? 1 : 0;
  const point = (radius: number, angle: number): string =>
    `${(radius * Math.cos(angle)).toFixed(2)} ${(radius * Math.sin(angle)).toFixed(2)}`;
  return [
    `M ${point(inner, a0)}`,
    `L ${point(outer, a0)}`,
    `A ${outer} ${outer} 0 ${large} 1 ${point(outer, a1)}`,
    `L ${point(inner, a1)}`,
    `A ${inner} ${inner} 0 ${large} 0 ${point(inner, a0)}`,
    "Z",
  ].join(" ");
}

/** One ring of the fan chart. Ring 0 holds the parents. */
export interface FanRing {
  inner: number;
  outer: number;
  /** The names follow the arc. Otherwise they run along the radius. */
  curved: boolean;
}

/** Radius of the circle of the main person. */
export const FAN_CENTER = 72;

const FAN_WIDE_RING = 86;
const FAN_ARC_RING = 70;
const FAN_RADIAL_RING = 118;
const FAN_MIN_ARC = 80;

/**
 * Returns the rings of a fan chart. A ring bends its names along the arc while
 * one segment is long enough for a name. Narrow outer rings write along the radius.
 */
export function fanRings(generations: number): FanRing[] {
  const rings: FanRing[] = [];
  let inner = FAN_CENTER;
  for (let generation = 1; generation <= generations; generation++) {
    const span = ((FAN_SWEEP / 2 ** generation) * Math.PI) / 180;
    const depth = generation <= 2 ? FAN_WIDE_RING : FAN_ARC_RING;
    const curved = span * (inner + depth / 2) >= FAN_MIN_ARC;
    const outer = inner + (curved ? depth : FAN_RADIAL_RING);
    rings.push({ inner, outer, curved });
    inner = outer;
  }
  return rings;
}

/**
 * Returns an arc at one radius, as a path for text. Angle 0 points up. A
 * reversed arc runs counterclockwise, so text in the lower half reads upright.
 */
export function fanArc(start: number, end: number, radius: number, reverse = false): string {
  const point = (degrees: number): string => {
    const angle = ((degrees - 90) * Math.PI) / 180;
    return `${(radius * Math.cos(angle)).toFixed(2)} ${(radius * Math.sin(angle)).toFixed(2)}`;
  };
  const large = end - start > 180 ? 1 : 0;
  return reverse
    ? `M ${point(end)} A ${radius} ${radius} 0 ${large} 0 ${point(start)}`
    : `M ${point(start)} A ${radius} ${radius} 0 ${large} 1 ${point(end)}`;
}
