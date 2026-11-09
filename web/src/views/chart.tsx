import { useMemo, useRef, useState } from "preact/hooks";
import { mediaImage } from "../api/client";
import type { Card } from "../api/types";
import { usePanZoom } from "../components/panzoom";
import { Sheet } from "../components/sheet";
import { Avatar, displayName } from "../components/ui";
import { locale, t } from "../i18n";
import { fullName, initials, lifeYears, surnameLine } from "../lib/format";
import type { GraphIndex } from "../lib/graph";
import { type ChartLink, type ChartNode, layoutTree, NODE_H, NODE_W } from "../lib/layout";
import { link } from "../lib/router";
import { navigate } from "../state";
import { ChartHint, GenerationStepper, treeHref, ZoomControls } from "./tree";

export type ChartKind = "pedigree" | "descendants" | "hourglass";

const limits: Record<ChartKind, { up: number; down: number; min: number; max: number }> = {
  pedigree: { up: 4, down: 0, min: 1, max: 8 },
  descendants: { up: 0, down: 3, min: 1, max: 8 },
  hourglass: { up: 3, down: 2, min: 1, max: 6 },
};

/** Shortens text to fit a card. SVG text does not wrap. */
const NAME_CHARS = 16;

/** Keeps the given name when the full first line does not fit a card. */
function cardName(card: Card, main: string): string {
  return [...main].length > NAME_CHARS && card.given ? card.given : main;
}

function fit(text: string, max: number): string {
  const chars = [...text];
  return chars.length <= max ? text : `${chars.slice(0, max - 1).join("")}…`;
}

/** A long name in large letters gets narrower letters, so it stays inside the card. */
function squeeze(text: string, max: number): { textLength?: number; lengthAdjust?: string } {
  return [...text].length > max ? { textLength: NODE_W - 20, lengthAdjust: "spacingAndGlyphs" } : {};
}

export function linkPath(item: ChartLink): string {
  const [first, ...rest] = item.points;
  if (!first) {
    return "";
  }
  if (rest.length === 3) {
    const [a, b, c] = rest as [[number, number], [number, number], [number, number]];
    const dx = c[0] - first[0];
    const r = Math.min(12, Math.abs(dx) / 2, Math.abs(a[1] - first[1]));
    if (Math.abs(dx) < 1) {
      return `M${first[0]} ${first[1]}V${c[1]}`;
    }
    const sx = Math.sign(dx);
    const sy = Math.sign(c[1] - first[1]);
    return [
      `M${first[0]} ${first[1]}`,
      `V${a[1] - sy * r}`,
      `Q${a[0]} ${a[1]} ${a[0] + sx * r} ${a[1]}`,
      `H${b[0] - sx * r}`,
      `Q${b[0]} ${b[1]} ${b[0]} ${b[1] + sy * r}`,
      `V${c[1]}`,
    ].join("");
  }
  return `M${first[0]} ${first[1]}${rest.map((point) => `L${point[0]} ${point[1]}`).join("")}`;
}

/** The room for the surname line of a card, in pixels: from the text to the right edge. */
const SECOND_WIDTH = NODE_W - 64 - 10;
/** A surname line up to this length gets narrower letters; a longer one is cut. */
const SECOND_MAX = 25;

/**
 * The surname line of a card, with the surname at birth in brackets. A line
 * that is too long for the card gets narrower letters, and a much longer one
 * is cut at the end.
 */
function secondLine(
  card: Card,
  second: string,
): { text: string; attrs: { textLength?: number; lengthAdjust?: string } } {
  const text = card.birthSurname && second === (card.surname ?? "") ? surnameLine(card) : second;
  const length = [...text].length;
  if (length <= NAME_CHARS + 1) {
    return { text, attrs: {} };
  }
  if (!card.birthSurname) {
    return { text: fit(text, NAME_CHARS + 1), attrs: {} };
  }
  return {
    text: fit(text, SECOND_MAX),
    attrs: { textLength: SECOND_WIDTH, lengthAdjust: "spacingAndGlyphs" },
  };
}

export function Node({
  node,
  card,
  selected,
  onSelect,
}: {
  node: ChartNode;
  card: Card;
  selected: boolean;
  onSelect: (id: string) => void;
}) {
  const name = displayName(card);
  const second = secondLine(card, name.second);
  const years = lifeYears(card, locale.value);
  const classes = ["node", node.side, selected ? "selected" : ""].join(" ");
  // The names for the zoomed-out levels: large letters in place of the full card.
  const first = fit(card.given || card.surname || name.main, 15);
  const last = card.given && card.surname ? fit(card.surname, 18) : "";
  return (
    <g
      class={classes}
      transform={`translate(${node.x - NODE_W / 2} ${node.y - NODE_H / 2})`}
      tabIndex={0}
      role="button"
      aria-label={[fullName(card, locale.value) || t("unnamed"), years].filter(Boolean).join(", ")}
      onClick={() => onSelect(node.id)}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect(node.id);
        }
      }}
    >
      <rect width={NODE_W} height={NODE_H} rx="10" />
      <circle cx="34" cy={NODE_H / 2} r="20" />
      <text class="node-initials" x="34" y={NODE_H / 2 + 5} text-anchor="middle">
        {initials(card)}
      </text>
      {card.photo && !__DEMO__ && (
        <image
          class="node-photo"
          href={mediaImage(card.photo.id, "s", card.photo.region)}
          x="14"
          y={NODE_H / 2 - 20}
          width="40"
          height="40"
          preserveAspectRatio="xMidYMid slice"
          clip-path="url(#node-photo-clip)"
        />
      )}
      <text class="node-name" x="64" y="27">
        {fit(cardName(card, name.main), NAME_CHARS)}
      </text>
      <text class="node-second" x="64" y="45" {...second.attrs}>
        {second.text}
      </text>
      <text class="node-years" x="64" y="62">
        {years}
      </text>
      <text
        class="node-big given"
        x={NODE_W / 2}
        y={last ? 34 : 48}
        text-anchor="middle"
        {...squeeze(first, 10)}
      >
        {first}
      </text>
      {last && (
        <text class="node-big" x={NODE_W / 2} y="61" text-anchor="middle" {...squeeze(last, 13)}>
          {last}
        </text>
      )}
      <text class="node-huge" x={NODE_W / 2} y="54" text-anchor="middle" {...squeeze(first, 7)}>
        {first}
      </text>
    </g>
  );
}

export function PersonPreview({
  card,
  onClose,
  view,
  onCenter,
}: {
  card: Card;
  onClose: () => void;
  view: string;
  /** Runs after the view moved to this person. */
  onCenter?: () => void;
}) {
  const name = displayName(card);
  return (
    <Sheet onClose={onClose}>
      <div class="preview">
        <Avatar card={card} size="m" />
        <div class="preview-text">
          <h2>
            {name.main} {surnameLine(card) || name.second}
          </h2>
          <p>{lifeYears(card, locale.value)}</p>
        </div>
      </div>
      <div class="preview-actions">
        <a class="button filled" href={link("people", card.id)}>
          {t("openPage")}
        </a>
        <button
          type="button"
          class="button tinted"
          onClick={() => {
            onClose();
            navigate(treeHref(card.id, view));
            onCenter?.();
          }}
        >
          {t("centerHere")}
        </button>
      </div>
    </Sheet>
  );
}

export function ChartView({ index, id, kind }: { index: GraphIndex; id: string; kind: ChartKind }) {
  const settings = limits[kind];
  const [extra, setExtra] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const up = settings.up > 0 ? Math.min(settings.max, Math.max(settings.min, settings.up + extra)) : 0;
  const down = settings.down > 0 ? Math.min(settings.max, Math.max(settings.min, settings.down + extra)) : 0;
  const chart = useMemo(() => layoutTree(index, id, up, down), [index, id, up, down]);
  const svg = useRef<SVGSVGElement>(null);
  const layer = useRef<SVGGElement>(null);
  const root = chart.nodes.find((node) => node.id === id);
  const controls = usePanZoom(
    svg,
    layer,
    chart.bounds,
    { x: root?.x ?? 0, y: root?.y ?? 0 },
    kind === "pedigree" ? 0.8 : kind === "descendants" ? 0.2 : 0.5,
  );
  const generations = Math.max(up, down);
  const selectedCard = selected ? index.byId.get(selected) : undefined;

  const select = (person: string): void => {
    if (!controls.current.wasDragged()) {
      setSelected(person);
    }
  };

  return (
    <div class="chart-wrap">
      <div class="chart-tools">
        <GenerationStepper
          value={generations}
          min={settings.min}
          max={settings.max}
          onChange={(value) => setExtra(value - (settings.up > 0 ? settings.up : settings.down))}
        />
      </div>
      {/* biome-ignore lint/a11y/noNoninteractiveTabindex: the chart takes arrow keys for panning */}
      <svg ref={svg} class="chart" tabIndex={0} aria-label={t("chartLabel")} role="application">
        <defs>
          <clipPath id="node-photo-clip" clipPathUnits="objectBoundingBox">
            <circle cx="0.5" cy="0.5" r="0.5" />
          </clipPath>
        </defs>
        <g ref={layer}>
          {chart.links.map((item) => (
            <path key={item.key} class={`link ${item.kind}`} d={linkPath(item)} />
          ))}
          {chart.nodes.map((node) => {
            const card = index.byId.get(node.id);
            return card ? (
              <Node key={node.id} node={node} card={card} selected={selected === node.id} onSelect={select} />
            ) : null;
          })}
        </g>
      </svg>
      <ZoomControls
        onZoom={(factor) => controls.current.zoomBy(factor)}
        onFit={() => controls.current.fit()}
      />
      <ul class="sr-only">
        {chart.nodes.map((node) => {
          const card = index.byId.get(node.id);
          return card ? (
            <li key={node.id}>
              <a href={link("people", node.id)}>{fullName(card, locale.value) || t("unnamed")}</a>
            </li>
          ) : null;
        })}
      </ul>
      {selectedCard && <PersonPreview card={selectedCard} view={kind} onClose={() => setSelected(null)} />}
      <ChartHint chart={svg} />
    </div>
  );
}
