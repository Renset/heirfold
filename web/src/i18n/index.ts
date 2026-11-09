import { signal } from "@preact/signals";
import type { Dictionary, Plural } from "./en";
import { en } from "./en";
import { ru } from "./ru";

export type Locale = "ru" | "en";

const dictionaries: Record<Locale, Dictionary> = { ru, en };

type PluralKey = {
  [K in keyof Dictionary]: Dictionary[K] extends Plural ? K : never;
}[keyof Dictionary];
export type TextKey = Exclude<keyof Dictionary, PluralKey>;

const storageKey = "heirfold.locale";

function readStored(): string | null {
  try {
    return localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

export function detectLocale(stored: string | null, languages: readonly string[]): Locale {
  if (stored === "ru" || stored === "en") {
    return stored;
  }
  for (const language of languages) {
    const code = language.toLowerCase().split("-")[0];
    if (code === "ru" || code === "en") {
      return code;
    }
  }
  return "en";
}

const initial: Locale =
  typeof navigator === "undefined" ? "en" : detectLocale(readStored(), navigator.languages ?? []);

export const locale = signal<Locale>(initial);

export function setLocale(next: Locale): void {
  locale.value = next;
  try {
    localStorage.setItem(storageKey, next);
  } catch {
    // Private windows can refuse storage. The choice lasts for this page.
  }
  document.documentElement.lang = next;
  document.title = t("appName");
}

function fill(template: string, params?: Record<string, string | number>): string {
  if (!params) {
    return template;
  }
  return template.replace(/\{(\w+)\}/g, (match, name: string) => {
    const value = params[name];
    return value === undefined ? match : String(value);
  });
}

export function t(key: TextKey, params?: Record<string, string | number>): string {
  return fill(dictionaries[locale.value][key] as string, params);
}

export function plural(key: PluralKey, count: number): string {
  const forms = dictionaries[locale.value][key] as Plural;
  const category = new Intl.PluralRules(locale.value).select(count);
  return fill((category === "other" ? forms.other : forms[category]) ?? forms.other, { count });
}

const eventLabels: Record<string, string> = {
  birth: "Рождение",
  death: "Смерть",
  baptism: "Крещение",
  christening: "Крещение",
  burial: "Погребение",
  cremation: "Кремация",
  marriage: "Свадьба",
  engagement: "Помолвка",
  divorce: "Развод",
  occupation: "Занятие",
  residence: "Место жительства",
  census: "Перепись",
  education: "Образование",
  graduation: "Окончание учёбы",
  immigration: "Иммиграция",
  emigration: "Эмиграция",
  naturalization: "Натурализация",
  "military service": "Военная служба",
  retirement: "Выход на пенсию",
  religion: "Вероисповедание",
  ordination: "Рукоположение",
  "nobility title": "Титул",
  property: "Имущество",
  will: "Завещание",
  probate: "Утверждение завещания",
  adopted: "Усыновление",
  "cause of death": "Причина смерти",
  "medical information": "Здоровье",
  nationality: "Национальность",
};

const relationLabels: Record<string, string> = {
  married: "Брак",
  unmarried: "Без брака",
  "civil union": "Гражданский союз",
};

/** Returns the translated label of a standard Gramps event type. */
export function eventLabel(type: string): string {
  if (locale.value !== "ru") {
    return type;
  }
  return eventLabels[type.trim().toLowerCase()] ?? type;
}

export function relationLabel(rel: string): string {
  if (rel.trim().toLowerCase() === "unknown") {
    return "";
  }
  if (locale.value !== "ru") {
    return rel;
  }
  return relationLabels[rel.trim().toLowerCase()] ?? rel;
}
