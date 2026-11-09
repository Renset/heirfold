import { signal } from "@preact/signals";

// Routes live in the URL fragment. A fragment never reaches the server, so
// person IDs stay out of server and proxy logs.

export interface Route {
  path: string[];
  query: URLSearchParams;
}

export function parseRoute(hash: string): Route {
  const raw = hash.replace(/^#\/?/, "");
  const [path = "", query = ""] = raw.split("?", 2);
  return { path: path.split("/").filter(Boolean).map(decodeURIComponent), query: new URLSearchParams(query) };
}

export const route = signal<Route>(parseRoute(typeof location === "undefined" ? "" : location.hash));

export function startRouter(): void {
  window.addEventListener("hashchange", () => {
    route.value = parseRoute(location.hash);
    window.scrollTo(0, 0);
  });
}

export function link(...segments: string[]): string {
  return `#/${segments.map(encodeURIComponent).join("/")}`;
}

export function go(href: string): void {
  location.hash = href;
}
