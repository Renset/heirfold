// The map of the world, drawn from a file inside the app. No tile and no
// request leaves the server of the family, so a map shows nobody where the
// photos of the family were made.
//
// The outlines come from Natural Earth (public domain). The cities come from
// GeoNames (CC BY 4.0). scripts/geo/build.py makes the file.

/** A line of the map in the units of the map: x and y from 0 to 1, y down. */
interface Shape {
  points: Float32Array;
  minX: number;
  minY: number;
  maxX: number;
  maxY: number;
}

interface Country {
  code: string;
  name: string;
  labelX: number;
  labelY: number;
  /** The zoom from which the name shows. */
  zoom: number;
  people: number;
  rings: Shape[];
}

interface River {
  rank: number;
  line: Shape;
}

interface City {
  name: string;
  nameRu: string;
  x: number;
  y: number;
  people: number;
  capital: boolean;
}

export interface World {
  countries: Country[];
  lakes: Shape[];
  rivers: River[];
  cities: City[];
}

/** What the map shows now: the middle in the units of the map, and the pixels for the whole width of the world. */
export interface MapView {
  x: number;
  y: number;
  scale: number;
}

export interface MapColors {
  sea: string;
  /** The main roads of the close map. */
  road: string;
  /** Rivers and lakes. They are darker than the sea, so a thin river shows on the land. */
  water: string;
  land: string;
  border: string;
  text: string;
  faint: string;
  font: string;
}

const MAX_LATITUDE = 85;

/** Turns a longitude into the x of the map. */
export function mapX(lon: number): number {
  return (lon + 180) / 360;
}

/** Turns a latitude into the y of the map. The map is the usual one of the web, where north is up and the poles are cut. */
export function mapY(lat: number): number {
  const limited = Math.max(-MAX_LATITUDE, Math.min(MAX_LATITUDE, lat));
  const angle = (limited * Math.PI) / 180;
  return (1 - Math.log(Math.tan(Math.PI / 4 + angle / 2)) / Math.PI) / 2;
}

/** Turns the x of the map back into a longitude. */
export function mapLon(x: number): number {
  return x * 360 - 180;
}

/** Turns the y of the map back into a latitude. */
export function mapLat(y: number): number {
  return (Math.atan(Math.sinh(Math.PI * (1 - 2 * y))) * 180) / Math.PI;
}

/** The part of the globe that a view shows, in degrees. */
export function viewBox(
  view: MapView,
  width: number,
  height: number,
): { south: number; west: number; north: number; east: number } {
  return {
    west: mapLon(Math.max(view.x - width / view.scale / 2, 0)),
    east: mapLon(Math.min(view.x + width / view.scale / 2, 1)),
    north: mapLat(Math.max(view.y - height / view.scale / 2, 0)),
    south: mapLat(Math.min(view.y + height / view.scale / 2, 1)),
  };
}

/** The zoom of web maps: 0 shows the world in 256 pixels, and each step doubles it. */
export function zoomOf(scale: number): number {
  return Math.log2(scale / 256);
}

export function scaleOf(zoom: number): number {
  return 256 * 2 ** zoom;
}

export const MIN_ZOOM = 0.5;
/** The outlines are for a small scale. Closer than this they are only corners. */
export const MAX_ZOOM = 11;
/** From this zoom the map names the towns and the villages around, which the server lists. */
export const TOWN_ZOOM = 6.4;

/** From this zoom the map shows roads, the borders of regions and small rivers, which come in squares. */
export const DETAIL_ZOOM = 6.8;
/** The side of one square of the close map, in degrees. */
const SQUARE = 2;

/** One square of the close map. */
export interface Detail {
  rivers: River[];
  roads: River[];
  borders: Shape[];
  lakes: Shape[];
}

interface RawDetail {
  v?: number[][];
  d?: number[][];
  b?: number[][];
  l?: number[][];
}

/** Reads one square of the close map. Its numbers are 1/2000 of a degree; a river and a road start with a rank. */
export function decodeDetail(raw: RawDetail): Detail {
  const q = 2000;
  const ranked = (lines: number[][] | undefined): River[] =>
    (lines ?? []).map((line) => ({ rank: line[0] ?? 9, line: shape(line.slice(1), q) }));
  return {
    rivers: ranked(raw.v),
    roads: ranked(raw.d),
    borders: (raw.b ?? []).map((line) => shape(line, q)),
    lakes: (raw.l ?? []).map((line) => shape(line, q)),
  };
}

/** The squares of the close map that a part of the globe touches, with one degree more on each side. */
export function detailSquares(box: {
  south: number;
  west: number;
  north: number;
  east: number;
}): [number, number][] {
  const out: [number, number][] = [];
  const rows = 180 / SQUARE;
  const columns = 360 / SQUARE;
  const firstRow = Math.max(Math.floor((box.south - 1 + 90) / SQUARE), 0);
  const lastRow = Math.min(Math.floor((box.north + 1 + 90) / SQUARE), rows - 1);
  const firstColumn = Math.max(Math.floor((box.west - 1 + 180) / SQUARE), 0);
  const lastColumn = Math.min(Math.floor((box.east + 1 + 180) / SQUARE), columns - 1);
  for (let row = firstRow; row <= lastRow; row++) {
    for (let column = firstColumn; column <= lastColumn; column++) {
      out.push([row, column]);
    }
  }
  return out;
}

const squares = new Map<string, Detail | Promise<Detail>>();

/** Returns a square of the close map if it is in memory. */
export function knownDetail(row: number, column: number): Detail | null {
  const known = squares.get(`${row}_${column}`);
  return known && !(known instanceof Promise) ? known : null;
}

/** Loads a square of the close map once. The file is a part of the app, the same for everybody. */
export function loadDetail(row: number, column: number): Promise<Detail> {
  const key = `${row}_${column}`;
  const known = squares.get(key);
  if (known) {
    return Promise.resolve(known);
  }
  const loading = fetch(`geo/detail/${key}.json`)
    .then((response) => {
      if (!response.ok) {
        throw new Error(`map: ${response.status}`);
      }
      return response.json() as Promise<RawDetail>;
    })
    .then((raw) => {
      const detail = decodeDetail(raw);
      squares.set(key, detail);
      return detail;
    })
    .catch((error) => {
      squares.delete(key);
      throw error;
    });
  squares.set(key, loading);
  return loading;
}

/** A town or a village that the server names for the part of the world that the map shows. */
export interface Town {
  name: string;
  nameRu: string;
  x: number;
  y: number;
  people: number;
}

function shape(steps: number[], q: number): Shape {
  const points = new Float32Array(steps.length);
  let lon = 0;
  let lat = 0;
  let minX = 1;
  let minY = 1;
  let maxX = 0;
  let maxY = 0;
  for (let i = 0; i + 1 < steps.length; i += 2) {
    lon += steps[i] ?? 0;
    lat += steps[i + 1] ?? 0;
    const x = mapX(lon / q);
    const y = mapY(lat / q);
    points[i] = x;
    points[i + 1] = y;
    minX = Math.min(minX, x);
    minY = Math.min(minY, y);
    maxX = Math.max(maxX, x);
    maxY = Math.max(maxY, y);
  }
  return { points, minX, minY, maxX, maxY };
}

interface RawWorld {
  q: number;
  countries: { c: string; n: string; l: [number, number]; z: number; p: number; r: number[][] }[];
  lakes: number[][];
  rivers: { k: number; p: number[] }[];
  cities: { n: string; r?: string; p: [number, number]; s: number; c?: number }[];
}

/** Reads the file of the map into shapes that are ready to draw. */
export function decodeWorld(raw: RawWorld): World {
  const q = raw.q;
  return {
    countries: raw.countries.map((country) => ({
      code: country.c,
      name: country.n,
      labelX: mapX(country.l[0] / q),
      labelY: mapY(country.l[1] / q),
      zoom: country.z,
      people: country.p,
      rings: country.r.map((ring) => shape(ring, q)),
    })),
    lakes: raw.lakes.map((ring) => shape(ring, q)),
    rivers: raw.rivers.map((river) => ({ rank: river.k, line: shape(river.p, q) })),
    cities: raw.cities.map((city) => ({
      name: city.n,
      nameRu: city.r ?? "",
      x: mapX(city.p[0] / q),
      y: mapY(city.p[1] / q),
      people: city.s,
      capital: city.c === 1,
    })),
  };
}

let loading: Promise<World> | null = null;

/** Loads the map once for the whole app. */
export function loadWorld(): Promise<World> {
  if (!loading) {
    loading = fetch("geo/world.json")
      .then((response) => {
        if (!response.ok) {
          throw new Error(`map: ${response.status}`);
        }
        return response.json() as Promise<RawWorld>;
      })
      .then(decodeWorld)
      .catch((error) => {
        loading = null;
        throw error;
      });
  }
  return loading;
}

/** The view that shows all the points with some room around them. One point gets the view of a region. */
export function fitView(
  points: { x: number; y: number }[],
  width: number,
  height: number,
  maxZoom = 6,
): MapView {
  if (points.length === 0 || width <= 0 || height <= 0) {
    return { x: 0.5, y: 0.42, scale: Math.max(width, scaleOf(MIN_ZOOM)) };
  }
  let minX = 1;
  let minY = 1;
  let maxX = 0;
  let maxY = 0;
  for (const point of points) {
    minX = Math.min(minX, point.x);
    minY = Math.min(minY, point.y);
    maxX = Math.max(maxX, point.x);
    maxY = Math.max(maxY, point.y);
  }
  // The marks and their names need room at the edges.
  const room = 0.6;
  const scale = Math.min(
    (width * room) / Math.max(maxX - minX, 1e-9),
    (height * room) / Math.max(maxY - minY, 1e-9),
    scaleOf(maxZoom),
  );
  return clampView({ x: (minX + maxX) / 2, y: (minY + maxY) / 2, scale }, width, height);
}

/** The closest zoom of a map with streets from a tile service. */
export const STREET_ZOOM = 18;

// The closest zoom that the map allows now. The map of the program stops
// where its outlines are only corners; a map with streets goes on.
let deepest = MAX_ZOOM;

/** Says whether the map has streets from a tile service, so the zoom can go closer. */
export function setStreets(on: boolean): void {
  deepest = on ? STREET_ZOOM : MAX_ZOOM;
}

/** Keeps the view on the map and inside the limits of the zoom. */
export function clampView(view: MapView, width: number, height: number): MapView {
  const scale = Math.min(Math.max(view.scale, scaleOf(MIN_ZOOM), Math.min(width, height)), scaleOf(deepest));
  const halfX = Math.min(width / scale / 2, 0.5);
  const halfY = Math.min(height / scale / 2, 0.5);
  return {
    scale,
    x: Math.min(Math.max(view.x, halfX), 1 - halfX),
    y: Math.min(Math.max(view.y, halfY), 1 - halfY),
  };
}

/** Changes the zoom so that the point under the pointer stays where it is. */
export function zoomAt(
  view: MapView,
  factor: number,
  px: number,
  py: number,
  width: number,
  height: number,
): MapView {
  const scale = Math.min(Math.max(view.scale * factor, scaleOf(MIN_ZOOM)), scaleOf(deepest));
  const worldX = view.x + (px - width / 2) / view.scale;
  const worldY = view.y + (py - height / 2) / view.scale;
  return clampView(
    { scale, x: worldX - (px - width / 2) / scale, y: worldY - (py - height / 2) / scale },
    width,
    height,
  );
}

/** The place of a point of the map on the screen. */
export function toScreen(
  view: MapView,
  x: number,
  y: number,
  width: number,
  height: number,
): [number, number] {
  return [(x - view.x) * view.scale + width / 2, (y - view.y) * view.scale + height / 2];
}

/** The smallest city that gets a name at a zoom. */
function cityLimit(zoom: number): number {
  if (zoom < 2.6) {
    return Number.POSITIVE_INFINITY;
  }
  if (zoom < 3.6) {
    return 4_000_000;
  }
  if (zoom < 4.6) {
    return 1_000_000;
  }
  if (zoom < 5.6) {
    return 400_000;
  }
  return 100_000;
}

/** The smallest town that gets a name close to the ground. A smaller place is a dot until the map comes closer. */
function townLimit(zoom: number): number {
  if (zoom < 7.4) {
    return 50_000;
  }
  if (zoom < 8.4) {
    return 10_000;
  }
  if (zoom < 9.4) {
    return 2_000;
  }
  return 0;
}

interface Box {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

function overlaps(a: Box, b: Box): boolean {
  return a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
}

/**
 * Draws the map into a canvas. `taken` holds the places of the screen that
 * the marks of the photos use, so no name of the map goes under a mark.
 */
export function drawWorld(
  ctx: CanvasRenderingContext2D,
  world: World,
  view: MapView,
  width: number,
  height: number,
  colors: MapColors,
  lang: string,
  taken: Box[] = [],
  towns: Town[] | null = null,
  details: Detail[] = [],
): void {
  const left = view.x - width / view.scale / 2;
  const top = view.y - height / view.scale / 2;
  const right = view.x + width / view.scale / 2;
  const bottom = view.y + height / view.scale / 2;
  const visible = (s: Shape): boolean =>
    s.maxX >= left && s.minX <= right && s.maxY >= top && s.minY <= bottom;
  // A shape smaller than one pixel is not worth a path.
  const tiny = 0.75 / view.scale;
  const large = (s: Shape): boolean => s.maxX - s.minX > tiny || s.maxY - s.minY > tiny;
  const trace = (s: Shape, close: boolean): void => {
    const p = s.points;
    ctx.moveTo(((p[0] ?? 0) - left) * view.scale, ((p[1] ?? 0) - top) * view.scale);
    for (let i = 2; i + 1 < p.length; i += 2) {
      ctx.lineTo(((p[i] ?? 0) - left) * view.scale, ((p[i + 1] ?? 0) - top) * view.scale);
    }
    if (close) {
      ctx.closePath();
    }
  };
  const zoom = zoomOf(view.scale);

  ctx.fillStyle = colors.sea;
  ctx.fillRect(0, 0, width, height);

  ctx.beginPath();
  for (const country of world.countries) {
    for (const ring of country.rings) {
      if (visible(ring) && large(ring)) {
        trace(ring, true);
      }
    }
  }
  ctx.fillStyle = colors.land;
  ctx.fill();
  ctx.lineJoin = "round";
  ctx.lineWidth = 1;
  ctx.strokeStyle = colors.border;
  ctx.stroke();

  // The close map: the borders of regions, the main roads, and rivers and
  // lakes at a larger scale. They come in squares, as far as they are loaded.
  const close = details.length > 0 && zoom >= DETAIL_ZOOM;
  if (close) {
    ctx.beginPath();
    for (const detail of details) {
      for (const border of detail.borders) {
        if (visible(border)) {
          trace(border, false);
        }
      }
    }
    ctx.setLineDash([5, 4]);
    ctx.lineWidth = 1;
    ctx.strokeStyle = colors.border;
    ctx.stroke();
    ctx.setLineDash([]);

    // Thin roads first, so a main road lies over a side road.
    for (const [from, to, thick] of [
      [8, 99, 1],
      [5, 7, 1.4],
      [0, 4, 2],
    ] as const) {
      if (from > zoom + 1.5) {
        continue;
      }
      ctx.beginPath();
      for (const detail of details) {
        for (const road of detail.roads) {
          if (road.rank >= from && road.rank <= to && road.rank <= zoom + 1.5 && visible(road.line)) {
            trace(road.line, false);
          }
        }
      }
      ctx.lineWidth = thick;
      ctx.strokeStyle = colors.road;
      ctx.stroke();
    }

    for (const [from, to, thick] of [
      [7, 99, 1],
      [4, 6, 1.6],
      [0, 3, 2.4],
    ] as const) {
      ctx.beginPath();
      for (const detail of details) {
        for (const river of detail.rivers) {
          if (river.rank >= from && river.rank <= to && visible(river.line)) {
            trace(river.line, false);
          }
        }
      }
      ctx.lineWidth = thick;
      ctx.strokeStyle = colors.water;
      ctx.stroke();
    }
    ctx.beginPath();
    for (const detail of details) {
      for (const lake of detail.lakes) {
        if (visible(lake) && large(lake)) {
          trace(lake, true);
        }
      }
    }
    ctx.fillStyle = colors.water;
    ctx.fill();
  }

  if (zoom >= 3 && !close) {
    ctx.beginPath();
    for (const river of world.rivers) {
      if (river.rank <= zoom + 2 && visible(river.line)) {
        trace(river.line, false);
      }
    }
    ctx.lineWidth = zoom >= 8 ? 2 : zoom >= 5 ? 1.5 : 1;
    ctx.strokeStyle = colors.water;
    ctx.stroke();
  }
  ctx.beginPath();
  for (const lake of world.lakes) {
    if (visible(lake) && large(lake)) {
      trace(lake, true);
    }
  }
  ctx.fillStyle = colors.water;
  ctx.fill();

  // The names. A name that would lie on another name, or on a mark, stays out.
  const used: Box[] = [...taken];
  const place = (text: string, x: number, y: number, align: "center" | "left"): boolean => {
    const w = ctx.measureText(text).width;
    const box: Box =
      align === "center"
        ? { left: x - w / 2 - 4, right: x + w / 2 + 4, top: y - 9, bottom: y + 9 }
        : { left: x - 6, right: x + w + 12, top: y - 9, bottom: y + 9 };
    if (
      box.right < 0 ||
      box.left > width ||
      box.bottom < 0 ||
      box.top > height ||
      used.some((other) => overlaps(box, other))
    ) {
      return false;
    }
    used.push(box);
    return true;
  };
  ctx.textBaseline = "middle";
  ctx.lineJoin = "round";
  ctx.lineWidth = 3;
  ctx.strokeStyle = colors.land;

  let regions: Intl.DisplayNames | null = null;
  try {
    regions = new Intl.DisplayNames([lang], { type: "region" });
  } catch {
    regions = null;
  }
  // Close to the ground the server names the towns and the villages. Far
  // from it the file of the map has the large cities.
  const near = towns !== null && zoom >= TOWN_ZOOM;
  const smallest = near ? townLimit(zoom) : cityLimit(zoom);
  const cities: Town[] = (near ? towns : world.cities).filter(
    (city) => city.x >= left && city.x <= right && city.y >= top && city.y <= bottom,
  );
  ctx.font = `500 12px ${colors.font}`;
  ctx.textAlign = "left";
  for (const city of cities) {
    const [x, y] = toScreen(view, city.x, city.y, width, height);
    const named =
      city.people >= smallest || (!near && zoom >= 4.2 && (city as { capital?: boolean }).capital === true);
    if (!named && !near) {
      continue;
    }
    const name = lang === "ru" && city.nameRu ? city.nameRu : city.name;
    const shown = named && place(name, x, y, "left");
    // A village without room for its name is still a dot, so the map shows where people live.
    ctx.fillStyle = shown ? colors.text : colors.faint;
    ctx.beginPath();
    ctx.arc(x, y, shown ? 2.2 : 1.4, 0, Math.PI * 2);
    ctx.fill();
    if (shown) {
      ctx.strokeText(name, x + 6, y);
      ctx.fillText(name, x + 6, y);
    }
  }

  ctx.font = `600 11px ${colors.font}`;
  ctx.textAlign = "center";
  const countries = world.countries
    .filter((country) => zoom + 1.2 >= country.zoom && zoom <= country.zoom + 4.5)
    .sort((a, b) => b.people - a.people);
  for (const country of countries) {
    const [x, y] = toScreen(view, country.labelX, country.labelY, width, height);
    let name = country.name;
    if (country.code && regions) {
      try {
        name = regions.of(country.code) ?? name;
      } catch {
        // The browser does not know the code. The name of the file stays.
      }
    }
    const text = name.toLocaleUpperCase(lang);
    if (!place(text, x, y, "center")) {
      continue;
    }
    ctx.fillStyle = colors.faint;
    ctx.strokeText(text, x, y);
    ctx.fillText(text, x, y);
  }
}

// The pictures of a map with streets. The server of the family gives them:
// it asks the tile service and keeps the answers, so the service never sees
// the browser of a relative.
type Tile = { image: HTMLImageElement; ready: boolean; failed: boolean };
const tiles = new Map<string, Tile>();
/** The most pictures in memory. One screen takes some dozens. */
const MAX_TILES = 320;

function tile(z: number, x: number, y: number, loaded: () => void): Tile {
  const key = `${z}/${x}/${y}`;
  const known = tiles.get(key);
  if (known) {
    // The picture is in use, so it goes to the end of the line of the old ones.
    tiles.delete(key);
    tiles.set(key, known);
    return known;
  }
  if (tiles.size >= MAX_TILES) {
    const oldest = tiles.keys().next().value;
    if (oldest !== undefined) {
      tiles.delete(oldest);
    }
  }
  const made: Tile = { image: new Image(), ready: false, failed: false };
  made.image.decoding = "async";
  made.image.onload = () => {
    made.ready = true;
    loaded();
  };
  made.image.onerror = () => {
    made.failed = true;
  };
  made.image.src = `/api/v1/map/tiles/${key}`;
  tiles.set(key, made);
  return made;
}

/** The whole zoom of the pictures for a view: the nearest step that is not blurred much. */
export function tileZoom(scale: number): number {
  return Math.min(Math.max(Math.round(zoomOf(scale)), 0), STREET_ZOOM + 1);
}

/**
 * Draws the map with streets into a canvas. A picture that is still on its
 * way leaves the ground color, or a part of a picture from a farther zoom
 * that is in memory. `loaded` runs when a new picture is ready to draw.
 */
export function drawStreets(
  ctx: CanvasRenderingContext2D,
  view: MapView,
  width: number,
  height: number,
  colors: MapColors,
  loaded: () => void,
): void {
  ctx.fillStyle = colors.land;
  ctx.fillRect(0, 0, width, height);
  const z = tileZoom(view.scale);
  const count = 2 ** z;
  const size = view.scale / count;
  const left = view.x - width / view.scale / 2;
  const top = view.y - height / view.scale / 2;
  const firstX = Math.max(Math.floor(left * count), 0);
  const firstY = Math.max(Math.floor(top * count), 0);
  const lastX = Math.min(Math.floor((left + width / view.scale) * count), count - 1);
  const lastY = Math.min(Math.floor((top + height / view.scale) * count), count - 1);
  for (let y = firstY; y <= lastY; y++) {
    for (let x = firstX; x <= lastX; x++) {
      const sx = (x / count - left) * view.scale;
      const sy = (y / count - top) * view.scale;
      const wanted = tile(z, x, y, loaded);
      if (wanted.ready) {
        // Half a pixel more hides the seams between the pictures.
        ctx.drawImage(wanted.image, sx, sy, size + 0.5, size + 0.5);
        continue;
      }
      // A part of a picture from one to three steps farther, if it is in memory.
      for (let up = 1; up <= 3 && z - up >= 0; up++) {
        const parent = tiles.get(`${z - up}/${x >> up}/${y >> up}`);
        if (parent?.ready) {
          const part = 256 / 2 ** up;
          ctx.drawImage(
            parent.image,
            (x % 2 ** up) * part,
            (y % 2 ** up) * part,
            part,
            part,
            sx,
            sy,
            size + 0.5,
            size + 0.5,
          );
          break;
        }
      }
    }
  }
}
