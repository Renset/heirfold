import type { Card, DatePoint, DateView } from "../api/types";

export type Locale = "ru" | "en";

const calendarNotes: Record<Locale, Record<string, string>> = {
  ru: {
    julian: "ст. ст.",
    hebrew: "евр. календарь",
    islamic: "хиджра",
    persian: "перс. календарь",
    "french republican": "респ. календарь",
    swedish: "швед. календарь",
  },
  en: {
    julian: "O.S.",
    hebrew: "Hebrew",
    islamic: "Hijri",
    persian: "Persian",
    "french republican": "Republican",
    swedish: "Swedish",
  },
};

const words: Record<Locale, Record<string, string>> = {
  ru: {
    about: "около",
    before: "до",
    after: "после",
    between: "между",
    and: "и",
    from: "с",
    to: "по",
    estimated: "оценка",
    calculated: "расчёт",
  },
  en: {
    about: "about",
    before: "before",
    after: "after",
    between: "between",
    and: "and",
    from: "from",
    to: "to",
    estimated: "estimated",
    calculated: "calculated",
  },
};

function formatPoint(point: DatePoint, locale: Locale): string {
  if (point.y === 0) {
    return "?";
  }
  if (!point.m) {
    return String(point.y);
  }
  const date = new Date(0);
  date.setUTCFullYear(point.y, point.m - 1, point.d || 1);
  if (!point.d) {
    return new Intl.DateTimeFormat(locale, { month: "long", year: "numeric", timeZone: "UTC" }).format(date);
  }
  return new Intl.DateTimeFormat(locale, {
    day: "numeric",
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  }).format(date);
}

/** Writes a date in full, for example "about 14 May 1901 (O.S.)". */
export function formatDate(view: DateView | undefined, locale: Locale): string {
  if (!view) {
    return "";
  }
  const dictionary = words[locale];
  if (view.form === "text") {
    return view.text ?? "";
  }
  const start = view.start ? formatPoint(view.start, locale) : "?";
  const stop = view.stop ? formatPoint(view.stop, locale) : "?";
  let text: string;
  switch (view.form) {
    case "about":
      text = `${dictionary.about} ${start}`;
      break;
    case "before":
      text = `${dictionary.before} ${start}`;
      break;
    case "after":
      text = `${dictionary.after} ${start}`;
      break;
    case "range":
      text = `${dictionary.between} ${start} ${dictionary.and} ${stop}`;
      break;
    case "span":
      text = `${dictionary.from} ${start} ${dictionary.to} ${stop}`;
      break;
    default:
      text = start;
  }
  const notes: string[] = [];
  const calendar = view.calendar ? calendarNotes[locale][view.calendar.toLowerCase()] : undefined;
  if (calendar) {
    notes.push(calendar);
  }
  if (view.quality && dictionary[view.quality]) {
    notes.push(dictionary[view.quality] as string);
  }
  return notes.length > 0 ? `${text} (${notes.join(", ")})` : text;
}

function shortYear(view: DateView | undefined, locale: Locale): string {
  if (!view) {
    return "";
  }
  if (view.form === "text") {
    return "";
  }
  const point = view.start?.y ? view.start : view.stop;
  if (!point?.y) {
    return "";
  }
  if (view.form === "about") {
    return locale === "ru" ? `ок. ${point.y}` : `c. ${point.y}`;
  }
  if (view.form === "before") {
    return locale === "ru" ? `до ${point.y}` : `bef. ${point.y}`;
  }
  if (view.form === "after") {
    return locale === "ru" ? `после ${point.y}` : `aft. ${point.y}`;
  }
  return String(point.y);
}

/** Writes the years of life, for example "1901-1975" or "born 1990". */
export function lifeYears(card: Card, locale: Locale): string {
  const birth = shortYear(card.birth, locale);
  const death = shortYear(card.death, locale);
  if (birth && death) {
    return `${birth}-${death}`;
  }
  if (birth) {
    if (card.living) {
      return locale === "ru" ? `род. ${birth}` : `born ${birth}`;
    }
    return `${birth}-?`;
  }
  if (death) {
    return locale === "ru" ? `ум. ${death}` : `died ${death}`;
  }
  return "";
}

/** Splits a name into a main line and a secondary line for a card. */
export function nameLines(card: Card, locale: Locale): { main: string; second: string } {
  const given = card.given ?? "";
  const surname = card.surname ?? "";
  const patronymic = card.patronymic ?? "";
  if (locale === "ru") {
    return { main: [given, patronymic].filter(Boolean).join(" "), second: surname };
  }
  return { main: [given, patronymic].filter(Boolean).join(" "), second: surname };
}

/**
 * Says the surname at birth when it differs from the surname that the card
 * shows: «урожд. Сидорова», «née Sidorova». Empty when there is none.
 */
export function birthNote(card: Card, locale: Locale): string {
  const birth = card.birthSurname?.trim();
  if (!birth) {
    return "";
  }
  if (locale === "ru") {
    return `урожд. ${birth}`;
  }
  const word = card.gender === "F" ? "née" : card.gender === "M" ? "né" : "born";
  return `${word} ${birth}`;
}

/** The surname with the surname at birth in brackets, for a place with little room: «Иванова (Сидорова)». */
export function surnameLine(card: Card): string {
  const surname = card.surname?.trim() ?? "";
  const birth = card.birthSurname?.trim() ?? "";
  if (!birth) {
    return surname;
  }
  return surname ? `${surname} (${birth})` : birth;
}

/** Writes the full name in the order that the language expects, with the surname at birth at the end. */
export function fullName(card: Card, locale: Locale): string {
  const parts =
    locale === "ru"
      ? [card.surname, card.given, card.patronymic]
      : [card.given, card.patronymic, card.surname];
  const name = parts.filter(Boolean).join(" ");
  const note = birthNote(card, locale);
  return note && name ? `${name} (${note})` : name || card.birthSurname || "";
}

/** Returns one or two initials for a person without a photo. */
export function initials(card: Card): string {
  const first = [...(card.given ?? "")][0] ?? "";
  const second = [...(card.surname ?? "")][0] ?? "";
  return (first + second).toLocaleUpperCase() || "?";
}

/** Orders people by surname, then by given name. */
export function byName(locale: Locale): (a: Card, b: Card) => number {
  const collator = new Intl.Collator(locale, { sensitivity: "base" });
  return (a, b) =>
    collator.compare(a.surname ?? "", b.surname ?? "") || collator.compare(a.given ?? "", b.given ?? "");
}
