# ADR 0003: Store lossless documents and filter every read

Status: accepted, 2026-11-09

## Context

Heirfold must import Gramps data, export it again with no loss, and hide private records and living people from viewers who have no right to see them. Gramps Web offers the same primary object types, so the model has to match them for parity and for later two-way sync.

## Decision

### One model, three uses

`internal/model` mirrors the Gramps XML 1.7 definition. The same structs read XML, write XML, and serialize to JSON.

### Storage

One SQLite file per tree.

| Table | Purpose |
| --- | --- |
| `object` | Every Gramps object as a lossless JSON document, keyed by kind and handle |
| `person_index` | ID, private and living flags, and two short cards per person |
| `family_index`, `family_child` | The structure of the tree |
| `media_index` | What the server needs to show a file, with the living flag and the gallery flag |
| `user`, `session`, `login_code`, `audit` | Accounts, sign-in codes and the audit log |
| `change_set`, `change_item` | The journal of edits ([ADR 0008](0008-editing.md)) |
| `face`, `face_scan`, `media_info` | The found faces and the data from the files of pictures, outside the tree ([ADR 0010](0010-photo-gallery.md)) |
| `meta` | Header, bookmarks and other tree-level data |

Each person has two cards. `card_full` may use private facts. `card_public` is built only from public names and events.

A future multi-tree setup gives each tree its own database file. A bug in one query then cannot cross trees, and a backup is one file.

### Privacy filter

Read access for viewers exists only through `store.View`, which always holds a scope. A scope has three switches: private records, living people and hidden notes. A hidden note is a note with the private flag ([ADR 0007](0007-sign-in.md)).

| Role | Private records | Living people | Hidden notes |
| --- | --- | --- | --- |
| Guest | No | No | No |
| Family member | Yes | Yes | No |
| Tree editor, administrator | Yes | Yes | Yes |

The filter works in two layers:

1. SQL conditions remove hidden people and families.
2. The detail builder removes nested parts: private names, attributes, links, notes, citations, events and places.

Further rules:

- A citation of a hidden source disappears, because it would reveal the source.
- If a spouse is hidden, the events and notes of the couple are hidden too.
- A guest never sees a placeholder for a hidden person. A family with fewer than two visible members does not appear.
- A note style can link to another record, so only viewers with full access receive styles.

### Who is living

`privacy.Living` treats a person as living unless evidence proves otherwise. The evidence is a death-type event of the person, a birth more than 110 years ago, or a bound that family links prove. A person's own birth date always beats a bound from relatives, so one mistyped date cannot reveal a living person. Each step through a family link uses the widest gap that is possible, and a link to a missing person is no evidence. A date with no upper bound ("after 1900", or a range with no end) proves nothing. A mistake therefore hides a dead person and should never reveal a living one. The server recomputes the result at start and once a day, because the answer changes with the calendar.

### Identifiers

`handle` is the internal key. The web address uses the Gramps ID. If a file has duplicate or missing IDs, the store assigns a unique value and keeps the original in the document.

## Consequences

- Export is lossless. A test compares a tree before and after a round trip. It also runs on the Gramps example file on a developer machine.
- Full-text search needs a separate index, and that index must respect the scope.
- The graph endpoint returns the whole visible structure. It suits trees up to tens of thousands of people. Larger trees need a neighbourhood endpoint.
- Gramps XML written by Heirfold validates against `grampsxml.dtd`.
