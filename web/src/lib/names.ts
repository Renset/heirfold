import type { Card } from "../api/types";
import { t } from "../i18n";
import type { GraphIndex } from "./graph";

/** The words for a type of a name of Gramps. An unknown type stays as it is. */
export function nameTypeLabel(type: string): string {
  switch (type) {
    case "Birth Name":
      return t("nameTypeBirth");
    case "Married Name":
      return t("nameTypeMarried");
    case "Also Known As":
      return t("nameTypeAka");
    default:
      return type;
  }
}

/** The form of a surname for a woman: Иванов → Иванова, Вишневский → Вишневская. Other surnames stay as they are. */
export function feminine(surname: string): string {
  const value = surname.trim();
  const lower = value.toLocaleLowerCase("ru");
  for (const [man, woman] of [
    ["ский", "ская"],
    ["цкий", "цкая"],
    ["ой", "ая"],
    ["ый", "ая"],
  ] as const) {
    if (lower.endsWith(man)) {
      return value.slice(0, -man.length) + woman;
    }
  }
  return /(ов|ев|ёв|ин|ын)$/i.test(value) ? `${value}а` : value;
}

/** Reports whether the surname of a woman is the surname of a man, in the form for a woman or as it is. */
export function sameSurname(man: string | undefined, woman: string | undefined): boolean {
  const a = man?.trim().toLocaleLowerCase("ru") ?? "";
  const b = woman?.trim().toLocaleLowerCase("ru") ?? "";
  return Boolean(a && b) && (a === b || feminine(a).toLocaleLowerCase("ru") === b);
}

/** What the tree knows about the surname of a woman: the surnames of her father and of her husbands. */
export interface FamilySurnames {
  father?: string;
  husbands: string[];
}

/**
 * Reports whether a single surname of a woman looks like a surname from a
 * marriage that the tree keeps as the surname at birth: it is the surname of a
 * husband, and it is not the surname of her father.
 */
export function looksMarried(surname: string, family: FamilySurnames): boolean {
  if (!surname.trim() || !family.husbands.some((husband) => sameSurname(husband, surname))) {
    return false;
  }
  return !(family.father && sameSurname(family.father, surname));
}

/** The surnames of the father and of the husbands of a person, from the tree. */
export function familySurnames(index: GraphIndex, id: string): FamilySurnames {
  const father = index.parentFamilies.get(id)?.find((family) => family.father)?.father;
  const husbands = (index.ownFamilies.get(id) ?? [])
    .map((family) => (family.mother === id ? family.father : undefined))
    .map((husband) => (husband ? index.byId.get(husband)?.surname : undefined))
    .filter((surname): surname is string => Boolean(surname));
  return { father: father ? index.byId.get(father)?.surname : undefined, husbands };
}

/**
 * The women whose only surname looks like a surname from a marriage: the tree
 * keeps no surname at birth and no name from a marriage for them, and the
 * surname is the surname of a husband and not of the father.
 */
export function doubtfulSurnames(index: GraphIndex): Card[] {
  return index.graph.people.filter(
    (card) =>
      card.gender === "F" &&
      !card.married &&
      !card.birthSurname &&
      looksMarried(card.surname ?? "", familySurnames(index, card.id)),
  );
}
