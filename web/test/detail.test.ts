import assert from "node:assert/strict";
import { test } from "node:test";
import { detailAt } from "../src/components/panzoom";

test("a chart shows less in a card when the reader zooms out", () => {
  assert.equal(detailAt(1.15), "");
  assert.equal(detailAt(0.7), "");
  assert.equal(detailAt(0.69), "mid");
  assert.equal(detailAt(0.25), "mid");
  assert.equal(detailAt(0.24), "low");
  assert.equal(detailAt(0.14), "low");
  assert.equal(detailAt(0.13), "far");
  assert.equal(detailAt(0.01), "far");
});
