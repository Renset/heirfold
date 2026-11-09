# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Preact and TypeScript in the browser, built with esbuild. A Go server embeds the frontend, so the product runs as one binary. The project chose Preact after a measured comparison with Vue 3 and native Web Components. Reasons: the browser can block every assignment of an HTML string (Trusted Types with no policy), and the dependency tree is small. See `docs/adr/0002-frontend.md`.

## Users

- **Relatives.** Not technical, and often on a phone. They get an invitation by e-mail. They want to see who is who in the family. They do not know genealogy terms.
- **The keeper of the tree.** One family member who researches, keeps the tree in Gramps Desktop, shows it in Heirfold or moves it there, invites relatives and gives them roles.
- **Self-hosters.** People who install Heirfold for their own family.

## Product Purpose

Heirfold is a private, self-hosted website for one family tree. A relative signs in and reads the tree without any training. The keeper keeps full control of the data. Success means that a relative finds a person and understands how that person is related to others, on a phone, within the first minute.

## Positioning

Heirfold reads a Gramps tree, hides living people by default, and runs as one binary on a server that the family controls. The tree is the first screen. It is a replacement for Gramps Web that aims for feature parity, a much smaller attack surface, and security checks in CI that anyone can inspect.

## Operating Context

- A relative receives a letter, types the code from a second letter, and lands on the tree. The device stays signed in for a year.
- The tree is centred on one person. Search is always at hand.
- The keeper chooses the main copy. Either the keeper works in Gramps Desktop and Heirfold shows the tree, or the tree lives in Heirfold and tree editors change it in the browser.
- Some families have relatives in Russia, where Cloudflare is throttled, so a family's own server must be a complete option.
- A public demo exists for people who evaluate the project. It holds invented people only.

## Capabilities and Constraints

- Import and lossless export of Gramps XML. `.gpkg` and GEDCOM are planned.
- Four roles: guest, family member, tree editor, administrator. Guests see no living people and no private records.
- Editing in the browser with a journal and undo. Heirfold never writes to a Gramps database.
- Every screen works in Russian and English.
- Strict Content Security Policy: no inline code, no third-party origins, no HTML strings.
- No file loads from a CDN. Fonts ship with the binary.
- Real family data never enters the repository, issues, screenshots, CI logs or the demo.
- The app draws its map from files inside the program. A street map through the server is a choice of the operator (`--map-tiles`).
- Undecided: 2FA or passkeys, offline mode.

## Brand Commitments

The name is Heirfold. [DESIGN.md](DESIGN.md) holds the colours and the shapes. The tone is plain and direct. It never uses marketing words, and it avoids genealogy jargon on screens for relatives.

## Evidence on Hand

- The fixture generator (`internal/fixture`) produces invented families in Russian and English.
- Real people, photos, testimonials and usage numbers do not exist in this project and must not be invented.

## Product Principles

1. A relative never needs training. If a screen needs an explanation, the screen is wrong.
2. Hidden is the default. A mistake must hide a dead person, never reveal a living one.
3. The family owns its data. Every feature keeps a lossless way out.
4. Every security claim has a check that runs in CI.
5. Honest status. The product says what it does not do yet.

## Accessibility & Inclusion

Older relatives are a main audience. Text must stay readable at large sizes. Touch targets must be large. The tree must work with touch, with a keyboard and with a screen reader, which reads the list of relatives in place of the chart. WCAG 2.2 AA is the target.
