# ADR 0002: Build the frontend with Preact and TypeScript

Status: accepted, 2026-11-09

## Context

The frontend holds family data and needs:

- a strict Content Security Policy with no inline code and no third-party origin;
- a very small dependency tree;
- enough structure to reach feature parity with Gramps Web;
- a good experience on phones.

Trusted Types reached Baseline status in February 2026. With `require-trusted-types-for 'script'` and `trusted-types 'none'`, the browser blocks every assignment of an HTML string to the DOM. That closes the main XSS path at the browser level.

## Measurements

The numbers come from one minimal app (a button with reactive state), bundled and minified with esbuild, then compressed with `gzip -9`. Package counts come from `npm install --package-lock-only` and exclude platform binaries.

| Option | App size, gzip | Runtime packages | Packages with the build tools | Inserts templates through | Trusted Types |
| --- | --- | --- | --- | --- | --- |
| Preact 11.1 + signals | 8.2 KB | 3 | 6 | DOM API | No policy needed |
| Vue 3.5 | 22.4 KB | 23 | 90 (with Vite, `vue-tsc`) | `innerHTML` for static blocks | Own policy `vue`, which does not sanitize |
| Svelte 5 | not measured | not measured | not measured | `innerHTML` | Own policy `svelte-trusted-html` |
| Lit 3 (used by Gramps Web) | not measured | not measured | not measured | `innerHTML` | Own policy `lit-html` |
| Solid 1 | not measured | not measured | not measured | `innerHTML` | No policy |
| Native Web Components | 0 KB | 0 | build tools only | Manual | No policy needed |
| Astro 7.4 | not measured | 185 | 185 | Inline island loader; CSP needs hashes | Still needs a UI library |

Source inspection on 2026-11-09 confirmed the columns on template insertion and Trusted Types for Preact, Vue, Svelte, Lit and Solid. In Preact, the only assignment to `innerHTML` is the `dangerouslySetInnerHTML` branch.

## Decision

Use Preact with `@preact/signals` and TypeScript. Build with esbuild. Lint with Biome.

## Rules

- `dangerouslySetInnerHTML` is forbidden. The `npm run check` script fails if the source contains it, or any other HTML sink.
- The CSP stays at `script-src 'self'; style-src 'self'; trusted-types 'none'`.
- Styles are plain CSS files. Dynamic positions use SVG attributes or the CSSOM.
- Fonts and all other assets ship inside the binary.
- Routing uses the URL fragment. The server then never receives a person ID in the address of a page.

## Consequences

- There are fewer ready-made components than for React or Vue, so Heirfold builds its own small set.
- Preact 11 is a young major version. Heirfold pins exact versions, and Dependabot applies a 7-day cooldown.
- Astro stays an option for a separate documentation site. It does not suit the application, because almost every screen is interactive and behind a login.

## Evaluation of Astryx, 2026-11-09

[Astryx](https://github.com/meta/astryx), the open design system from Meta, was a candidate for a more modern look. Astryx has the MIT license and is in beta. The test used `@astryxdesign/core` 0.7.11 with nine components and four themes, bundled and minified with esbuild.

| Measure | Astryx on React 19 | Astryx on Preact compat | Heirfold with its own components |
| --- | --- | --- | --- |
| JavaScript | 762 KB | 550 KB | 116 KB |
| JavaScript, gzip | 231 KB | 169 KB | 36 KB |
| CSS | 353 KB | 353 KB | 25 KB |
| CSS, gzip | 49 KB | 49 KB | 6 KB |
| Runtime packages | 16 | not counted | 3 |

The Astryx columns cover only the nine test components. The Heirfold column covers the whole app.

Findings:

- Astryx needs React 19. It rendered the same on Preact compat with a shim for `useOptimistic`, but Meta does not support that setup.
- Trusted Types reported no violations.
- The CSP reported one violation. Astryx injects a 35 KB `<style>` element at run time. A `<link>` element with the same ID can prevent the injection, but that relies on an internal detail.
- Astryx has no tree or chart component, so the family views stay custom in any case.

Decision: keep Preact and the current CSP. Take the visual language of Astryx and do not take the library. That language is the colour values, the flat outlined controls and the underlined view tabs. Onest replaces the system font and ships inside the binary. A first visit loads two font files at most, about 50 KB in total.

Check this decision again if Astryx gets a stable release with official Preact support and no injected styles, or if Heirfold needs complex widgets such as data tables or date pickers.
