import assert from "node:assert/strict";
import { test } from "node:test";
import type { Card, Graph } from "../src/api/types";
import { layoutWholeTree } from "../src/lib/forest";
import { indexGraph } from "../src/lib/graph";
import { NODE_H, NODE_W } from "../src/lib/layout";

function person(id: string, surname?: string, gender?: string): Card {
  return { id, given: id, surname, gender };
}

function overlap(nodes: { id: string; x: number; y: number }[]): string | null {
  for (let i = 0; i < nodes.length; i++) {
    for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i];
      const b = nodes[j];
      if (a && b && Math.abs(a.x - b.x) < NODE_W - 0.01 && Math.abs(a.y - b.y) < NODE_H - 0.01) {
        return `${a.id} and ${b.id}`;
      }
    }
  }
  return null;
}

const two: Graph = {
  people: [
    person("gf", "Orlov", "M"),
    person("gm", "Orlova", "F"),
    person("dad", "Orlov", "M"),
    person("aunt", "Orlova", "F"),
    person("mom", "Lebedeva", "F"),
    person("me", "Orlov", "M"),
    person("sis", "Orlova", "F"),
    person("mgf", "Lebedev", "M"),
    person("x1", "Belov", "M"),
    person("x2", "Belova", "F"),
    person("x3", "Belov", "M"),
    person("alone1", "Sokolov"),
    person("alone2", "Sokolov"),
  ],
  families: [
    { id: "F1", father: "gf", mother: "gm", children: ["dad", "aunt"] },
    { id: "F2", father: "dad", mother: "mom", children: ["me", "sis"] },
    { id: "F3", father: "mgf", children: ["mom"] },
    { id: "F4", father: "x1", mother: "x2", children: ["x3"] },
    { id: "F5", father: "ghost", children: ["alone1"] },
  ],
};

function place(graph: Graph, root: string) {
  const forest = layoutWholeTree(indexGraph(graph), root);
  const at = new Map(forest.nodes.map((node) => [node.id, node]));
  const get = (id: string) => {
    const node = at.get(id);
    assert.ok(node, `no node for ${id}`);
    return node;
  };
  return { forest, get };
}

test("the tree holds every linked person once, and no two cards overlap", () => {
  const { forest, get } = place(two, "me");
  const ids = forest.nodes.map((node) => node.id).sort();
  assert.deepEqual(ids, ["aunt", "dad", "gf", "gm", "me", "mgf", "mom", "sis"]);
  assert.equal(forest.others, 5);
  assert.equal(overlap(forest.nodes), null);
  assert.equal(get("me").side, "root");
  assert.equal(forest.nodes.filter((node) => node.side === "root").length, 1);
  for (const node of forest.nodes) {
    assert.ok(
      node.x - NODE_W / 2 >= forest.bounds.minX - 0.01 && node.x + NODE_W / 2 <= forest.bounds.maxX + 0.01,
    );
    assert.ok(
      node.y - NODE_H / 2 >= forest.bounds.minY - 0.01 && node.y + NODE_H / 2 <= forest.bounds.maxY + 0.01,
    );
  }
});

test("the tree is the same from every person of it, and other trees stay out", () => {
  const fromMe = place(two, "me").forest;
  const fromGrandmother = place(two, "gm").forest;
  const at = (nodes: typeof fromMe.nodes) => nodes.map((node) => [node.id, node.x, node.y]);
  assert.deepEqual(at(fromGrandmother.nodes), at(fromMe.nodes));
  assert.deepEqual(fromGrandmother.bounds, fromMe.bounds);
  const other = place(two, "x3").forest;
  assert.deepEqual(other.nodes.map((node) => node.id).sort(), ["x1", "x2", "x3"]);
  assert.equal(other.others, 10);
  // A person without any link is a tree of one.
  const alone = place(two, "alone2").forest;
  assert.deepEqual(
    alone.nodes.map((node) => node.id),
    ["alone2"],
  );
  assert.equal(alone.links.length, 0);
  assert.equal(alone.others, 12);
  // An unknown person gives an empty canvas.
  const none = place(two, "nobody").forest;
  assert.equal(none.nodes.length, 0);
  assert.ok(none.bounds.maxX > none.bounds.minX);
});

test("partners share a row and children stand below their parents", () => {
  const { get } = place(two, "me");
  assert.equal(get("gf").y, get("gm").y);
  assert.equal(get("dad").y, get("mom").y);
  assert.ok(get("dad").y > get("gf").y);
  assert.ok(get("me").y > get("dad").y);
  assert.equal(get("me").y, get("sis").y);
  // The father of the wife stands in the row of the other grandparents.
  assert.equal(get("mgf").y, get("gf").y);
  // A couple stands side by side.
  assert.ok(Math.abs(Math.abs(get("dad").x - get("mom").x) - NODE_W) < 20);
  // Each partner stands on the side of the own parents.
  assert.equal(get("dad").x < get("mom").x, get("gf").x < get("mgf").x);
});

test("lines join a couple and lead from the parents to each child", () => {
  const { forest, get } = place(two, "me");
  const spouse = forest.links.filter((item) => item.kind === "spouse");
  assert.equal(spouse.length, 2);
  const toMe = forest.links.filter((item) => {
    const last = item.points[item.points.length - 1];
    return item.kind === "parent" && last && Math.abs(last[0] - get("me").x) < 0.01;
  });
  assert.equal(toMe.length, 1);
  const points = toMe[0]?.points ?? [];
  // The line starts between the parents and ends at the top of the card.
  assert.ok(Math.abs((points[0]?.[0] ?? 0) - (get("dad").x + get("mom").x) / 2) < 0.01);
  assert.ok(Math.abs((points[3]?.[1] ?? 0) - (get("me").y - NODE_H / 2)) < 0.01);
  assert.equal(new Set(forest.links.map((item) => item.key)).size, forest.links.length);
});

test("a marriage across generations and a loop in the data do not break the layout", () => {
  const graph: Graph = {
    people: ["a", "b", "c", "d", "e", "f", "loop1", "loop2"].map((id) => person(id)),
    families: [
      { id: "F1", father: "a", mother: "b", children: ["c", "d"] },
      { id: "F2", father: "c", children: ["e"] },
      // An uncle marries the daughter of his brother.
      { id: "F3", father: "d", mother: "e", children: ["f"] },
      // Wrong data: two people are the parents of each other, and one of them is a child of the family.
      { id: "F4", father: "loop1", children: ["loop2"] },
      { id: "F5", father: "loop2", children: ["loop1"] },
      { id: "F6", father: "f", children: ["loop1"] },
    ],
  };
  const { forest, get } = place(graph, "a");
  assert.equal(forest.nodes.length, 8);
  assert.equal(overlap(forest.nodes), null);
  assert.equal(get("d").y, get("e").y);
  assert.ok(get("f").y > get("d").y);
  assert.ok(get("c").y < get("e").y);
  for (const node of forest.nodes) {
    assert.ok(Number.isFinite(node.x) && Number.isFinite(node.y));
  }
});

test("a person with several partners keeps them in one row", () => {
  const graph: Graph = {
    people: ["h", "w1", "w2", "w3", "k1", "k2", "k3"].map((id) => person(id)),
    families: [
      { id: "F1", father: "h", mother: "w1", children: ["k1"] },
      { id: "F2", father: "h", mother: "w2", children: ["k2"] },
      { id: "F3", father: "h", mother: "w3", children: ["k3"] },
    ],
  };
  const { forest, get } = place(graph, "k2");
  assert.equal(overlap(forest.nodes), null);
  for (const id of ["w1", "w2", "w3"]) {
    assert.equal(get(id).y, get("h").y);
  }
  assert.equal(forest.links.filter((item) => item.kind === "spouse").length, 3);
  assert.equal(forest.others, 0);
});

test("a large tree stays compact and the layout is the same each time", () => {
  // Five generations: each couple has three children and each child marries.
  const people: Card[] = [];
  const families: Graph["families"] = [];
  let next = 0;
  const add = (): string => {
    const id = `p${next++}`;
    people.push(person(id, "Orlov"));
    return id;
  };
  const grow = (father: string, mother: string, depth: number): void => {
    const children = depth > 0 ? [add(), add(), add()] : [];
    families.push({ id: `F${families.length}`, father, mother, children });
    for (const child of children) {
      grow(child, add(), depth - 1);
    }
  };
  grow(add(), add(), 4);
  const graph = { people, families };
  const first = layoutWholeTree(indexGraph(graph), "p0");
  const second = layoutWholeTree(indexGraph(graph), "p0");
  assert.deepEqual(first.nodes, second.nodes);
  assert.equal(first.nodes.length, people.length);
  assert.equal(overlap(first.nodes), null);
  // The widest row has 81 couples. The canvas is not much wider than that row.
  const width = first.bounds.maxX - first.bounds.minX;
  assert.ok(width < 81 * (2 * NODE_W + 60) * 1.15, `width ${width}`);
});

test("an empty tree gives an empty canvas", () => {
  const forest = layoutWholeTree(indexGraph({ people: [], families: [] }), "x");
  assert.equal(forest.nodes.length, 0);
  assert.ok(forest.bounds.maxX > forest.bounds.minX);
});
