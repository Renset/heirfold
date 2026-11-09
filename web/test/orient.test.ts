import assert from "node:assert/strict";
import { test } from "node:test";
import { toDisplay, toStored } from "../src/lib/orient";

const part = { x1: 10, y1: 20, x2: 30, y2: 60 };

test("an upright picture keeps the part", () => {
  assert.deepEqual(toDisplay(part, 1), part);
  assert.deepEqual(toStored(part, 1), part);
  // An unknown value counts as upright.
  assert.deepEqual(toDisplay(part, 0), part);
  assert.deepEqual(toStored(part, 9), part);
});

test("a picture that lies on its side moves the part", () => {
  // Orientation 6: the camera was turned, and the stored top is the right side of the upright picture.
  assert.deepEqual(toDisplay(part, 6), { x1: 40, y1: 10, x2: 80, y2: 30 });
  // Orientation 8 is the other side.
  assert.deepEqual(toDisplay(part, 8), { x1: 20, y1: 70, x2: 60, y2: 90 });
  // Orientation 3 is upside down.
  assert.deepEqual(toDisplay(part, 3), { x1: 70, y1: 40, x2: 90, y2: 80 });
});

test("the way to the upright picture and back gives the same part", () => {
  for (let orientation = 1; orientation <= 8; orientation++) {
    assert.deepEqual(toStored(toDisplay(part, orientation), orientation), part, `orientation ${orientation}`);
    assert.deepEqual(toDisplay(toStored(part, orientation), orientation), part, `orientation ${orientation}`);
    const shown = toDisplay(part, orientation);
    assert.ok(shown.x1 < shown.x2 && shown.y1 < shown.y2);
  }
});
