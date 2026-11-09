import { useLayoutEffect, useRef, useState } from "preact/hooks";
import type { Card } from "../api/types";
import { Avatar, displayName } from "../components/ui";
import { locale, t } from "../i18n";
import { birthNote, fullName, lifeYears, surnameLine } from "../lib/format";
import { type GraphIndex, parentsOf, siblingsOf, unitsOf } from "../lib/graph";
import { link } from "../lib/router";
import { navigate } from "../state";

type Relation = "father" | "mother" | "grandfather" | "grandmother" | "sibling" | "spouse" | "child";

function relationLabel(relation: Relation, gender: string | undefined): string {
  const male = gender === "M";
  const female = gender === "F";
  switch (relation) {
    case "father":
      return t("relFather");
    case "mother":
      return t("relMother");
    case "grandfather":
      return t("relGrandfather");
    case "grandmother":
      return t("relGrandmother");
    case "sibling":
      return male ? t("relBrother") : female ? t("relSister") : t("relSibling");
    case "spouse":
      return male ? t("relHusband") : female ? t("relWife") : t("relSpouse");
    case "child":
      return male ? t("relSon") : female ? t("relDaughter") : t("relChild");
  }
}

/** Gives a shared portrait a transition name, once per person on the page. */
function transitionName(id: string, used: Set<string>): string | undefined {
  if (used.has(id)) {
    return undefined;
  }
  used.add(id);
  return `p-${id.replace(/[^a-zA-Z0-9_-]/g, "_")}`;
}

function Relative({
  card,
  relation,
  size,
  used,
}: {
  card: Card;
  relation: Relation;
  size: "s" | "m";
  used: Set<string>;
}) {
  const name = displayName(card);
  const href = link("tree", card.id);
  const portraitName = transitionName(card.id, used);
  return (
    <a
      class={`relative ${size}`}
      href={href}
      data-node={card.id}
      aria-label={`${relationLabel(relation, card.gender)}: ${fullName(card, locale.value) || t("unnamed")}`}
      onClick={(event) => {
        event.preventDefault();
        navigate(href, true);
      }}
    >
      <span class="portrait" style={{ viewTransitionName: portraitName }}>
        <Avatar card={card} size={size} />
      </span>
      <span class="relative-text">
        <span class="relative-label">{relationLabel(relation, card.gender)}</span>
        <span class="relative-name">{name.main}</span>
        {size === "m" && <span class="relative-surname">{surnameLine(card) || name.second}</span>}
        {size === "m" && <span class="relative-years">{lifeYears(card, locale.value)}</span>}
      </span>
    </a>
  );
}

interface Connection {
  from: string[];
  to: string[];
}

interface LinePaths {
  paths: string[];
  width: number;
  height: number;
}

const RADIUS = 10;

/** Draws a line from a parent down to the shared horizontal line. */
function toBus(x: number, y0: number, trunkX: number, busY: number): string {
  const dx = trunkX - x;
  if (Math.abs(dx) < 1) {
    return `M${x} ${y0}V${busY}`;
  }
  const direction = Math.sign(dx);
  const r = Math.min(RADIUS, Math.abs(dx) / 2, Math.abs(busY - y0));
  return `M${x} ${y0}V${busY - r}Q${x} ${busY} ${x + direction * r} ${busY}H${trunkX}`;
}

/** Draws a line from the shared horizontal line down to a child. */
function fromBus(trunkX: number, busY: number, x: number, y1: number): string {
  const dx = x - trunkX;
  if (Math.abs(dx) < 1) {
    return `M${trunkX} ${busY}V${y1}`;
  }
  const direction = Math.sign(dx);
  const r = Math.min(RADIUS, Math.abs(dx) / 2, Math.abs(y1 - busY));
  return `M${trunkX} ${busY}H${x - direction * r}Q${x} ${busY} ${x} ${busY + r}V${y1}`;
}

function measure(container: HTMLElement, connections: Connection[], couples: [string, string][]): LinePaths {
  const base = container.getBoundingClientRect();
  const shown = (rect: DOMRect | undefined): DOMRect | undefined =>
    rect && rect.width > 0 ? rect : undefined;
  const node = (id: string): Element | null => container.querySelector(`[data-node="${CSS.escape(id)}"]`);
  const portrait = (id: string): DOMRect | undefined =>
    shown(node(id)?.querySelector(".avatar")?.getBoundingClientRect());
  // The lines below the main person start under the whole row, so that they
  // never cross a spouse who stands under the portrait on a phone.
  const lower = (id: string): DOMRect | undefined => {
    const element = node(id);
    return shown((element?.closest(".center") ?? element)?.getBoundingClientRect());
  };
  const hero = (id: string): DOMRect | undefined => node(id)?.closest(".hero")?.getBoundingClientRect();
  const centre = (rect: DOMRect): number => rect.left + rect.width / 2 - base.left;
  const paths: string[] = [];
  for (const connection of connections) {
    const sources = connection.from
      .map((id) => ({ avatar: portrait(id), box: lower(id) }))
      .filter((item): item is { avatar: DOMRect; box: DOMRect } => Boolean(item.avatar && item.box));
    // A line to the main person stops at the edge of the card.
    const targets = connection.to
      .map((id) => {
        const avatar = portrait(id);
        return avatar ? { x: centre(avatar), top: (hero(id) ?? avatar).top - base.top - 5 } : undefined;
      })
      .filter((item): item is { x: number; top: number } => Boolean(item));
    if (sources.length === 0 || targets.length === 0) {
      continue;
    }
    const bottom = Math.max(...sources.map((item) => item.box.bottom)) - base.top;
    const firstRow = Math.min(...targets.map((item) => item.top));
    const row = targets.filter((item) => item.top < firstRow + 4);
    const busY = (bottom + firstRow) / 2 + 2;
    const trunkX = sources.reduce((sum, item) => sum + centre(item.avatar), 0) / sources.length;
    for (const item of sources) {
      paths.push(toBus(centre(item.avatar), item.box.bottom - base.top + 4, trunkX, busY));
    }
    for (const item of row) {
      paths.push(fromBus(trunkX, busY, item.x, item.top));
    }
  }
  for (const [a, b] of couples) {
    const left = portrait(a);
    const right = portrait(b);
    if (left && right && Math.abs(left.top + left.height / 2 - (right.top + right.height / 2)) < 48) {
      const y = right.top + right.height / 2 - base.top;
      const start = (hero(a)?.right ?? left.right + 4) - base.left;
      paths.push(`M${start} ${y}H${right.left - base.left - 4}`);
    }
  }
  return { paths, width: base.width, height: base.height };
}

export function FamilyView({ index, id }: { index: GraphIndex; id: string }) {
  const container = useRef<HTMLDivElement>(null);
  const [lines, setLines] = useState<LinePaths>({ paths: [], width: 0, height: 0 });
  const card = index.byId.get(id);
  const used = new Set<string>();

  const { father, mother } = parentsOf(index, id);
  const paternal = father ? parentsOf(index, father.id) : {};
  const maternal = mother ? parentsOf(index, mother.id) : {};
  const units = unitsOf(index, id);
  const siblings = siblingsOf(index, id);
  const spouses = units.map((unit) => unit.spouse).filter((spouse): spouse is Card => Boolean(spouse));
  const children = units.flatMap((unit) => unit.children);

  const connections: Connection[] = [];
  const ids = (cards: (Card | undefined)[]): string[] =>
    cards.filter((item): item is Card => Boolean(item)).map((item) => item.id);
  if (father && (paternal.father || paternal.mother)) {
    connections.push({ from: ids([paternal.father, paternal.mother]), to: [father.id] });
  }
  if (mother && (maternal.father || maternal.mother)) {
    connections.push({ from: ids([maternal.father, maternal.mother]), to: [mother.id] });
  }
  if (father || mother) {
    connections.push({ from: ids([father, mother]), to: [id] });
  }
  if (children.length > 0) {
    connections.push({ from: [id], to: children.map((child) => child.id) });
  }

  const signature = JSON.stringify([connections, spouses.map((spouse) => spouse.id)]);
  useLayoutEffect(() => {
    const element = container.current;
    if (!element) {
      return;
    }
    const update = (): void =>
      setLines(
        measure(
          element,
          connections,
          spouses.map((spouse) => [id, spouse.id]),
        ),
      );
    update();
    const observer = new ResizeObserver(update);
    observer.observe(element);
    return () => observer.disconnect();
  }, [signature]);

  if (!card) {
    return null;
  }
  const name = displayName(card);
  const grandparents = [paternal.father, paternal.mother, maternal.father, maternal.mother];
  const hasGrandparents = grandparents.some(Boolean);
  const portraitName = transitionName(id, used);

  return (
    <div class="family" ref={container}>
      <svg class="family-lines" width={lines.width} height={lines.height} aria-hidden="true">
        <path d={lines.paths.join("")} />
      </svg>

      {hasGrandparents && (
        <div class="family-row grandparents">
          <div class="pair">
            {paternal.father && (
              <Relative card={paternal.father} relation="grandfather" size="s" used={used} />
            )}
            {paternal.mother && (
              <Relative card={paternal.mother} relation="grandmother" size="s" used={used} />
            )}
          </div>
          <div class="pair">
            {maternal.father && (
              <Relative card={maternal.father} relation="grandfather" size="s" used={used} />
            )}
            {maternal.mother && (
              <Relative card={maternal.mother} relation="grandmother" size="s" used={used} />
            )}
          </div>
        </div>
      )}

      {father || mother ? (
        <div class="family-row parents">
          {father ? <Relative card={father} relation="father" size="m" used={used} /> : <span />}
          {mother ? <Relative card={mother} relation="mother" size="m" used={used} /> : <span />}
        </div>
      ) : (
        <p class="family-none">{t("noParents")}</p>
      )}

      <div class="family-row center">
        <section class="hero" data-node={id} aria-label={fullName(card, locale.value)}>
          <span class="portrait" style={{ viewTransitionName: portraitName }}>
            <Avatar card={card} size="l" />
          </span>
          <h1>
            {name.main}
            {name.second && <span>{name.second}</span>}
          </h1>
          {card.birthSurname && <p class="hero-born">{birthNote(card, locale.value)}</p>}
          <p class="hero-years">{lifeYears(card, locale.value)}</p>
          <a class="button filled" href={link("people", id)}>
            {t("openPage")}
          </a>
        </section>
        {spouses.length > 0 && (
          <div class="spouses">
            {spouses.map((spouse) => (
              <Relative key={spouse.id} card={spouse} relation="spouse" size="m" used={used} />
            ))}
          </div>
        )}
      </div>

      {children.length > 0 ? (
        <div class="family-row children">
          {children.map((child) => (
            <Relative key={child.id} card={child} relation="child" size="m" used={used} />
          ))}
        </div>
      ) : (
        <p class="family-none">{t("noChildren")}</p>
      )}

      {siblings.length > 0 && (
        <section class="siblings" aria-label={t("siblings")}>
          <h2>{t("siblings")}</h2>
          <div class="siblings-row">
            {siblings.map((sibling) => (
              <Relative key={sibling.id} card={sibling} relation="sibling" size="m" used={used} />
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
