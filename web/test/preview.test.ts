import assert from "node:assert/strict";
import { test } from "node:test";
import { kindLabel, maxCanvasPixels, pageAt, pageBox, previewOf } from "../src/lib/preview";

const word = "application/vnd.openxmlformats-officedocument.wordprocessingml.document";

test("the viewer picks a view by the type of the file", () => {
  assert.equal(previewOf({ mime: "image/jpeg", image: true }), "image");
  assert.equal(previewOf({ mime: "application/pdf" }), "pdf");
  assert.equal(previewOf({ mime: " Application/PDF " }), "pdf");
  assert.equal(previewOf({ mime: word }), "document");
  assert.equal(previewOf({ mime: "text/plain" }), "document");
  assert.equal(previewOf({ mime: "application/msword" }), "none");
  assert.equal(previewOf({ mime: "image/svg+xml" }), "none");
  assert.equal(previewOf({}), "none");
});

test("a tile gets a short name of the file type", () => {
  assert.equal(kindLabel({ mime: word }), "DOCX");
  assert.equal(kindLabel({ mime: "application/pdf" }), "PDF");
  assert.equal(kindLabel({ mime: "text/plain" }), "TXT");
  assert.equal(kindLabel({ mime: "image/tiff" }), "TIFF");
  assert.equal(kindLabel({ mime: "application/octet-stream" }), "");
  assert.equal(kindLabel({ mime: "image/svg+xml" }), "");
  assert.equal(kindLabel({}), "");
});

test("a PDF page fills the width of the frame", () => {
  const a4 = { width: 595, height: 842 };
  const box = pageBox(a4, 400, 1, 2);
  assert.equal(box.width, 400);
  assert.equal(box.height, 566);
  // Two canvas pixels for each screen pixel.
  assert.ok(Math.abs(box.scale - (400 / 595) * 2) < 1e-9);
  const zoomed = pageBox(a4, 400, 2, 1);
  assert.equal(zoomed.width, 800);
  assert.ok(Math.abs(zoomed.scale - 800 / 595) < 1e-9);
});

test("a PDF page never asks for a canvas above the limit", () => {
  const a4 = { width: 595, height: 842 };
  for (const [frame, zoom, ratio] of [
    [1600, 6, 3],
    [400, 6, 3],
    [3000, 1, 2],
  ] as const) {
    const box = pageBox(a4, frame, zoom, ratio);
    const pixels = a4.width * box.scale * a4.height * box.scale;
    assert.ok(pixels <= maxCanvasPixels * 1.001, `${frame} ${zoom} ${ratio}: ${pixels}`);
  }
  // A broken page size gives a box and no error.
  const odd = pageBox({ width: 0, height: 0 }, 0, 1, 0);
  assert.ok(odd.width >= 1 && odd.height >= 1 && Number.isFinite(odd.scale));
});

test("the page counter follows the middle of the frame", () => {
  const tops = [12, 590, 1168];
  assert.equal(pageAt(tops, 0), 0);
  assert.equal(pageAt(tops, 300), 0);
  assert.equal(pageAt(tops, 590), 1);
  assert.equal(pageAt(tops, 5000), 2);
  assert.equal(pageAt([], 100), 0);
});
