import assert from "node:assert/strict";
import { test } from "node:test";
import type { Card, DateView, Graph } from "../src/api/types";
import { formatDate, fullName, initials, lifeYears } from "../src/lib/format";
import {
  branchOf,
  indexGraph,
  normalize,
  searchPeople,
  siblingsOf,
  startPerson,
  unitsOf,
} from "../src/lib/graph";

test("dates read naturally in both languages", () => {
  const exact: DateView = { form: "exact", start: { y: 1901, m: 5, d: 14 } };
  assert.equal(formatDate(exact, "en"), "May 14, 1901");
  assert.match(formatDate(exact, "ru"), /14 мая 1901/);
  assert.equal(formatDate({ form: "about", start: { y: 1901 } }, "ru"), "около 1901");
  assert.equal(formatDate({ form: "before", start: { y: 1901 } }, "en"), "before 1901");
  assert.equal(
    formatDate({ form: "range", start: { y: 1901 }, stop: { y: 1903 } }, "ru"),
    "между 1901 и 1903",
  );
  assert.equal(formatDate({ form: "text", text: "весной" }, "ru"), "весной");
  assert.equal(formatDate(undefined, "ru"), "");
});

test("old style dates are marked", () => {
  const view: DateView = { form: "exact", start: { y: 1880 }, calendar: "Julian" };
  assert.equal(formatDate(view, "ru"), "1880 (ст. ст.)");
  assert.equal(formatDate(view, "en"), "1880 (O.S.)");
});

test("years of life", () => {
  const birth: DateView = { form: "exact", start: { y: 1901 } };
  const death: DateView = { form: "exact", start: { y: 1975 } };
  assert.equal(lifeYears({ id: "1", birth, death }, "en"), "1901-1975");
  assert.equal(lifeYears({ id: "1", birth, living: true }, "ru"), "род. 1901");
  assert.equal(lifeYears({ id: "1", birth }, "en"), "1901-?");
  assert.equal(lifeYears({ id: "1", death }, "ru"), "ум. 1975");
  assert.equal(lifeYears({ id: "1" }, "en"), "");
  assert.equal(
    lifeYears({ id: "1", birth: { form: "about", start: { y: 1901 } }, living: true }, "ru"),
    "род. ок. 1901",
  );
});

test("names follow the order of the language", () => {
  const card: Card = { id: "1", given: "Глафира", patronymic: "Ефимовна", surname: "Вересова" };
  assert.equal(fullName(card, "ru"), "Вересова Глафира Ефимовна");
  assert.equal(fullName(card, "en"), "Глафира Ефимовна Вересова");
  assert.equal(initials(card), "ГВ");
  assert.equal(initials({ id: "2" }), "?");
});

const graph: Graph = {
  home: "hidden",
  people: [
    { id: "a", given: "Ада", surname: "Ольхина" },
    { id: "b", given: "Ёлка", surname: "Вересова" },
    { id: "c", given: "Глеб", surname: "Вересов" },
    { id: "d", given: "Анна", surname: "Ольхина", patronymic: "Глебовна" },
  ],
  families: [
    { id: "F1", father: "c", mother: "a", children: ["d", "b"] },
    { id: "F2", father: "c", children: ["b"] },
  ],
};

test("search folds case and the letter yo", () => {
  const index = indexGraph(graph);
  assert.equal(normalize("Ёлка"), "елка");
  assert.deepEqual(
    searchPeople(index, "елка").map((card) => card.id),
    ["b"],
  );
  assert.deepEqual(
    searchPeople(index, "ольх глеб").map((card) => card.id),
    ["d"],
  );
  assert.deepEqual(searchPeople(index, "   "), []);
  assert.deepEqual(searchPeople(index, "ъъъ"), []);
});

test("relatives", () => {
  const index = indexGraph(graph);
  assert.deepEqual(
    siblingsOf(index, "d").map((card) => card.id),
    ["b"],
  );
  const units = unitsOf(index, "c");
  assert.equal(units.length, 2);
  assert.equal(units[0]?.spouse?.id, "a");
  assert.equal(units[0]?.children.length, 2);
});

test("a hidden home person falls back to the best connected person", () => {
  assert.equal(startPerson(indexGraph(graph)), "c");
  assert.equal(startPerson(indexGraph({ home: "a", people: graph.people, families: [] })), "a");
  assert.equal(startPerson(indexGraph({ people: [], families: [] })), undefined);
});

test("the own branch is the person, the descendants and the partners", () => {
  const person = (id: string): Card => ({ id });
  const tree: Graph = {
    people: ["root", "wife", "child", "inlaw", "grandchild", "father", "brother", "ex", "step", "far"].map(
      person,
    ),
    families: [
      { id: "f1", father: "root", mother: "wife", children: ["child"] },
      { id: "f2", father: "inlaw", mother: "child", children: ["grandchild"] },
      { id: "f3", father: "father", children: ["root", "brother"] },
      { id: "f4", father: "ex", mother: "wife", children: ["step"] },
    ],
  };
  const branch = branchOf(indexGraph(tree), "root");
  assert.deepEqual([...branch.line].sort(), ["child", "grandchild", "root"]);
  assert.deepEqual([...branch.people].sort(), ["child", "grandchild", "inlaw", "root", "wife"]);
  assert.equal(branchOf(indexGraph(tree), "nobody").people.size, 0);
});
