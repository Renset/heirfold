# ADR 0001: Write the backend in Go

Status: accepted, 2026-11-09

## Context

Heirfold needs a server that:

- runs as one binary on a home computer or on a small server;
- has security checks in GitHub Actions that anyone can inspect;
- parses untrusted files and stores private data;
- can be maintained by a small open-source community.

The two candidates were Go and Rust. Cloudflare Workers run Rust through `workers-rs` and WebAssembly. Go does not run on Workers natively.

Some families have relatives in Russia, where Cloudflare has been throttled since 2025-06-09. A family's own server is therefore the main deployment, not a fallback (see ADR 0004).

## Decision

Use Go, with the standard library for HTTP and XML, and `modernc.org/sqlite` for storage. Cross-compile with `CGO_ENABLED=0`.

## Comparison

| Criterion | Go | Rust |
| --- | --- | --- |
| One static binary | Yes, cross-compiles with no C toolchain | Yes, needs a C toolchain or `cross` for SQLite |
| HTTP, TLS, XML in the standard library | Yes | No, about 150 to 250 crates for a similar server |
| Memory safety | Yes, data races are possible; CI runs `-race` | Yes, data races are rejected at compile time |
| Vulnerability audit | `govulncheck` checks reachable code, which gives little noise | `cargo-audit` and `cargo-deny`; `cargo-deny` also checks licenses |
| CodeQL | Generally available | Generally available since 2025-10 |
| Other SAST | `gosec`, Semgrep rules, `staticcheck` | `clippy`; Semgrep support is less complete |
| Native run on Cloudflare Workers | No | Yes, through WebAssembly |
| Contributor pool for self-hosted tools | Large | Large, with a steeper learning curve |

## Consequences

- The standard library holds the sensitive code paths, and the Go security team maintains it.
- The only direct dependency is the module `modernc.org/sqlite`. Each new dependency needs a reason in the pull request.
- `encoding/xml` does not load external entities. XXE and entity expansion do not apply to the importer. A test covers both.
- Heirfold does not run on Cloudflare Workers. Workers would also need a second storage layer, because D1 has no interactive transactions.
- Cloudflare can still front a Heirfold server through Tunnel and Access (see ADR 0004).

## When to review this decision

Review it if a large share of users need Cloudflare Workers specifically, or if a data race reaches production.
