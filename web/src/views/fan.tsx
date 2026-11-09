import { useMemo, useRef, useState } from "preact/hooks";
import type { Card } from "../api/types";
import { usePanZoom } from "../components/panzoom";
import { displayName } from "../components/ui";
import { locale, t } from "../i18n";
import { fullName, initials, lifeYears, surnameLine } from "../lib/format";
import type { GraphIndex } from "../lib/graph";
import {
  FAN_CENTER,
  FAN_SWEEP,
  type FanRing,
  type FanSegment,
  fanArc,
  fanPath,
  fanRings,
  layoutFan,
} from "../lib/layout";
import { link } from "../lib/router";
import { PersonPreview } from "./chart";
import { ChartHint, GenerationStepper, ZoomControls } from "./tree";

const CENTER_R = FAN_CENTER;

function clip(text: string, max: number): string {
  const chars = [...text];
  return chars.length <= max ? text : `${chars.slice(0, Math.max(1, max - 1)).join("")}…`;
}

/** Text that bends along the arc of a ring, at a right angle to the radius. */
function CurvedLabel({
  segment,
  ring,
  card,
  uid,
}: {
  segment: FanSegment;
  ring: FanRing;
  card: Card;
  uid: string;
}) {
  const name = displayName(card);
  const large = segment.generation <= 2;
  const size = large ? 13 : 11;
  const step = large ? 16 : 13.5;
  const lines = [
    { text: name.main, bold: true },
    { text: surnameLine(card) || name.second, bold: false },
    { text: lifeYears(card, locale.value), bold: false },
  ].filter((line) => line.text);
  const middle = (ring.inner + ring.outer) / 2;
  const span = ((segment.end - segment.start) * Math.PI) / 180;
  // In the lower half the arc runs the other way, so the text stays upright.
  const flip = Math.abs((segment.start + segment.end) / 2) > 90;
  return (
    <g>
      {lines.map((line, row) => {
        const fromTop = (lines.length - 1) / 2 - row;
        const radius = flip ? middle - fromTop * step + size * 0.35 : middle + fromTop * step - size * 0.35;
        const room = span * (flip ? radius - size * 0.7 : radius) - 14;
        const width = size * (line.bold ? 0.62 : 0.57);
        const id = `${uid}-${row}`;
        return (
          <g key={id}>
            <path id={id} class="fan-guide" d={fanArc(segment.start, segment.end, radius, flip)} />
            <text
              class={`${line.bold ? "fan-name" : "fan-second"}${large ? "" : " small"}`}
              text-anchor="middle"
            >
              <textPath href={`#${id}`} startOffset="50%">
                {clip(line.text, Math.max(2, Math.floor(room / width)))}
              </textPath>
            </text>
          </g>
        );
      })}
    </g>
  );
}

/** Text along the radius, for rings whose segments are too narrow for an arc. */
function RadialLabel({ segment, ring, card }: { segment: FanSegment; ring: FanRing; card: Card }) {
  const name = displayName(card);
  const middle = (segment.start + segment.end) / 2;
  // The text reads outward on the right and inward on the left.
  const left = middle < 0;
  const span = segment.end - segment.start;
  const twoLines = span * (Math.PI / 180) * ((ring.inner + ring.outer) / 2) > 26;
  return (
    <g
      transform={`rotate(${middle - 90}) translate(${left ? ring.outer - 8 : ring.inner + 8} 0)${left ? " rotate(180)" : ""}`}
    >
      <text class="fan-name small" y={twoLines ? -2 : 4}>
        {clip(name.main, 15)}
      </text>
      {twoLines && (
        <text class="fan-second small" y={11}>
          {clip(name.second, 15)}
        </text>
      )}
    </g>
  );
}

export function FanView({ index, id }: { index: GraphIndex; id: string }) {
  const [generations, setGenerations] = useState(4);
  const [selected, setSelected] = useState<string | null>(null);
  const fan = useMemo(() => layoutFan(index, id, generations), [index, id, generations]);
  const svg = useRef<SVGSVGElement>(null);
  const layer = useRef<SVGGElement>(null);
  const rings = useMemo(() => fanRings(generations), [generations]);
  const outer = rings[rings.length - 1]?.outer ?? CENTER_R;
  const half = (FAN_SWEEP / 2) * (Math.PI / 180);
  const lowest = -Math.cos(half) * outer;
  const bounds = { minX: -outer, maxX: outer, minY: -outer, maxY: Math.max(lowest, CENTER_R + 32) };
  const controls = usePanZoom(svg, layer, bounds, { x: 0, y: 0 }, 0.66);
  const root = index.byId.get(id);
  const selectedCard = selected ? index.byId.get(selected) : undefined;

  const select = (person: string): void => {
    if (!controls.current.wasDragged()) {
      setSelected(person);
    }
  };

  return (
    <div class="chart-wrap">
      <div class="chart-tools">
        <GenerationStepper value={generations} min={2} max={6} onChange={setGenerations} />
      </div>
      {/* biome-ignore lint/a11y/noNoninteractiveTabindex: the chart takes arrow keys for panning */}
      <svg ref={svg} class="chart fan" tabIndex={0} aria-label={t("chartLabel")} role="application">
        <g ref={layer}>
          {fan.segments.map((segment, position) => {
            const card = index.byId.get(segment.id);
            const ring = rings[segment.generation - 1];
            if (!card || !ring) {
              return null;
            }
            return (
              <g
                key={segment.id}
                class={`fan-segment ${segment.side} g${Math.min(segment.generation, 6)}${selected === segment.id ? " selected" : ""}`}
                tabIndex={0}
                role="button"
                aria-label={[fullName(card, locale.value) || t("unnamed"), lifeYears(card, locale.value)]
                  .filter(Boolean)
                  .join(", ")}
                onClick={() => select(segment.id)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    select(segment.id);
                  }
                }}
              >
                <path class="fan-shape" d={fanPath(segment.start, segment.end, ring.inner, ring.outer)} />
                {ring.curved ? (
                  <CurvedLabel segment={segment} ring={ring} card={card} uid={`fan-arc-${position}`} />
                ) : (
                  <RadialLabel segment={segment} ring={ring} card={card} />
                )}
              </g>
            );
          })}
          {root && (
            <g
              class={`fan-root${selected === id ? " selected" : ""}`}
              tabIndex={0}
              role="button"
              aria-label={fullName(root, locale.value) || t("unnamed")}
              onClick={() => select(id)}
            >
              <circle r={CENTER_R - 4} />
              <text class="fan-root-initials" y="-14" text-anchor="middle">
                {initials(root)}
              </text>
              <text class="fan-root-name" y="10" text-anchor="middle">
                {clip(displayName(root).main, 14)}
              </text>
              <text class="fan-root-second" y="28" text-anchor="middle">
                {clip(displayName(root).second, 14)}
              </text>
            </g>
          )}
        </g>
      </svg>
      <ZoomControls
        onZoom={(factor) => controls.current.zoomBy(factor)}
        onFit={() => controls.current.fit()}
      />
      <ChartHint chart={svg} />
      <ul class="sr-only">
        {fan.segments.map((segment) => {
          const card = index.byId.get(segment.id);
          return card ? (
            <li key={segment.id}>
              <a href={link("people", segment.id)}>{fullName(card, locale.value) || t("unnamed")}</a>
            </li>
          ) : null;
        })}
      </ul>
      {selectedCard && <PersonPreview card={selectedCard} view="fan" onClose={() => setSelected(null)} />}
    </div>
  );
}
