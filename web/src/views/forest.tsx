import { useMemo, useRef, useState } from "preact/hooks";
import { usePanZoom } from "../components/panzoom";
import { locale, plural, t } from "../i18n";
import { layoutWholeTree } from "../lib/forest";
import { fullName } from "../lib/format";
import type { GraphIndex } from "../lib/graph";
import { link } from "../lib/router";
import { linkPath, Node, PersonPreview } from "./chart";
import { ChartHint, ZoomControls } from "./tree";

/** Shows the whole tree of a person on one canvas: everybody who is linked to the person, with all the links. */
export function ForestView({ index, id }: { index: GraphIndex; id: string }) {
  const forest = useMemo(() => layoutWholeTree(index, id), [index, id]);
  const [selected, setSelected] = useState<string | null>(null);
  const svg = useRef<SVGSVGElement>(null);
  const layer = useRef<SVGGElement>(null);
  // The canvas opens whole. The focus is the middle of the tree, so a new main person does not reset the view.
  const { minX, minY, maxX, maxY } = forest.bounds;
  const focus = useMemo(() => ({ x: (minX + maxX) / 2, y: (minY + maxY) / 2 }), [minX, minY, maxX, maxY]);
  const controls = usePanZoom(svg, layer, forest.bounds, focus, 0.5, true);
  const selectedCard = selected ? index.byId.get(selected) : undefined;
  const places = useMemo(() => new Map(forest.nodes.map((node) => [node.id, node])), [forest]);

  const select = (person: string): void => {
    if (!controls.current.wasDragged()) {
      setSelected(person);
    }
  };

  return (
    <div class="chart-wrap">
      {forest.others > 0 && <p class="chart-tools chart-note">{plural("wholeOthers", forest.others)}</p>}
      {/* biome-ignore lint/a11y/noNoninteractiveTabindex: the chart takes arrow keys for panning */}
      <svg ref={svg} class="chart forest" tabIndex={0} aria-label={t("chartLabel")} role="application">
        <defs>
          <clipPath id="node-photo-clip" clipPathUnits="objectBoundingBox">
            <circle cx="0.5" cy="0.5" r="0.5" />
          </clipPath>
        </defs>
        <g ref={layer}>
          {forest.links.map((item) => (
            <path key={item.key} class={`link ${item.kind}`} d={linkPath(item)} />
          ))}
          {forest.nodes.map((node) => {
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
        {forest.nodes.map((node) => {
          const card = index.byId.get(node.id);
          return card ? (
            <li key={node.id}>
              <a href={link("people", node.id)}>{fullName(card, locale.value) || t("unnamed")}</a>
            </li>
          ) : null;
        })}
      </ul>
      {selectedCard && (
        <PersonPreview
          card={selectedCard}
          view="all"
          onClose={() => setSelected(null)}
          onCenter={() => {
            // The view goes to the person at a size that people can read.
            const node = places.get(selectedCard.id);
            if (node) {
              controls.current.focusOn(node.x, node.y);
            }
          }}
        />
      )}
      <ChartHint chart={svg} />
    </div>
  );
}
