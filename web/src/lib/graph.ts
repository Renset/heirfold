import type { Card, Graph, GraphFamily } from "../api/types";

/** Fast lookups over the family graph. */
export interface GraphIndex {
  graph: Graph;
  byId: Map<string, Card>;
  /** Families in which a person is a child. */
  parentFamilies: Map<string, GraphFamily[]>;
  /** Families in which a person is a parent. */
  ownFamilies: Map<string, GraphFamily[]>;
}

function push<K, V>(map: Map<K, V[]>, key: K, value: V): void {
  const list = map.get(key);
  if (list) {
    list.push(value);
  } else {
    map.set(key, [value]);
  }
}

export function indexGraph(graph: Graph): GraphIndex {
  const byId = new Map(graph.people.map((card) => [card.id, card]));
  const parentFamilies = new Map<string, GraphFamily[]>();
  const ownFamilies = new Map<string, GraphFamily[]>();
  for (const family of graph.families) {
    for (const child of family.children ?? []) {
      push(parentFamilies, child, family);
    }
    if (family.father) {
      push(ownFamilies, family.father, family);
    }
    if (family.mother && family.mother !== family.father) {
      push(ownFamilies, family.mother, family);
    }
  }
  return { graph, byId, parentFamilies, ownFamilies };
}

export function parentsOf(index: GraphIndex, id: string): { father?: Card; mother?: Card } {
  const family = index.parentFamilies.get(id)?.[0];
  if (!family) {
    return {};
  }
  return {
    father: family.father ? index.byId.get(family.father) : undefined,
    mother: family.mother ? index.byId.get(family.mother) : undefined,
  };
}

export function spouseIn(family: GraphFamily, id: string): string | undefined {
  return family.father === id ? family.mother : family.father;
}

export interface Unit {
  family: GraphFamily;
  spouse?: Card;
  children: Card[];
}

export function unitsOf(index: GraphIndex, id: string): Unit[] {
  return (index.ownFamilies.get(id) ?? []).map((family) => {
    const spouse = spouseIn(family, id);
    return {
      family,
      spouse: spouse ? index.byId.get(spouse) : undefined,
      children: (family.children ?? [])
        .map((child) => index.byId.get(child))
        .filter((card): card is Card => card !== undefined),
    };
  });
}

export function siblingsOf(index: GraphIndex, id: string): Card[] {
  const seen = new Set([id]);
  const out: Card[] = [];
  for (const family of index.parentFamilies.get(id) ?? []) {
    for (const child of family.children ?? []) {
      if (seen.has(child)) {
        continue;
      }
      seen.add(child);
      const card = index.byId.get(child);
      if (card) {
        out.push(card);
      }
    }
  }
  return out;
}

/** The size of the known family around a person, used to pick a good start. */
function relatedCount(index: GraphIndex, id: string): number {
  const parents = parentsOf(index, id);
  let count = (parents.father ? 1 : 0) + (parents.mother ? 1 : 0) + siblingsOf(index, id).length;
  for (const unit of unitsOf(index, id)) {
    count += (unit.spouse ? 1 : 0) + unit.children.length;
  }
  return count;
}

/**
 * Chooses the person who opens the tree: the person of the viewer, then the
 * home person, if the viewer can see them. Otherwise it is the visible person
 * with the most relatives.
 */
export function startPerson(index: GraphIndex, own?: string): string | undefined {
  if (own && index.byId.has(own)) {
    return own;
  }
  const home = index.graph.home;
  if (home && index.byId.has(home)) {
    return home;
  }
  let best: string | undefined;
  let bestCount = -1;
  for (const card of index.graph.people) {
    const count = relatedCount(index, card.id);
    if (count > bestCount) {
      best = card.id;
      bestCount = count;
    }
  }
  return best;
}

/** Lowercases text and folds letters that people type interchangeably. */
export function normalize(text: string): string {
  return text.toLocaleLowerCase("ru").replaceAll("ё", "е").normalize("NFD").replace(/\p{M}/gu, "");
}

export interface SearchHit {
  card: Card;
  score: number;
}

/** Finds people by words of a name. Every word of the query must match. */
export function searchPeople(index: GraphIndex, query: string, limit = 50): Card[] {
  const words = normalize(query).split(/\s+/).filter(Boolean);
  if (words.length === 0) {
    return [];
  }
  const hits: SearchHit[] = [];
  for (const card of index.graph.people) {
    // A woman is found by the surname from the marriage and by the surname at birth.
    const parts = [card.surname, card.birthSurname, card.given, card.patronymic]
      .filter((part): part is string => Boolean(part))
      .map(normalize);
    let score = 0;
    let matched = true;
    for (const word of words) {
      if (parts.some((part) => part.startsWith(word))) {
        score += 2;
      } else if (parts.some((part) => part.includes(word))) {
        score += 1;
      } else {
        matched = false;
        break;
      }
    }
    if (matched) {
      hits.push({ card, score });
    }
  }
  hits.sort((a, b) => b.score - a.score);
  return hits.slice(0, limit).map((hit) => hit.card);
}

/** The part of the tree that a family member with the own branch can change. */
export interface Branch {
  /** The person and the descendants. */
  line: Set<string>;
  /** The line and the partners of its people. */
  people: Set<string>;
}

/** Returns the branch of a person: the person, the descendants, and the partners of all of them. */
export function branchOf(index: GraphIndex, root: string): Branch {
  const line = new Set<string>();
  const people = new Set<string>();
  if (!index.byId.has(root)) {
    return { line, people };
  }
  const queue = [root];
  line.add(root);
  for (let at = 0; at < queue.length; at++) {
    const person = queue[at] as string;
    people.add(person);
    for (const family of index.ownFamilies.get(person) ?? []) {
      const partner = spouseIn(family, person);
      if (partner && index.byId.has(partner)) {
        people.add(partner);
      }
      for (const child of family.children ?? []) {
        if (!line.has(child) && index.byId.has(child)) {
          line.add(child);
          queue.push(child);
        }
      }
    }
  }
  return { line, people };
}
