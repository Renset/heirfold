import assert from "node:assert/strict";
import { test } from "node:test";
import { boxHeight, boxRegion, clampBox, moveBox, resizeBox, scaleBox, startBox } from "../src/lib/crop";

const near = (a: number, b: number): boolean => Math.abs(a - b) < 1e-9;

test("the first square is the largest one, near the top of a tall picture", () => {
  assert.deepEqual(startBox(2), { w: 0.5, x: 0.25, y: 0 });
  const tall = startBox(0.5);
  assert.equal(tall.w, 1);
  assert.equal(tall.x, 0);
  assert.ok(near(tall.y, 0.1));
  assert.ok(near(boxHeight(tall, 0.5), 0.5));
  assert.deepEqual(boxRegion(startBox(1), 1), { x1: 0, y1: 0, x2: 100, y2: 100 });
});

test("an old region gives a square around the same centre", () => {
  // 40% by 20% of a square picture: the square has the larger side.
  const box = startBox(1, { x1: 10, y1: 30, x2: 50, y2: 50 });
  assert.ok(near(box.w, 0.4) && near(box.x, 0.1) && near(box.y, 0.2));
  assert.deepEqual(boxRegion(box, 1), { x1: 10, y1: 20, x2: 50, y2: 60 });
  // A region at the edge stays inside the picture.
  const edge = startBox(1, { x1: 80, y1: 0, x2: 100, y2: 60 });
  assert.ok(near(edge.w, 0.6) && near(edge.x, 0.4) && near(edge.y, 0));
});

test("the square stays inside the picture", () => {
  const moved = moveBox({ w: 0.5, x: 0.25, y: 0 }, 1, 1, 2);
  assert.deepEqual(moved, { w: 0.5, x: 0.5, y: 0 });
  assert.deepEqual(moveBox({ w: 0.2, x: 0.1, y: 0.1 }, -1, -1, 1), { w: 0.2, x: 0, y: 0 });
  const clamped = clampBox({ w: 5, x: -1, y: 3 }, 1);
  assert.deepEqual(clamped, { w: 1, x: 0, y: 0 });
  assert.equal(clampBox({ w: 0, x: 0.5, y: 0.5 }, 1).w, 0.1);
});

test("a new size keeps the centre", () => {
  const box = scaleBox({ w: 0.2, x: 0.4, y: 0.4 }, 0.4, 1);
  assert.ok(near(box.w, 0.4) && near(box.x, 0.3) && near(box.y, 0.3));
  assert.equal(scaleBox(box, 9, 1).w, 1);
});

test("a drag of a corner keeps the opposite corner and the square form", () => {
  const start = { w: 0.2, x: 0.4, y: 0.4 };
  const se = resizeBox(start, "se", 0.9, 0.7, 1);
  assert.ok(near(se.x, 0.4) && near(se.y, 0.4) && near(se.w, 0.5));
  const nw = resizeBox(start, "nw", 0.3, 0.35, 1);
  assert.ok(near(nw.w, 0.3) && near(nw.x + nw.w, 0.6) && near(nw.y + nw.w, 0.6));
  // The picture ends before the pointer does.
  const far = resizeBox(start, "se", 5, 5, 1);
  assert.ok(near(far.w, 0.6) && near(far.x, 0.4));
  // A pointer on the wrong side gives the smallest square.
  assert.ok(near(resizeBox(start, "se", 0, 0, 1).w, 0.1));
  // In a wide picture the square is narrower in parts of the width.
  const wide = resizeBox({ w: 0.1, x: 0, y: 0 }, "se", 1, 1, 2);
  assert.ok(near(wide.w, 0.5) && near(boxHeight(wide, 2), 1));
});

test("the region is whole percent with a real area", () => {
  assert.deepEqual(boxRegion({ w: 0.333, x: 0.105, y: 0.5 }, 1), { x1: 11, y1: 50, x2: 44, y2: 83 });
  const tiny = boxRegion({ w: 0.001, x: 0.999, y: 0.999 }, 1);
  assert.ok(tiny.x2 > tiny.x1 && tiny.y2 > tiny.y1 && tiny.x2 <= 100 && tiny.y2 <= 100);
});
