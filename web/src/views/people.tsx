import { useMemo, useState } from "preact/hooks";
import type { Card } from "../api/types";
import { Icon } from "../components/icons";
import { Avatar, displayName, EmptyState } from "../components/ui";
import { locale, plural, t } from "../i18n";
import { birthNote, byName, fullName, lifeYears } from "../lib/format";
import { searchPeople } from "../lib/graph";
import { doubtfulSurnames } from "../lib/names";
import { link } from "../lib/router";
import { graph, me } from "../state";

function groupLetter(card: Card): string {
  const letter = [...(card.surname ?? card.given ?? "")][0];
  return letter ? letter.toLocaleUpperCase() : "#";
}

function Row({ card }: { card: Card }) {
  const name = displayName(card);
  return (
    <a class="row link" href={link("people", card.id)}>
      <Avatar card={card} size="s" />
      <span class="row-text">
        <span>
          <strong>{name.second}</strong> {name.main}
        </span>
        {card.birthSurname && <small>{birthNote(card, locale.value)}</small>}
      </span>
      <span class="row-value">{lifeYears(card, locale.value)}</span>
      <Icon name="caret-right" />
    </a>
  );
}

export function PeopleView() {
  const index = graph.value;
  const [filter, setFilter] = useState("");
  const people = index?.graph.people ?? [];

  const groups = useMemo(() => {
    const sorted = [...people].sort(byName(locale.value));
    const map = new Map<string, Card[]>();
    for (const card of sorted) {
      const letter = groupLetter(card);
      const list = map.get(letter);
      if (list) {
        list.push(card);
      } else {
        map.set(letter, [card]);
      }
    }
    return [...map.entries()];
  }, [people, locale.value]);

  const hits = useMemo(() => (index ? searchPeople(index, filter, 200) : []), [index, filter]);
  const filtering = filter.trim().length > 0;
  // An editor sees the women whose surname may be a surname from a marriage.
  const canFix = Boolean(me.value?.edit) && !__DEMO__;
  const doubtful = useMemo(() => (index && canFix ? doubtfulSurnames(index) : []), [index, canFix]);
  const [checking, setChecking] = useState(false);

  if (people.length === 0) {
    return (
      <div class="center-state">
        <EmptyState icon="users-three" title={t("emptyTree")} />
      </div>
    );
  }

  return (
    <div class="people">
      <header class="page-head">
        <h1>{t("peopleTitle")}</h1>
        <p>{plural("peopleCount", people.length)}</p>
        {me.value?.edit && !__DEMO__ && (
          <a class="button" href={link("people", "new")}>
            <Icon name="plus" />
            {t("personAdd")}
          </a>
        )}
      </header>
      <label class="filter">
        <Icon name="magnifying-glass" />
        <input
          type="search"
          placeholder={t("peopleSearchHint")}
          aria-label={t("searchLabel")}
          value={filter}
          onInput={(event) => setFilter(event.currentTarget.value)}
        />
      </label>
      {doubtful.length > 0 && (
        <div class="people-check">
          <button
            type="button"
            class="filter-toggle"
            aria-pressed={checking}
            onClick={() => setChecking(!checking)}
          >
            {t("surnamesCheck", { count: doubtful.length })}
          </button>
        </div>
      )}
      {checking && doubtful.length > 0 ? (
        <section class="letter">
          <p class="page-lead">{t("surnamesCheckLead")}</p>
          <div class="group-body">
            {[...doubtful].sort(byName(locale.value)).map((card) => (
              <a class="row link" key={card.id} href={link("people", card.id, "edit")}>
                <Avatar card={card} size="s" />
                <span class="row-text">
                  <span>{fullName(card, locale.value)}</span>
                </span>
                <span class="row-value">{lifeYears(card, locale.value)}</span>
                <Icon name="caret-right" />
              </a>
            ))}
          </div>
        </section>
      ) : filtering ? (
        hits.length > 0 ? (
          <div class="group-body">
            {hits.map((card) => (
              <Row key={card.id} card={card} />
            ))}
          </div>
        ) : (
          <EmptyState icon="magnifying-glass" title={t("noResults")} />
        )
      ) : (
        groups.map(([letter, cards]) => (
          <section class="letter" key={letter}>
            <h2>{letter}</h2>
            <div class="group-body">
              {cards.map((card) => (
                <Row key={card.id} card={card} />
              ))}
            </div>
          </section>
        ))
      )}
    </div>
  );
}
