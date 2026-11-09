import assert from "node:assert/strict";
import { test } from "node:test";
import type { Card, Graph } from "../src/api/types";
import { indexGraph } from "../src/lib/graph";
import {
  FAN_SWEEP,
  fanArc,
  fanPath,
  fanRings,
  layoutFan,
  layoutTree,
  NODE_H,
  NODE_W,
} from "../src/lib/layout";

function person(id: string): Card {
  return { id, given: id };
}

const graph: Graph = {
  home: "me",
  people: ["me", "dad", "mom", "gf", "gm", "kid1", "kid2", "wife", "grandkid", "sis"].map(person),
  families: [
    { id: "F1", father: "dad", mother: "mom", children: ["me", "sis"] },
    { id: "F2", father: "gf", mother: "gm", children: ["dad"] },
    { id: "F3", father: "me", mother: "wife", children: ["kid1", "kid2"] },
    { id: "F4", father: "kid1", children: ["grandkid"] },
  ],
};

function overlaps(nodes: { x: number; y: number }[]): boolean {
  for (let i = 0; i < nodes.length; i++) {
    for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i];
      const b = nodes[j];
      if (a && b && Math.abs(a.x - b.x) < NODE_W && Math.abs(a.y - b.y) < NODE_H) {
        return true;
      }
    }
  }
  return false;
}

test("an hourglass holds the root, ancestors and descendants, with no overlap", () => {
  const chart = layoutTree(indexGraph(graph), "me", 2, 2);
  const ids = new Set(chart.nodes.map((node) => node.id));
  for (const id of ["me", "dad", "mom", "gf", "gm", "kid1", "kid2", "wife", "grandkid"]) {
    assert.ok(ids.has(id), `${id} is missing`);
  }
  assert.equal(chart.nodes.length, ids.size, "a person appears twice");
  assert.equal(overlaps(chart.nodes), false);
});

test("ancestors go up and descendants go down", () => {
  const chart = layoutTree(indexGraph(graph), "me", 2, 2);
  const y = (id: string): number => chart.nodes.find((node) => node.id === id)?.y ?? Number.NaN;
  assert.equal(y("me"), 0);
  assert.ok(y("dad") < 0);
  assert.ok(y("gf") < y("dad"));
  assert.ok(y("kid1") > 0);
  assert.ok(y("grandkid") > y("kid1"));
  assert.equal(y("wife"), 0);
});

test("the father's line is on the paternal side", () => {
  const chart = layoutTree(indexGraph(graph), "me", 2, 0);
  const side = (id: string): string | undefined => chart.nodes.find((node) => node.id === id)?.side;
  assert.equal(side("dad"), "paternal");
  assert.equal(side("gf"), "paternal");
  assert.equal(side("mom"), "maternal");
  assert.equal(side("me"), "root");
});

test("generation limits apply", () => {
  const chart = layoutTree(indexGraph(graph), "me", 1, 1);
  const ids = new Set(chart.nodes.map((node) => node.id));
  assert.ok(ids.has("dad"));
  assert.ok(!ids.has("gf"));
  assert.ok(ids.has("kid1"));
  assert.ok(!ids.has("grandkid"));
});

test("cycles in bad data end", () => {
  const bad: Graph = {
    people: ["a", "b"].map(person),
    families: [
      { id: "F1", father: "a", children: ["b"] },
      { id: "F2", father: "b", children: ["a"] },
    ],
  };
  const chart = layoutTree(indexGraph(bad), "a", 5, 5);
  assert.ok(chart.nodes.length <= 3);
  layoutFan(indexGraph(bad), "a", 5);
});

test("a person with no relatives gets a chart", () => {
  const chart = layoutTree(indexGraph({ people: [person("x")], families: [] }), "x", 3, 3);
  assert.equal(chart.nodes.length, 1);
  assert.deepEqual(chart.links, []);
});

test("fan segments split the sweep without gaps", () => {
  const fan = layoutFan(indexGraph(graph), "me", 2);
  const segment = (id: string) => fan.segments.find((item) => item.id === id);
  const dad = segment("dad");
  const mom = segment("mom");
  assert.ok(dad && mom);
  assert.equal(dad.start, -FAN_SWEEP / 2);
  assert.equal(dad.end, mom.start);
  assert.equal(mom.end, FAN_SWEEP / 2);
  const gf = segment("gf");
  assert.ok(gf);
  assert.equal(gf.start, dad.start);
  assert.equal(gf.end, (dad.start + dad.end) / 2);
  assert.equal(gf.side, "paternal");
});

test("a fan path is well formed", () => {
  const path = fanPath(-30, 30, 50, 100);
  assert.match(path, /^M [-\d.]+ [-\d.]+ L/);
  assert.match(path, /Z$/);
  assert.ok(!path.includes("NaN"));
});

test("fan rings touch, and the names bend while a segment is wide enough", () => {
  const rings = fanRings(6);
  assert.equal(rings.length, 6);
  for (let i = 1; i < rings.length; i++) {
    assert.equal(rings[i]?.inner, rings[i - 1]?.outer);
  }
  assert.deepEqual(
    rings.map((ring) => ring.curved),
    [true, true, true, true, false, false],
  );
});

test("a fan arc runs clockwise, and counterclockwise when reversed", () => {
  assert.equal(fanArc(0, 90, 100), "M 0.00 -100.00 A 100 100 0 0 1 100.00 0.00");
  assert.equal(fanArc(0, 90, 100, true), "M 100.00 0.00 A 100 100 0 0 0 0.00 -100.00");
});
