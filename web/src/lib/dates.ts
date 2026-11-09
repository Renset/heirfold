import type { Dated, DatePair, DateVal } from "../api/types";

/** The forms of a date that the editor offers. */
export type DateKind = "none" | "exact" | "about" | "before" | "after" | "between" | "span" | "text";

/** One calendar point as a person types it. An empty field means "not known". */
export interface PointDraft {
  day: string;
  month: string;
  year: string;
}

/** A date while a person edits it. */
export interface DateDraft {
  kind: DateKind;
  first: PointDraft;
  second: PointDraft;
  text: string;
  /** The parts that the form does not show: calendar, quality and so on. */
  keep: Partial<DateVal & DatePair>;
}

const emptyPoint = (): PointDraft => ({ day: "", month: "", year: "" });

function parsePoint(text: string): PointDraft {
  const [year = "", month = "", day = ""] = text.split("-");
  return {
    year: year === "????" ? "" : String(Number(year) || ""),
    month: Number(month) ? String(Number(month)) : "",
    day: Number(day) ? String(Number(day)) : "",
  };
}

/** Returns the stored text of a point, or null if the fields make no date. */
function formatPoint(point: PointDraft): string | null {
  const year = Number(point.year);
  const month = Number(point.month);
  const day = Number(point.day);
  const hasYear = point.year.trim() !== "";
  if (hasYear && !(Number.isInteger(year) && year >= 1 && year <= 9999)) {
    return null;
  }
  if (point.month.trim() !== "" && !(Number.isInteger(month) && month >= 1 && month <= 12)) {
    return null;
  }
  if (point.day.trim() !== "" && !(Number.isInteger(day) && day >= 1 && day <= 31)) {
    return null;
  }
  if (day && !month) {
    return null;
  }
  if (!hasYear && !month) {
    return null;
  }
  const pad = (value: number, size: number): string => String(value).padStart(size, "0");
  let out = hasYear ? pad(year, 4) : "????";
  if (month) {
    out += `-${pad(month, 2)}`;
    if (day) {
      out += `-${pad(day, 2)}`;
    }
  }
  return out;
}

export function draftFromDate(date: Dated | undefined): DateDraft {
  const draft: DateDraft = { kind: "none", first: emptyPoint(), second: emptyPoint(), text: "", keep: {} };
  if (date?.dateval) {
    const { val, type, ...keep } = date.dateval;
    draft.kind = type === "about" || type === "before" || type === "after" ? type : "exact";
    draft.first = parsePoint(val);
    draft.keep = keep;
  } else if (date?.daterange || date?.datespan) {
    const pair = (date.daterange ?? date.datespan) as DatePair;
    const { start, stop, ...keep } = pair;
    draft.kind = date.daterange ? "between" : "span";
    draft.first = parsePoint(start);
    draft.second = parsePoint(stop);
    draft.keep = keep;
  } else if (date?.datestr) {
    draft.kind = "text";
    draft.text = date.datestr.val;
  }
  return draft;
}

/** Returns the date to store, or null if the fields are not a valid date. */
export function dateFromDraft(draft: DateDraft): Dated | null {
  switch (draft.kind) {
    case "none":
      return {};
    case "text":
      return draft.text.trim() ? { datestr: { val: draft.text.trim() } } : {};
    case "between":
    case "span": {
      const start = formatPoint(draft.first);
      const stop = formatPoint(draft.second);
      if (!start || !stop) {
        return null;
      }
      const pair: DatePair = { ...draft.keep, start, stop };
      return draft.kind === "between" ? { daterange: pair } : { datespan: pair };
    }
    default: {
      const val = formatPoint(draft.first);
      if (!val) {
        return null;
      }
      const value: DateVal = { ...draft.keep, val };
      if (draft.kind !== "exact") {
        value.type = draft.kind;
      }
      return { dateval: value };
    }
  }
}

/** A short text of a stored date for a list, in the order day, month, year. */
export function dateSummary(date: Dated, words: Record<DateKind, string>): string {
  const draft = draftFromDate(date);
  const point = (p: PointDraft): string => [p.day, p.month, p.year].filter(Boolean).join(".");
  switch (draft.kind) {
    case "none":
      return "";
    case "text":
      return draft.text;
    case "between":
    case "span":
      return `${point(draft.first)} – ${point(draft.second)}`;
    case "exact":
      return point(draft.first);
    default:
      return `${words[draft.kind]} ${point(draft.first)}`.trim();
  }
}
