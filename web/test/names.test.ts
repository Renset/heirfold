import assert from "node:assert/strict";
import { test } from "node:test";
import type { Card } from "../src/api/types";
import { birthNote, fullName, surnameLine } from "../src/lib/format";
import { feminine, looksMarried, sameSurname } from "../src/lib/names";

test("the form of a surname for a woman", () => {
  assert.equal(feminine("Иванов"), "Иванова");
  assert.equal(feminine("Королёв"), "Королёва");
  assert.equal(feminine("Галкин"), "Галкина");
  assert.equal(feminine("Вишневский"), "Вишневская");
  assert.equal(feminine("Толстой"), "Толстая");
  assert.equal(feminine("Шевченко"), "Шевченко");
  assert.ok(sameSurname("Иванов", "Иванова"));
  assert.ok(sameSurname("Шевченко", "шевченко"));
  assert.ok(!sameSurname("Иванов", "Петрова"));
  assert.ok(!sameSurname("", "Петрова"));
});

test("a surname that looks like a surname from a marriage", () => {
  assert.ok(looksMarried("Иванова", { father: "Сидоров", husbands: ["Иванов"] }));
  assert.ok(looksMarried("Иванова", { husbands: ["Петров", "Иванов"] }));
  // The same surname as the father: the surname at birth, whatever the husband.
  assert.ok(!looksMarried("Иванова", { father: "Иванов", husbands: ["Иванов"] }));
  assert.ok(!looksMarried("Сидорова", { father: "Сидоров", husbands: ["Иванов"] }));
  assert.ok(!looksMarried("Иванова", { husbands: [] }));
  assert.ok(!looksMarried("", { husbands: ["Иванов"] }));
});

test("the surname at birth next to the name", () => {
  const anna: Card = {
    id: "I1",
    given: "Анна",
    patronymic: "Петровна",
    surname: "Иванова",
    birthSurname: "Сидорова",
    gender: "F",
  };
  assert.equal(birthNote(anna, "ru"), "урожд. Сидорова");
  assert.equal(birthNote(anna, "en"), "née Sidorova".replace("Sidorova", "Сидорова"));
  assert.equal(birthNote({ ...anna, gender: "M" }, "en"), "né Сидорова");
  assert.equal(birthNote({ ...anna, gender: "U" }, "en"), "born Сидорова");
  assert.equal(birthNote({ ...anna, birthSurname: undefined }, "ru"), "");
  assert.equal(fullName(anna, "ru"), "Иванова Анна Петровна (урожд. Сидорова)");
  assert.equal(surnameLine(anna), "Иванова (Сидорова)");
  assert.equal(surnameLine({ id: "I2", surname: "Иванова" }), "Иванова");
  assert.equal(fullName({ id: "I3", birthSurname: "Сидорова" }, "ru"), "Сидорова");
});
