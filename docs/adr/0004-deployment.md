# ADR 0004: Make a family's own server the main deployment

Status: accepted, 2026-11-09

## Context

A family needs a choice of hosting. Relatives in different countries must reach the site. Known facts on 2026-11-09:

- Cloudflare has been throttled in Russia since 2025-06-09. There are also reports of failures at OVH, Hetzner and DigitalOcean.
- GitHub Pages is public and has no server-side authorization.
- Cloudflare Workers do not run Go natively (see ADR 0001).
- Cloudflare Containers became generally available in 2026. Their disk is not a dependable place for a database file. The vendor suggests a FUSE mount to R2. Heirfold has not tested this.

## Decision

Heirfold supports four deployment models, in this order of priority.

| Model | Data on the host | Editing | Access control | Status |
| --- | --- | --- | --- | --- |
| A. One binary on a personal computer | Local file | Yes | Heirfold accounts | Works now |
| B. Own server or VPS, with Caddy or nginx for HTTPS, or a container | Local file | Yes | Heirfold accounts | Works now |
| C. Model B behind Cloudflare Tunnel and Access | Local file | Yes | Cloudflare Access plus Heirfold accounts | Documented, not tested |
| D. Static encrypted snapshot on GitHub Pages, Cloudflare Pages or R2 | Ciphertext only | No | Passphrase | Planned |

### Rules for each model

- **Model A** listens on `127.0.0.1` by default.
- **Plain HTTP on a public address** stops the server, unless the operator sets `--insecure-http`.
- **Model C** is not a good choice if relatives live in Russia.
- **Model D** must never publish plaintext. GitHub Pages is public, so anyone can download the ciphertext and try passphrases offline. The passphrase must therefore be generated, with at least 128 bits of entropy. The key cannot be revoked after publication.

### Public demo

The demo on GitHub Pages uses the same static bundle format as model D, but without encryption. Its only data source is the fixture generator. The workflow takes no input, and no step reads a family file.

### Choosing a server for relatives in several countries

The owner tests a candidate host from each country before moving the family there. Heirfold has no dependency on any CDN, so the host and the domain can change without a code change.

## Consequences

- There is one code path and one storage engine to secure.
- Cloudflare Workers are not supported.
- Backups are the responsibility of the operator. The database is one file, and `heirfold export` writes a standard Gramps file.
