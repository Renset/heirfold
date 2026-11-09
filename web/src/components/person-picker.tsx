import { useMemo, useState } from "preact/hooks";
import type { Card } from "../api/types";
import { locale, t } from "../i18n";
import { lifeYears, surnameLine } from "../lib/format";
import { searchPeople } from "../lib/graph";
import { graph } from "../state";
import { Avatar, Button, displayName } from "./ui";

/** Returns the name of a person in one line. */
export function cardLine(card: Card): string {
  const name = displayName(card);
  return [surnameLine(card) || name.second, name.main].filter(Boolean).join(" ");
}

/**
 * Chooses one person of the tree. It shows the chosen person, or a search
 * field. An empty value means that no person is chosen.
 */
export function PersonPicker({
  label,
  help,
  value,
  busy,
  locked,
  onChange,
}: {
  label: string;
  help?: string;
  value: string;
  busy?: boolean;
  /** The user sees the choice and cannot change it. */
  locked?: boolean;
  onChange: (id: string, card?: Card) => void;
}) {
  const [query, setQuery] = useState("");
  const index = graph.value;
  const chosen = value ? index?.byId.get(value) : undefined;
  const hits = useMemo(() => (index && query.trim() ? searchPeople(index, query, 6) : []), [index, query]);

  if (value) {
    return (
      <div class="picker">
        <span class="field-label">{label}</span>
        <div class="picker-chosen">
          {chosen && <Avatar card={chosen} size="s" />}
          <span class="row-text">
            <span>{chosen ? cardLine(chosen) : t("pickerHidden")}</span>
            {chosen && <small>{lifeYears(chosen, locale.value)}</small>}
          </span>
          {!locked && (
            <Button kind="plain" busy={busy} onClick={() => onChange("")}>
              {t("pickerClear")}
            </Button>
          )}
        </div>
        {help && <p class="field-help">{help}</p>}
      </div>
    );
  }
  return (
    <div class="picker">
      <label class="field">
        <span class="field-label">{label}</span>
        <input
          type="text"
          value={query}
          autocomplete="off"
          placeholder={t("pickerPlaceholder")}
          disabled={busy}
          onInput={(event) => setQuery(event.currentTarget.value)}
        />
      </label>
      {hits.length > 0 && (
        <div class="picker-hits">
          {hits.map((card) => (
            <button
              type="button"
              class="row link"
              key={card.id}
              disabled={busy}
              onClick={() => {
                setQuery("");
                onChange(card.id, card);
              }}
            >
              <Avatar card={card} size="s" />
              <span class="row-text">
                <span>{cardLine(card)}</span>
              </span>
              <span class="row-value">{lifeYears(card, locale.value)}</span>
            </button>
          ))}
        </div>
      )}
      {query.trim() && hits.length === 0 && <p class="row-empty">{t("noResults")}</p>}
      {help && <p class="field-help">{help}</p>}
    </div>
  );
}
