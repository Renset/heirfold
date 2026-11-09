import { computed, signal } from "@preact/signals";
import { ApiError, type DataSource, loadBundle, server } from "./api/client";
import type { Me } from "./api/types";
import { type Locale, locale, setLocale } from "./i18n";
import { type Branch, branchOf, type GraphIndex, indexGraph } from "./lib/graph";
import { parseRoute, route } from "./lib/router";

export type LoadState = "loading" | "ready" | "error";
export type DemoRole = "guest" | "member";
export type Theme = "system" | "light" | "dark";

export const me = signal<Me | null>(null);
export const session = signal<"unknown" | "signed-out" | "signed-in" | "needs-setup">("unknown");
export const source = signal<DataSource | null>(null);
export const graph = signal<GraphIndex | null>(null);
export const treeState = signal<LoadState>("loading");
export const demoRole = signal<DemoRole>("member");
/** The server sends no letters, so a sign-in code is in the server log. */
export const codesInLog = signal(false);
/** The number of the tree state that this page shows. It grows when the tree changes. */
export const revision = signal(0);

/**
 * The branch that a family member can change: the person of the account, the
 * descendants and the partners. It is null for everybody else.
 */
export const ownBranch = computed<Branch | null>(() => {
  const account = me.value;
  const index = graph.value;
  if (!account?.editOwn || !account.user.person || !index) {
    return null;
  }
  return branchOf(index, account.user.person);
});

/** Reports whether the user can change this person. The server checks each change again. */
export function canEdit(person: string): boolean {
  return Boolean(me.value?.edit) || (ownBranch.value?.people.has(person) ?? false);
}

/** Reports whether the user can choose the portrait of a person: an editor for everybody, each person for oneself. */
export function canPortrait(person: string): boolean {
  const account = me.value;
  return canEdit(person) || Boolean(account?.pictures && account.user.person === person);
}

/**
 * Makes the page and the account use one language. An account with a
 * language sets the page, so letters and pages agree on every device. An
 * account without one takes the language of the page.
 */
function adoptLanguage(account: Me): void {
  if (account.lang === "ru" || account.lang === "en") {
    if (account.lang !== locale.value) {
      setLocale(account.lang as Locale);
    }
    return;
  }
  void server.updateMe({ lang: locale.value }).catch(() => undefined);
}

const themeKey = "heirfold.theme";

function readTheme(): Theme {
  try {
    const stored = localStorage.getItem(themeKey);
    return stored === "light" || stored === "dark" ? stored : "system";
  } catch {
    return "system";
  }
}

export const theme = signal<Theme>(typeof document === "undefined" ? "system" : readTheme());

export function applyTheme(next: Theme): void {
  theme.value = next;
  if (next === "system") {
    delete document.documentElement.dataset.theme;
  } else {
    document.documentElement.dataset.theme = next;
  }
  try {
    localStorage.setItem(themeKey, next);
  } catch {
    // Storage can be blocked. The theme then lasts for this page only.
  }
}

async function loadTree(from: DataSource): Promise<void> {
  treeState.value = "loading";
  try {
    graph.value = indexGraph(await from.graph());
    source.value = from;
    treeState.value = "ready";
  } catch {
    treeState.value = "error";
  }
}

/** How often the page asks the server whether the tree changed. */
const WATCH_MS = 10_000;
let watching = false;

async function checkTree(): Promise<void> {
  if (document.hidden || session.value !== "signed-in" || treeState.value !== "ready") {
    return;
  }
  try {
    const state = await server.sync();
    if (state.revision !== revision.value) {
      // Swap the tree in place. The reader keeps the current screen.
      graph.value = indexGraph(await server.graph());
      revision.value = state.revision;
    }
  } catch {
    // The next check tries again.
  }
}

/** Follows the tree of the server, so a change in Gramps appears without a reload. */
async function watchTree(): Promise<void> {
  try {
    revision.value = (await server.sync()).revision;
  } catch {
    // An old server has no revision. The page then keeps what it loaded.
    return;
  }
  if (watching) {
    return;
  }
  watching = true;
  setInterval(() => void checkTree(), WATCH_MS);
  document.addEventListener("visibilitychange", () => void checkTree());
}

/** Shows the tree after an edit of this page, without a wait for the next check. */
export async function treeEdited(next: number): Promise<void> {
  graph.value = indexGraph(await server.graph());
  revision.value = next;
}

export function reloadTree(): Promise<void> {
  return __DEMO__ ? loadDemo() : loadTree(server);
}

/** Loads the demo bundle for the current language and viewer. */
export async function loadDemo(): Promise<void> {
  treeState.value = "loading";
  try {
    await loadTree(await loadBundle(`snapshot/${locale.value}-${demoRole.value}.json`));
  } catch {
    treeState.value = "error";
  }
}

export async function bootServer(): Promise<void> {
  try {
    const status = await server.status();
    codesInLog.value = Boolean(status.codesInLog);
    if (status.needsSetup) {
      session.value = "needs-setup";
      return;
    }
    me.value = await server.me();
    adoptLanguage(me.value);
    session.value = "signed-in";
    await loadTree(server);
    void watchTree();
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      session.value = "signed-out";
      return;
    }
    session.value = "signed-out";
    treeState.value = "error";
  }
}

export async function afterSignIn(account: Me): Promise<void> {
  me.value = account;
  adoptLanguage(account);
  session.value = "signed-in";
  await loadTree(server);
  void watchTree();
}

export async function signOut(): Promise<void> {
  try {
    await server.logout();
  } finally {
    me.value = null;
    graph.value = null;
    source.value = null;
    session.value = "signed-out";
  }
}

function nextTask(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

/** Changes the route. With `morph`, the browser animates shared portraits. */
export function navigate(href: string, morph = false): void {
  const apply = (): void => {
    history.pushState(null, "", href);
    route.value = parseRoute(href);
  };
  const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (morph && document.startViewTransition && !reduced) {
    document.startViewTransition(async () => {
      apply();
      await nextTask();
    });
    return;
  }
  apply();
  window.scrollTo(0, 0);
}
