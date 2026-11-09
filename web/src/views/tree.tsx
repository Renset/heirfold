import type { RefObject } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { Icon } from "../components/icons";
import { EmptyState } from "../components/ui";
import { t } from "../i18n";
import { startPerson } from "../lib/graph";
import { link, route } from "../lib/router";
import { graph, me, navigate } from "../state";
import { type ChartKind, ChartView } from "./chart";
import { FamilyView } from "./family";
import { FanView } from "./fan";
import { ForestView } from "./forest";

type ViewName = "family" | ChartKind | "fan" | "all";

const viewNames: ViewName[] = ["family", "pedigree", "descendants", "hourglass", "fan", "all"];

function viewLabel(view: ViewName): string {
  switch (view) {
    case "family":
      return t("viewFamily");
    case "pedigree":
      return t("viewPedigree");
    case "descendants":
      return t("viewDescendants");
    case "hourglass":
      return t("viewHourglass");
    case "fan":
      return t("viewFan");
    case "all":
      return t("viewAll");
  }
}

function currentView(): ViewName {
  const requested = route.value.query.get("view");
  return viewNames.find((name) => name === requested) ?? "family";
}

export function treeHref(id: string, view?: string): string {
  return view && view !== "family" ? `${link("tree", id)}?view=${view}` : link("tree", id);
}

function ViewTabs({ id, view }: { id: string; view: ViewName }) {
  const row = useRef<HTMLDivElement>(null);
  // On a phone the row scrolls. Keep the open view in sight.
  useEffect(() => {
    const element = row.current;
    const active = element?.querySelector<HTMLElement>('[aria-selected="true"]');
    if (element && active) {
      element.scrollLeft = active.offsetLeft - (element.clientWidth - active.offsetWidth) / 2;
    }
  }, [view]);
  return (
    <div ref={row} class="tabs" role="tablist" aria-label={t("viewsLabel")}>
      {viewNames.map((name) => (
        <a
          key={name}
          role="tab"
          aria-selected={name === view}
          href={treeHref(id, name)}
          onClick={(event) => {
            event.preventDefault();
            navigate(treeHref(id, name));
          }}
        >
          {viewLabel(name)}
        </a>
      ))}
    </div>
  );
}

export function TreeView({ personId }: { personId?: string }) {
  const index = graph.value;
  if (!index) {
    return null;
  }
  const id = personId && index.byId.has(personId) ? personId : startPerson(index, me.value?.user.person);
  if (!id) {
    const canImport = me.value?.importTree ?? false;
    const canEdit = (me.value?.edit ?? false) && !__DEMO__;
    return (
      <div class="center-state">
        <EmptyState icon="tree-structure" title={t("emptyTree")}>
          <p>{canEdit ? t("emptyTreeEditor") : canImport ? t("emptyTreeOwner") : t("emptyTreeGuest")}</p>
          {canEdit && (
            <a class="button filled" href={link("people", "new")}>
              {t("personAdd")}
            </a>
          )}
          {canImport && (
            <a class={canEdit ? "button" : "button filled"} href={link("settings", "import")}>
              {t("goImport")}
            </a>
          )}
        </EmptyState>
      </div>
    );
  }
  const view = currentView();
  return (
    <div class={view === "family" ? "tree-page" : "tree-page chart-page"}>
      <ViewTabs id={id} view={view} />
      {view === "family" && <FamilyView index={index} id={id} />}
      {(view === "pedigree" || view === "descendants" || view === "hourglass") && (
        <ChartView index={index} id={id} kind={view} />
      )}
      {view === "fan" && <FanView index={index} id={id} />}
      {view === "all" && <ForestView index={index} id={id} />}
    </div>
  );
}

export function ZoomControls({ onZoom, onFit }: { onZoom: (factor: number) => void; onFit: () => void }) {
  return (
    <div class="zoom">
      <button type="button" aria-label={t("zoomIn")} onClick={() => onZoom(1.3)}>
        <Icon name="plus" />
      </button>
      <button type="button" aria-label={t("zoomOut")} onClick={() => onZoom(0.77)}>
        <Icon name="minus" />
      </button>
      <button type="button" aria-label={t("fitTree")} onClick={onFit}>
        <Icon name="corners-out" />
      </button>
    </div>
  );
}

const hintKey = "heirfold.hint.chart";

function hintSeen(): boolean {
  try {
    return localStorage.getItem(hintKey) === "1";
  } catch {
    return false;
  }
}

/**
 * Tells a new reader how to move a chart. The text fits the device. The hint
 * goes away at the first move of the chart and does not come back.
 */
export function ChartHint({ chart }: { chart: RefObject<SVGSVGElement | null> }) {
  const [open, setOpen] = useState(() => !hintSeen());
  const close = (): void => {
    setOpen(false);
    try {
      localStorage.setItem(hintKey, "1");
    } catch {
      // Without storage the hint shows again on the next visit.
    }
  };
  const done = useRef(close);
  done.current = close;
  useEffect(() => {
    const element = chart.current;
    if (!open || !element) {
      return;
    }
    const names = ["pointerdown", "wheel", "keydown"] as const;
    const onUse = (): void => done.current();
    for (const name of names) {
      element.addEventListener(name, onUse, { passive: true });
    }
    return () => {
      for (const name of names) {
        element.removeEventListener(name, onUse);
      }
    };
  }, [open, chart]);
  if (!open) {
    return null;
  }
  const touch = matchMedia("(pointer: coarse)").matches;
  return (
    <div class="chart-hint" role="note">
      <p>{t(touch ? "chartHintTouch" : "chartHintMouse")}</p>
      <button type="button" aria-label={t("hintClose")} onClick={close}>
        <Icon name="x" />
      </button>
    </div>
  );
}

export function GenerationStepper({
  value,
  min,
  max,
  onChange,
}: {
  value: number;
  min: number;
  max: number;
  onChange: (value: number) => void;
}) {
  return (
    <div class="stepper" role="group" aria-label={t("generations")}>
      <button
        type="button"
        aria-label={t("zoomOut")}
        disabled={value <= min}
        onClick={() => onChange(value - 1)}
      >
        <Icon name="minus" />
      </button>
      <span aria-live="polite">{t("generationsCount", { count: value })}</span>
      <button
        type="button"
        aria-label={t("zoomIn")}
        disabled={value >= max}
        onClick={() => onChange(value + 1)}
      >
        <Icon name="plus" />
      </button>
    </div>
  );
}
