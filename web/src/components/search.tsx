import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Card } from "../api/types";
import { locale, t } from "../i18n";
import { fullName, lifeYears, surnameLine } from "../lib/format";
import { searchPeople } from "../lib/graph";
import { link } from "../lib/router";
import { graph, navigate } from "../state";
import { Icon } from "./icons";
import { Avatar, displayName } from "./ui";

export function SearchBox() {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const box = useRef<HTMLDivElement>(null);

  const hits = useMemo<Card[]>(() => {
    const index = graph.value;
    return index ? searchPeople(index, query, 12) : [];
  }, [query, graph.value]);

  useEffect(() => {
    const close = (event: PointerEvent): void => {
      if (box.current && !box.current.contains(event.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, []);

  const choose = (card: Card): void => {
    setQuery("");
    setOpen(false);
    navigate(link("tree", card.id), true);
  };

  const onKeyDown = (event: KeyboardEvent): void => {
    if (event.key === "Escape") {
      setOpen(false);
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive((value) => Math.min(value + 1, hits.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((value) => Math.max(value - 1, 0));
    } else if (event.key === "Enter") {
      const hit = hits[active];
      if (hit) {
        choose(hit);
      }
    }
  };

  const showing = open && query.trim().length > 0;
  return (
    <div class="search" ref={box}>
      <Icon name="magnifying-glass" />
      <input
        type="search"
        placeholder={t("searchPlaceholder")}
        aria-label={t("searchLabel")}
        autocomplete="off"
        value={query}
        onInput={(event) => {
          setQuery(event.currentTarget.value);
          setActive(0);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={onKeyDown}
      />
      {showing && (
        <ul class="search-results">
          {hits.length === 0 && <li class="search-none">{t("noResults")}</li>}
          {hits.map((card, index) => {
            const name = displayName(card);
            return (
              <li key={card.id}>
                <button
                  type="button"
                  class={index === active ? "search-hit active" : "search-hit"}
                  aria-label={fullName(card, locale.value) || t("unnamed")}
                  onClick={() => choose(card)}
                >
                  <Avatar card={card} size="s" />
                  <span class="search-hit-text">
                    <span>
                      {name.main} {surnameLine(card) || name.second}
                    </span>
                    <small>{lifeYears(card, locale.value)}</small>
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
