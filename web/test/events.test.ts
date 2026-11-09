import assert from "node:assert/strict";
import { test } from "node:test";
import { orderEvents } from "../src/lib/events";

interface Item {
  id: string;
  type: string;
  sort?: number;
}

const event = (id: string, type: string, sort?: number): Item => ({ id, type, sort });
const order = (items: Item[]): string =>
  orderEvents(items, (item) => item)
    .map((item) => item.id)
    .join(" ");

// The same cases as TestOrderEvents on the server, so the two rules stay equal.
test("dates decide the order, also for a wedding that comes from the family", () => {
  assert.equal(
    order([
      event("birth", "Birth", 19251108),
      event("service", "Military Service", 19430100),
      event("death", "Death", 19980312),
      event("wedding", "Marriage", 19520607),
      event("medal", "Award", 19450509),
    ]),
    "birth service medal wedding death",
  );
});

test("a year without a month stands at the start of the year, but a death at its end", () => {
  assert.equal(
    order([
      event("death", "Death", 19980000),
      event("letter", "Residence", 19980500),
      event("move", "Residence", 19980000),
      event("burial", "Burial", 19980000),
    ]),
    "move letter death burial",
  );
  assert.equal(
    order([
      event("death", "Death", 19980300),
      event("visit", "Residence", 19980320),
      event("burial", "Burial", 19980300),
      event("april", "Residence", 19980401),
    ]),
    "visit death burial april",
  );
});

test("an event without a date stays after the event that it followed", () => {
  assert.equal(
    order([
      event("birth", "Birth", 19251108),
      event("school", "Education"),
      event("death", "Death", 19980312),
      event("note", "Residence"),
      event("wedding", "Marriage", 19520607),
      event("child", "Residence", 0),
    ]),
    "birth school wedding child death note",
  );
});

test("without dates a birth goes first and a death last", () => {
  assert.equal(
    order([
      event("death", "Death"),
      event("job", "Occupation"),
      event("burial", "Burial"),
      event("birth", "Birth"),
      event("wedding", "Marriage", 19520607),
    ]),
    "birth job wedding death burial",
  );
  assert.equal(
    order([
      event("job", "Occupation"),
      event("home", "Residence"),
      event("wedding", "Marriage", 19520607),
      event("birth", "Birth"),
    ]),
    "birth job home wedding",
  );
});

test("on one day a birth is first and a death is last", () => {
  assert.equal(
    order([
      event("death", "Death", 19000101),
      event("baptism", "Baptism", 19000101),
      event("birth", "Birth", 19000101),
    ]),
    "birth baptism death",
  );
  assert.equal(order([]), "");
});
