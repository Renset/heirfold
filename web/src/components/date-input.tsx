import { useMemo } from "preact/hooks";
import { locale, t } from "../i18n";
import type { DateDraft, DateKind } from "../lib/dates";

/** The words for the kinds of a date. */
export function dateWords(): Record<DateKind, string> {
  return {
    none: t("dateNone"),
    exact: t("dateExact"),
    about: t("dateAbout"),
    before: t("dateBefore"),
    after: t("dateAfter"),
    between: t("dateBetween"),
    span: t("dateSpan"),
    text: t("dateText"),
  };
}

function PointFields({
  point,
  label,
  onChange,
}: {
  point: DateDraft["first"];
  label: string;
  onChange: (next: DateDraft["first"]) => void;
}) {
  const months = useMemo(
    () =>
      Array.from({ length: 12 }, (_, i) =>
        new Intl.DateTimeFormat(locale.value, { month: "long" }).format(new Date(2001, i, 1)),
      ),
    [locale.value],
  );
  return (
    <span class="date-point" role="group" aria-label={label}>
      <input
        class="date-day"
        inputMode="numeric"
        maxLength={2}
        placeholder={t("dateDay")}
        aria-label={t("dateDay")}
        value={point.day}
        onInput={(event) => onChange({ ...point, day: event.currentTarget.value })}
      />
      <select
        class="date-month"
        aria-label={t("dateMonth")}
        value={point.month}
        onChange={(event) => onChange({ ...point, month: event.currentTarget.value })}
      >
        <option value="">{t("dateMonth")}</option>
        {months.map((name, i) => (
          <option key={name} value={String(i + 1)}>
            {name}
          </option>
        ))}
      </select>
      <input
        class="date-year"
        inputMode="numeric"
        maxLength={4}
        placeholder={t("dateYear")}
        aria-label={t("dateYear")}
        value={point.year}
        onInput={(event) => onChange({ ...point, year: event.currentTarget.value })}
      />
    </span>
  );
}

/** Lets a person type a date that is exact, rough, a range, or free text. */
export function DateInput({ draft, onChange }: { draft: DateDraft; onChange: (next: DateDraft) => void }) {
  const words = dateWords();
  const kinds: DateKind[] = ["none", "exact", "about", "before", "after", "between", "span", "text"];
  const pair = draft.kind === "between" || draft.kind === "span";
  return (
    <div class="field">
      <span class="field-label">{t("eventDate")}</span>
      <div class="date-input">
        <select
          aria-label={t("dateKind")}
          value={draft.kind}
          onChange={(event) => onChange({ ...draft, kind: event.currentTarget.value as DateKind })}
        >
          {kinds.map((kind) => (
            <option key={kind} value={kind}>
              {words[kind]}
            </option>
          ))}
        </select>
        {draft.kind !== "none" && draft.kind !== "text" && (
          <PointFields
            point={draft.first}
            label={pair ? t("dateFrom") : t("eventDate")}
            onChange={(first) => onChange({ ...draft, first })}
          />
        )}
        {pair && (
          <PointFields
            point={draft.second}
            label={t("dateTo")}
            onChange={(second) => onChange({ ...draft, second })}
          />
        )}
        {draft.kind === "text" && (
          <input
            aria-label={t("dateText")}
            maxLength={120}
            value={draft.text}
            onInput={(event) => onChange({ ...draft, text: event.currentTarget.value })}
          />
        )}
      </div>
    </div>
  );
}
