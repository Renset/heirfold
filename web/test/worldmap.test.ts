import assert from "node:assert/strict";
import { test } from "node:test";
import {
  clampView,
  decodeDetail,
  decodeWorld,
  detailSquares,
  fitView,
  MAX_ZOOM,
  mapLat,
  mapLon,
  mapX,
  mapY,
  scaleOf,
  toScreen,
  viewBox,
  zoomAt,
  zoomOf,
} from "../src/lib/worldmap";

const near = (a: number, b: number, epsilon = 1e-9): boolean => Math.abs(a - b) < epsilon;

test("a point of the globe on the map", () => {
  assert.ok(near(mapX(-180), 0));
  assert.ok(near(mapX(0), 0.5));
  assert.ok(near(mapX(180), 1));
  assert.ok(near(mapY(0), 0.5));
  // North is up, and the same distance north and south of the equator is the same distance on the map.
  assert.ok(mapY(60) < mapY(0));
  assert.ok(near(mapY(60) + mapY(-60), 1));
  // The poles are cut, so every point has a place.
  assert.ok(mapY(90) >= 0 && mapY(-90) <= 1);
  assert.equal(mapY(90), mapY(85));
});

test("the zoom and the scale", () => {
  assert.equal(scaleOf(0), 256);
  assert.equal(scaleOf(3), 2048);
  assert.ok(near(zoomOf(scaleOf(4.5)), 4.5));
});

test("the view that shows all the places", () => {
  const paris = { x: mapX(2.35), y: mapY(48.85) };
  const ufa = { x: mapX(55.97), y: mapY(54.74) };
  const view = fitView([paris, ufa], 800, 400);
  for (const point of [paris, ufa]) {
    const [x, y] = toScreen(view, point.x, point.y, 800, 400);
    assert.ok(x > 60 && x < 740 && y > 40 && y < 360, `the point is at ${x}, ${y}`);
  }
  // One place gets the view of a region, not the closest view.
  const one = fitView([paris], 320, 200, 5.4);
  assert.ok(near(zoomOf(one.scale), 5.4));
  assert.ok(near(one.x, paris.x) && near(one.y, paris.y));
  // No place shows the world.
  const world = fitView([], 800, 400);
  assert.ok(world.scale >= 800 && world.scale <= 1024);
});

test("the view stays on the map", () => {
  const moved = clampView({ x: -3, y: 9, scale: scaleOf(4) }, 400, 300);
  const half = 400 / scaleOf(4) / 2;
  assert.ok(near(moved.x, half));
  assert.ok(near(moved.y, 1 - 300 / scaleOf(4) / 2));
  assert.equal(clampView({ x: 0.5, y: 0.5, scale: 1e12 }, 400, 300).scale, scaleOf(MAX_ZOOM));
  // The map is never smaller than the short side of its frame.
  assert.equal(clampView({ x: 0.5, y: 0.5, scale: 1 }, 800, 600).scale, 600);
});

test("the zoom keeps the point under the pointer", () => {
  const view = { x: 0.52, y: 0.31, scale: scaleOf(4) };
  const before = [view.x + (300 - 400) / view.scale, view.y + (120 - 200) / view.scale];
  const zoomed = zoomAt(view, 2, 300, 120, 800, 400);
  assert.ok(near(zoomed.scale, view.scale * 2));
  const [x, y] = toScreen(zoomed, before[0] ?? 0, before[1] ?? 0, 800, 400);
  assert.ok(near(x, 300, 1e-6) && near(y, 120, 1e-6));
});

test("the file of the map becomes shapes", () => {
  const world = decodeWorld({
    q: 100,
    countries: [
      {
        c: "XX",
        n: "Example",
        l: [1000, 2000],
        z: 3,
        p: 5,
        r: [[0, 0, 1000, 0, 0, 1000, -1000, 0, 0, -1000]],
      },
    ],
    lakes: [],
    rivers: [{ k: 4, p: [0, 0, 500, 500] }],
    cities: [
      { n: "Town", r: "Город", p: [500, 500], s: 123456, c: 1 },
      { n: "Other", p: [100, 100], s: 200000 },
    ],
  });
  const ring = world.countries[0]?.rings[0];
  assert.ok(ring);
  // The steps add up: the corners are 0, 10 and 10 degrees east, then back.
  assert.ok(near(ring.points[0] ?? -1, mapX(0), 1e-6));
  assert.ok(near(ring.points[2] ?? -1, mapX(10), 1e-6));
  assert.ok(near(ring.points[5] ?? -1, mapY(10), 1e-6));
  assert.ok(near(ring.points[8] ?? -1, mapX(0), 1e-6));
  assert.ok(near(ring.minX, mapX(0), 1e-6) && near(ring.maxX, mapX(10), 1e-6));
  assert.equal(world.cities[0]?.nameRu, "Город");
  assert.equal(world.cities[0]?.capital, true);
  assert.equal(world.cities[1]?.nameRu, "");
  assert.equal(world.rivers[0]?.rank, 4);
});

test("a point of the map back on the globe", () => {
  for (const [lat, lon] of [
    [54.735, 55.958],
    [-33.86, 151.21],
    [0, 0],
    [70.5, -150.25],
  ] as const) {
    assert.ok(near(mapLat(mapY(lat)), lat, 1e-9), `latitude ${lat}`);
    assert.ok(near(mapLon(mapX(lon)), lon, 1e-9), `longitude ${lon}`);
  }
});

test("the part of the globe that a view shows", () => {
  const view = { x: mapX(56), y: mapY(54.7), scale: scaleOf(9) };
  const box = viewBox(view, 400, 300);
  assert.ok(box.west < 56 && box.east > 56 && box.south < 54.7 && box.north > 54.7);
  // 400 pixels at this zoom are about a degree of longitude.
  assert.ok(near(box.east - box.west, (400 / scaleOf(9)) * 360, 1e-9));
  assert.ok(box.north - box.south < box.east - box.west);
});

test("the squares of the close map", () => {
  // A view around 55 degrees north, 56 degrees east, one degree wide. With a
  // degree more on each side it reaches from 53.6 to 56.4 degrees north:
  // three rows of squares, and two columns.
  assert.deepEqual(detailSquares({ south: 54.6, west: 55.5, north: 55.4, east: 56.5 }), [
    [71, 117],
    [71, 118],
    [72, 117],
    [72, 118],
    [73, 117],
    [73, 118],
  ]);
  // The squares stay on the globe.
  const corner = detailSquares({ south: -90, west: -180, north: -89.5, east: -179.5 });
  assert.deepEqual(corner[0], [0, 0]);
  assert.ok(corner.every(([row, column]) => row >= 0 && column >= 0));
  const top = detailSquares({ south: 89, west: 179, north: 90, east: 180 });
  assert.ok(top.every(([row, column]) => row <= 89 && column <= 179));
});

test("a square of the close map becomes shapes", () => {
  const detail = decodeDetail({
    v: [[3, 112000, 110000, 2000, 0]],
    d: [[7, 112000, 110000, 0, 2000]],
    b: [[112000, 110000, 1000, 1000]],
  });
  assert.equal(detail.rivers[0]?.rank, 3);
  assert.equal(detail.roads[0]?.rank, 7);
  assert.equal(detail.lakes.length, 0);
  const river = detail.rivers[0]?.line;
  assert.ok(river);
  // 112000 / 2000 is 56 degrees east; the step goes one degree to the east.
  assert.ok(near(river.points[0] ?? -1, mapX(56), 1e-6));
  assert.ok(near(river.points[2] ?? -1, mapX(57), 1e-6));
  assert.ok(near(river.points[1] ?? -1, mapY(55), 1e-6));
  assert.ok(near(detail.borders[0]?.points[3] ?? -1, mapY(55.5), 1e-6));
});
