import assert from "node:assert/strict";
import { test } from "node:test";
import type { GalleryPhoto } from "../src/api/types";
import {
  filterGallery,
  formatPlace,
  kmBetween,
  mapLink,
  placeKey,
  placeName,
  placeOptions,
  takenDate,
  yearOptions,
} from "../src/lib/gallery";

function picture(id: string, ...people: string[]): GalleryPhoto {
  return { id, faces: people.map((person) => ({ person: { id: person } })) };
}

const photos = [
  picture("O1", "I1", "I2"),
  picture("O2", "I1"),
  picture("O3"),
  picture("O4", "I1", "I2", "I3"),
];
const ids = (list: GalleryPhoto[]): string[] => list.map((item) => item.id);
const show = (people: string[] = [], unmarked = false): string[] =>
  ids(filterGallery(photos, { unmarked, people }));

test("every chosen person must be on the photo", () => {
  assert.deepEqual(show(), ["O1", "O2", "O3", "O4"]);
  assert.deepEqual(show(["I1"]), ["O1", "O2", "O4"]);
  assert.deepEqual(show(["I1", "I2"]), ["O1", "O4"]);
  assert.deepEqual(show(["I2", "I1"]), ["O1", "O4"]);
  assert.deepEqual(show(["I1", "I2", "I3"]), ["O4"]);
  assert.deepEqual(show(["I9"]), []);
});

test("the photos without marks", () => {
  assert.deepEqual(show([], true), ["O3"]);
});

test("the filter by years", () => {
  const dated = [
    { ...picture("O1"), year: 2019 },
    { ...picture("O2"), year: 1975 },
    { ...picture("O3"), year: 2019 },
    picture("O4"),
  ];
  assert.deepEqual(yearOptions(dated), [
    { year: 2019, count: 2 },
    { year: 1975, count: 1 },
    { year: "none", count: 1 },
  ]);
  // Without any year there is nothing to choose.
  assert.deepEqual(yearOptions([picture("O4")]), []);
  const pick = (year: number | "none" | null): string[] =>
    ids(filterGallery(dated, { unmarked: false, people: [], year }));
  assert.deepEqual(pick(2019), ["O1", "O3"]);
  assert.deepEqual(pick(1975), ["O2"]);
  assert.deepEqual(pick("none"), ["O4"]);
  assert.deepEqual(pick(null), ["O1", "O2", "O3", "O4"]);
  assert.deepEqual(pick(1900), []);
});

test("the time from a file keeps the clock of the camera", () => {
  const date = takenDate("2019-07-14T15:42:10");
  assert.equal(date?.toISOString(), "2019-07-14T15:42:10.000Z");
  assert.equal(takenDate(""), null);
  assert.equal(takenDate(undefined), null);
  assert.equal(takenDate("2019-07-14"), null);
});

test("a place as text and as a link to a map", () => {
  const letters = { north: "N", south: "S", east: "E", west: "W" };
  assert.equal(formatPlace({ lat: -12.51, lon: -45.25 }, letters), "12.51000° S, 45.25000° W");
  assert.equal(formatPlace({ lat: 55.5, lon: 37.25 }, letters), "55.50000° N, 37.25000° E");
  assert.equal(
    mapLink({ lat: -12.51, lon: -45.25 }),
    "https://www.openstreetmap.org/?mlat=-12.51000&mlon=-45.25000#map=14/-12.51000/-45.25000",
  );
});

test("the places of the photos", () => {
  const paris = { lat: 48.8583, lon: 2.2944, name: "Paris", nameRu: "Париж", country: "FR" };
  const parisToo = { lat: 48.8683, lon: 2.3144, name: "Paris", nameRu: "Париж", country: "FR" };
  const sea = { lat: -12.5149, lon: -45.2551 };
  const placed = [
    { ...picture("O1"), place: paris },
    { ...picture("O2"), place: sea },
    { ...picture("O3"), place: parisToo },
    picture("O4"),
  ];
  assert.equal(placeKey(paris), "FR|Paris");
  assert.equal(placeKey(parisToo), placeKey(paris));
  assert.equal(placeKey(sea), "@-12.5,-45.3");
  assert.equal(placeName(paris, "ru"), "Париж, Франция");
  assert.equal(placeName(paris, "en"), "Paris, France");
  assert.equal(placeName(sea, "ru"), "");

  const options = placeOptions(placed);
  assert.deepEqual(
    options.map((option) => [option.key, option.count]),
    [
      ["FR|Paris", 2],
      ["@-12.5,-45.3", 1],
    ],
  );
  // The mark of a place stands in the middle of its photos.
  assert.ok(Math.abs((options[0]?.lat ?? 0) - 48.8633) < 1e-9);
  assert.ok(Math.abs((options[0]?.lon ?? 0) - 2.3044) < 1e-9);

  const pick = (place: string | null): string[] =>
    ids(filterGallery(placed, { unmarked: false, people: [], place }));
  assert.deepEqual(pick("FR|Paris"), ["O1", "O3"]);
  assert.deepEqual(pick("@-12.5,-45.3"), ["O2"]);
  assert.deepEqual(pick("none"), ["O4"]);
  assert.deepEqual(pick(null), ["O1", "O2", "O3", "O4"]);
});

test("the distance between two near points", () => {
  // One degree of latitude is about 111 km. One degree of longitude at 60 degrees north is half of that.
  assert.ok(Math.abs(kmBetween({ lat: 54, lon: 56 }, { lat: 55, lon: 56 }) - 111.2) < 0.1);
  assert.ok(Math.abs(kmBetween({ lat: 60, lon: 30 }, { lat: 60, lon: 31 }) - 55.6) < 0.1);
  assert.equal(kmBetween({ lat: 54, lon: 56 }, { lat: 54, lon: 56 }), 0);
});
