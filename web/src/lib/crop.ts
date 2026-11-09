import type { Region } from "../api/types";

/**
 * A square part of a picture. x, y and w are fractions of the width and the
 * height of the picture: x and w of the width, y of the height.
 */
export interface Box {
  x: number;
  y: number;
  w: number;
}

export type Corner = "nw" | "ne" | "sw" | "se";

/** The smallest square, as a fraction of the largest square that fits. */
const MIN_PART = 0.1;

/** Returns the largest width of a square. aspect is the width of the picture divided by its height. */
export function maxWidth(aspect: number): number {
  return Math.min(1, 1 / aspect);
}

function minWidth(aspect: number): number {
  return maxWidth(aspect) * MIN_PART;
}

/** Returns the height of the square as a fraction of the height of the picture. */
export function boxHeight(box: Box, aspect: number): number {
  return box.w * aspect;
}

function limit(value: number, low: number, high: number): number {
  return Math.min(Math.max(value, low), Math.max(low, high));
}

/** Moves and shrinks the square until it lies inside the picture. */
export function clampBox(box: Box, aspect: number): Box {
  const w = limit(box.w, minWidth(aspect), maxWidth(aspect));
  return { w, x: limit(box.x, 0, 1 - w), y: limit(box.y, 0, 1 - w * aspect) };
}

/**
 * Returns the square that the dialog shows first: the square around the old
 * region, or the largest square. A tall picture gets the square near the top,
 * because a face is usually there.
 */
export function startBox(aspect: number, region?: Region | null): Box {
  if (region) {
    const width = (region.x2 - region.x1) / 100;
    const height = (region.y2 - region.y1) / 100;
    const w = Math.max(width, height / aspect);
    return clampBox(
      { w, x: region.x1 / 100 + (width - w) / 2, y: region.y1 / 100 + (height - w * aspect) / 2 },
      aspect,
    );
  }
  const w = maxWidth(aspect);
  const spare = 1 - w * aspect;
  return { w, x: (1 - w) / 2, y: aspect < 1 ? spare * 0.2 : spare / 2 };
}

/** Moves the square by a part of the width and of the height. */
export function moveBox(box: Box, dx: number, dy: number, aspect: number): Box {
  return clampBox({ ...box, x: box.x + dx, y: box.y + dy }, aspect);
}

/** Changes the size of the square around its centre. */
export function scaleBox(box: Box, w: number, aspect: number): Box {
  const next = limit(w, minWidth(aspect), maxWidth(aspect));
  return clampBox(
    { w: next, x: box.x + (box.w - next) / 2, y: box.y + ((box.w - next) * aspect) / 2 },
    aspect,
  );
}

/**
 * Drags one corner of the square to the point (px, py). The opposite corner
 * stays where it is.
 */
export function resizeBox(box: Box, corner: Corner, px: number, py: number, aspect: number): Box {
  const east = corner === "ne" || corner === "se";
  const south = corner === "sw" || corner === "se";
  const ax = east ? box.x : box.x + box.w;
  const ay = south ? box.y : box.y + box.w * aspect;
  const room = Math.min(east ? 1 - ax : ax, (south ? 1 - ay : ay) / aspect);
  const wanted = Math.max(east ? px - ax : ax - px, (south ? py - ay : ay - py) / aspect);
  const w = limit(wanted, Math.min(minWidth(aspect), room), room);
  return { w, x: east ? ax : ax - w, y: south ? ay : ay - w * aspect };
}

/** Returns the region of the square, in whole percent. */
export function boxRegion(box: Box, aspect: number): Region {
  const x1 = limit(Math.round(box.x * 100), 0, 99);
  const y1 = limit(Math.round(box.y * 100), 0, 99);
  return {
    x1,
    y1,
    x2: limit(Math.round((box.x + box.w) * 100), x1 + 1, 100),
    y2: limit(Math.round((box.y + box.w * aspect) * 100), y1 + 1, 100),
  };
}
