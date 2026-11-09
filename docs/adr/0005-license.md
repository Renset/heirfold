# ADR 0005: Use AGPL-3.0-or-later and write the code from scratch

Status: accepted, 2026-11-09

## Context

Heirfold replaces Gramps Web and reads the Gramps file format. The licenses of the related projects, checked on 2026-11-09:

| Project | License |
| --- | --- |
| Gramps (desktop) | GPL-2.0-or-later (source headers), GPL-2.0 (GitHub metadata) |
| Gramps Web (frontend) | AGPL-3.0 |
| Gramps Web API | AGPL-3.0 |
| Gramps XML DTD (`grampsxml.dtd`) | GPL-2.0-or-later |

## Decision

License Heirfold under **AGPL-3.0-or-later**.

Write the code from scratch. Heirfold copies no code, templates, styles, translations or test data from Gramps or Gramps Web. The XML reader and writer implement the public format definition.

## Reasons

- AGPL protects a family-data product from closed hosted forks. Anyone who runs a modified version as a service must offer the source to its users.
- A person who hosts Heirfold for their own family, unmodified, has no extra duty.
- AGPL-3.0 is compatible with AGPL-3.0 code. GPL-2.0-or-later code can be used under GPL-3.0, and GPL-3.0 and AGPL-3.0 allow combination (section 13 of each). If a contributor later reuses code from these projects, the license permits it, provided that the contribution keeps the attribution.
- The dependencies use permissive licenses: BSD, MIT, Apache-2.0 and SIL OFL-1.1. The map data is public domain or CC BY 4.0. [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) has the list.

## Rules

- A contribution that copies code from another project must name the source and its license in the pull request.
- The file `example.gramps` from Gramps stays out of this repository. A developer can point a local test at it through `HEIRFOLD_EXAMPLE_GRAMPS`.

## Consequences

Companies that want to embed Heirfold in a closed service cannot do so. The project accepts that cost.

This ADR is an engineering decision and is not legal advice.
