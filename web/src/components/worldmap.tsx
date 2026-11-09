import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { server } from "../api/client";
import { locale, t } from "../i18n";
import {
  clampView,
  DETAIL_ZOOM,
  detailSquares,
  drawStreets,
  drawWorld,
  fitView,
  knownDetail,
  loadDetail,
  loadWorld,
  type MapColors,
  type MapView,
  mapLat,
  mapLon,
  mapX,
  mapY,
  setStreets,
  TOWN_ZOOM,
  type Town,
  toScreen,
  viewBox,
  type World,
  zoomAt,
  zoomOf,
} from "../lib/worldmap";
import { me } from "../state";
import { Icon } from "./icons";
import { Spinner } from "./ui";

/** A place with photos on the map. */
export interface MapMark {
  id: string;
  lat: number;
  lon: number;
  /** The name of the place. */
  label: string;
  /** The number of photos. Without it the mark is a plain dot. */
  count?: number;
}

interface Group {
  /** The marks that stand too near to each other at this zoom to show apart. */
  marks: MapMark[];
  x: number;
  y: number;
  count: number;
}

/** The towns that the server named, by the box of the question. Every map of the page shares them. */
const townCache = new Map<string, Town[]>();

/** Marks nearer than this to each other, in pixels, show as one. */
const NEAR = 34;

function readColors(element: Element): MapColors {
  const style = getComputedStyle(element);
  const value = (name: string, fallback: string): string => style.getPropertyValue(name).trim() || fallback;
  return {
    sea: value("--map-sea", "#e8eaed"),
    water: value("--map-water", "#a9bfdc"),
    road: value("--map-road", "#d9d0bd"),
    land: value("--map-land", "#ffffff"),
    border: value("--map-border", "#b9bdc4"),
    text: value("--map-text", "#444444"),
    faint: value("--map-faint", "#888888"),
    font: style.fontFamily || "sans-serif",
  };
}

/**
 * A map of the world with the places of photos. The app draws it from its own
 * file: no map service learns what a person looks at. With `onPick`, a press
 * on a place chooses it. Without `interactive` the map is a still picture.
 */
export function WorldMap({
  marks,
  interactive = false,
  size = "large",
  label,
  onPick,
  closest,
  onPoint,
}: {
  marks: MapMark[];
  interactive?: boolean;
  size?: "small" | "medium" | "large";
  /** The closest zoom at which the map opens. Without it the size of the map decides. */
  closest?: number;
  /** With it, a press on the map says the point of the press. */
  onPoint?: (lat: number, lon: number) => void;
  /** What the map shows, for a person who cannot see it. */
  label: string;
  onPick?: (id: string) => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const [world, setWorld] = useState<World | null>(null);
  const [failed, setFailed] = useState(false);
  const [area, setArea] = useState({ width: 0, height: 0 });
  const [view, setView] = useState<MapView | null>(null);
  // A change of the theme changes the colors, so the map is drawn again.
  const [theme, setTheme] = useState(0);
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  // Where the press started and how far the pointer went. A press without a move is a choice of a point.
  const press = useRef<{ x: number; y: number; moved: number } | null>(null);
  const [towns, setTowns] = useState<{ key: string; list: Town[] } | null>(null);
  const moves = interactive || onPoint !== undefined;
  // With a tile service the server gives a map with streets. Else the app draws its own map.
  const streets = Boolean(me.value?.mapTiles);
  setStreets(streets);

  useEffect(() => {
    let active = true;
    loadWorld()
      .then((loaded) => {
        if (active) {
          setWorld(loaded);
        }
      })
      .catch(() => {
        if (active) {
          setFailed(true);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    const element = box.current;
    if (!element) {
      return;
    }
    const measure = (): void => setArea({ width: element.clientWidth, height: element.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    const repaint = (): void => setTheme((value) => value + 1);
    const attributes = new MutationObserver(repaint);
    attributes.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    const scheme = window.matchMedia("(prefers-color-scheme: dark)");
    scheme.addEventListener("change", repaint);
    return () => {
      observer.disconnect();
      attributes.disconnect();
      scheme.removeEventListener("change", repaint);
    };
  }, []);

  const points = useMemo(
    () => marks.map((mark) => ({ mark, x: mapX(mark.lon), y: mapY(mark.lat) })),
    [marks],
  );
  // New places, or a new size of the map, show all the places again.
  const key = `${marks.map((mark) => mark.id).join("|")}/${area.width}x${area.height}`;
  useEffect(() => {
    if (area.width > 0 && area.height > 0) {
      // A map with streets opens nearer: a part of a town, not a region.
      const near = size === "large" ? 6 : size === "medium" ? (streets ? 15 : 9.6) : streets ? 13 : 8.6;
      setView(fitView(points, area.width, area.height, closest ?? near));
    }
  }, [key]);

  // The marks that stand on each other at this zoom become one mark.
  const groups = useMemo(() => {
    const out: Group[] = [];
    if (!view) {
      return out;
    }
    const sorted = [...points].sort((a, b) => (b.mark.count ?? 1) - (a.mark.count ?? 1));
    for (const point of sorted) {
      const [x, y] = toScreen(view, point.x, point.y, area.width, area.height);
      const near = out.find((group) => Math.hypot(group.x - x, group.y - y) < NEAR);
      if (near) {
        near.marks.push(point.mark);
        near.count += point.mark.count ?? 1;
      } else {
        out.push({ marks: [point.mark], x, y, count: point.mark.count ?? 1 });
      }
    }
    return out.filter(
      (group) => group.x > -20 && group.x < area.width + 20 && group.y > -20 && group.y < area.height + 20,
    );
  }, [points, view, area.width, area.height]);

  useEffect(() => {
    const element = canvas.current;
    const context = element?.getContext("2d");
    if (!element || !context || !world || !view || area.width <= 0 || area.height <= 0) {
      return;
    }
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    element.width = Math.round(area.width * ratio);
    element.height = Math.round(area.height * ratio);
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
    // The name of a place with photos stands under its mark.
    const taken = groups.map((group) => ({
      left: group.x - 60,
      right: group.x + 60,
      top: group.y - 16,
      bottom: group.y + 30,
    }));
    if (streets) {
      drawStreets(context, view, area.width, area.height, readColors(element), () =>
        setTheme((value) => value + 1),
      );
      return;
    }
    drawWorld(
      context,
      world,
      view,
      area.width,
      area.height,
      readColors(element),
      locale.value,
      taken,
      towns?.list ?? null,
      zoomOf(view.scale) >= DETAIL_ZOOM
        ? detailSquares(viewBox(view, area.width, area.height))
            .map(([row, column]) => knownDetail(row, column))
            .filter((detail) => detail !== null)
        : [],
    );
  }, [world, view, area.width, area.height, theme, locale.value, groups, towns, streets]);

  // Close to the ground the map loads the squares with roads, small rivers
  // and the borders of regions. Each square is a file of the app.
  useEffect(() => {
    if (streets || !view || area.width <= 0 || zoomOf(view.scale) < DETAIL_ZOOM) {
      return;
    }
    const wanted = detailSquares(viewBox(view, area.width, area.height)).filter(
      ([row, column]) => knownDetail(row, column) === null,
    );
    if (wanted.length === 0 || wanted.length > 48) {
      return;
    }
    let active = true;
    const timer = window.setTimeout(() => {
      for (const [row, column] of wanted) {
        loadDetail(row, column)
          .then(() => {
            if (active) {
              setTheme((value) => value + 1);
            }
          })
          .catch(() => {
            // Without the square the map shows what it has.
          });
      }
    }, 120);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [view, area.width, area.height, streets]);

  // Close to the ground the map asks the server for the towns and the
  // villages of the part of the world that it shows. The question has only a
  // box of the globe on a coarse grid, and the answer is public data.
  useEffect(() => {
    if (streets || !view || area.width <= 0 || zoomOf(view.scale) < TOWN_ZOOM) {
      return;
    }
    const box = viewBox(view, area.width, area.height);
    // A wider box on a grid of a quarter of a degree: a small move of the map asks nothing new.
    const grid = 0.25;
    const padLat = (box.north - box.south) * 0.3;
    const padLon = (box.east - box.west) * 0.3;
    const asked = {
      south: Math.max(Math.floor((box.south - padLat) / grid) * grid, -90),
      west: Math.max(Math.floor((box.west - padLon) / grid) * grid, -180),
      north: Math.min(Math.ceil((box.north + padLat) / grid) * grid, 90),
      east: Math.min(Math.ceil((box.east + padLon) / grid) * grid, 180),
    };
    const key = `${asked.south},${asked.west},${asked.north},${asked.east}`;
    if (towns?.key === key || asked.north - asked.south > 8 || asked.east - asked.west > 16) {
      return;
    }
    const known = townCache.get(key);
    if (known) {
      setTowns({ key, list: known });
      return;
    }
    let active = true;
    const timer = window.setTimeout(() => {
      server
        .placesAround(asked)
        .then((found) => {
          const list = found.map((place) => ({
            name: place.name,
            nameRu: place.nameRu ?? "",
            x: mapX(place.lon),
            y: mapY(place.lat),
            people: place.people ?? 0,
          }));
          if (townCache.size > 40) {
            townCache.clear();
          }
          townCache.set(key, list);
          if (active) {
            setTowns({ key, list });
          }
        })
        .catch(() => {
          // Without the names the map still shows the outlines and the mark.
        });
    }, 180);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [view, area.width, area.height]);

  // The wheel changes the zoom. The listener must be able to stop the scroll of the page.
  useEffect(() => {
    const element = canvas.current;
    if (!element || !moves) {
      return;
    }
    const wheel = (event: WheelEvent): void => {
      event.preventDefault();
      const rect = element.getBoundingClientRect();
      setView(
        (now) =>
          now &&
          zoomAt(
            now,
            Math.exp(-event.deltaY * 0.0016),
            event.clientX - rect.left,
            event.clientY - rect.top,
            rect.width,
            rect.height,
          ),
      );
    };
    element.addEventListener("wheel", wheel, { passive: false });
    return () => element.removeEventListener("wheel", wheel);
  }, [moves]);

  const down = (event: PointerEvent): void => {
    if (!moves) {
      return;
    }
    (event.currentTarget as Element).setPointerCapture(event.pointerId);
    press.current = pointers.current.size === 0 ? { x: event.clientX, y: event.clientY, moved: 0 } : null;
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
  };
  const move = (event: PointerEvent): void => {
    const before = pointers.current.get(event.pointerId);
    if (!moves || !before) {
      return;
    }
    if (press.current) {
      press.current.moved = Math.max(
        press.current.moved,
        Math.hypot(event.clientX - press.current.x, event.clientY - press.current.y),
      );
    }
    const rect = (event.currentTarget as Element).getBoundingClientRect();
    const others = [...pointers.current.entries()].filter(([id]) => id !== event.pointerId);
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    const other = others[0]?.[1];
    if (other) {
      // Two fingers: the distance between them changes the zoom around their middle.
      const was = Math.hypot(before.x - other.x, before.y - other.y);
      const now = Math.hypot(event.clientX - other.x, event.clientY - other.y);
      if (was > 0 && now > 0) {
        const middleX = (event.clientX + other.x) / 2 - rect.left;
        const middleY = (event.clientY + other.y) / 2 - rect.top;
        setView((value) => value && zoomAt(value, now / was, middleX, middleY, rect.width, rect.height));
      }
      return;
    }
    const dx = event.clientX - before.x;
    const dy = event.clientY - before.y;
    setView(
      (value) =>
        value &&
        clampView(
          { ...value, x: value.x - dx / value.scale, y: value.y - dy / value.scale },
          rect.width,
          rect.height,
        ),
    );
  };
  const up = (event: PointerEvent): void => {
    pointers.current.delete(event.pointerId);
    const pressed = press.current;
    press.current = null;
    if (!onPoint || !view || !pressed || pressed.moved > 5 || event.type !== "pointerup") {
      return;
    }
    const rect = (event.currentTarget as Element).getBoundingClientRect();
    const x = view.x + (event.clientX - rect.left - rect.width / 2) / view.scale;
    const y = view.y + (event.clientY - rect.top - rect.height / 2) / view.scale;
    if (x >= 0 && x <= 1 && y >= 0 && y <= 1) {
      onPoint(Math.round(mapLat(y) * 1e4) / 1e4, Math.round(mapLon(x) * 1e4) / 1e4);
    }
  };
  const step = (factor: number): void => {
    setView(
      (value) => value && zoomAt(value, factor, area.width / 2, area.height / 2, area.width, area.height),
    );
  };
  const open = (group: Group): void => {
    const only = group.marks[0];
    if (group.marks.length === 1 && only) {
      onPick?.(only.id);
      return;
    }
    // Several places stand on each other. The map comes closer to tell them apart.
    setView(
      fitView(
        group.marks.map((mark) => ({ x: mapX(mark.lon), y: mapY(mark.lat) })),
        area.width,
        area.height,
        7,
      ),
    );
  };

  return (
    <div class={`map ${size}${moves ? " live" : ""}${onPoint ? " point" : ""}`} ref={box}>
      <canvas
        ref={canvas}
        role="img"
        aria-label={label}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={up}
      />
      {!world && !failed && (
        <div class="map-state">
          <Spinner />
        </div>
      )}
      {failed && <p class="map-state">{t("mapFailed")}</p>}
      {world &&
        groups.map((group) => {
          const first = group.marks[0];
          if (!first) {
            return null;
          }
          const name = group.marks.length === 1 ? first.label : t("mapPlaces", { count: group.marks.length });
          const style = { left: `${group.x}px`, top: `${group.y}px` };
          const body = (
            <>
              <span class="map-dot">{first.count !== undefined ? group.count : ""}</span>
              <span class="map-name">{name}</span>
            </>
          );
          return interactive ? (
            <button
              type="button"
              class="map-mark"
              key={first.id}
              style={style}
              aria-label={
                group.marks.length === 1
                  ? t("mapMark", { name: first.label, count: group.count })
                  : t("mapGroup", { places: group.marks.length, count: group.count })
              }
              onClick={() => open(group)}
            >
              {body}
            </button>
          ) : (
            <span class="map-mark" key={first.id} style={style}>
              {body}
            </span>
          );
        })}
      {moves && world && (
        <div class="map-tools">
          <button type="button" class="tool" aria-label={t("zoomIn")} onClick={() => step(1.7)}>
            <Icon name="plus" />
          </button>
          <button type="button" class="tool" aria-label={t("zoomOut")} onClick={() => step(1 / 1.7)}>
            <Icon name="minus" />
          </button>
          <button
            type="button"
            class="tool"
            aria-label={t("mapFit")}
            onClick={() => setView(fitView(points, area.width, area.height))}
          >
            <Icon name="corners-out" />
          </button>
        </div>
      )}
    </div>
  );
}

/** Says where the map and the names of the places come from. */
export function MapCredit() {
  const account = me.value;
  return (
    <p class="map-credit">
      {account?.mapTiles ? t("mapCreditStreets", { credit: account.mapCredit ?? "" }) : t("mapCredit")}
    </p>
  );
}
