# ADR 0006: Read the Gramps database directly, one way

Status: accepted, 2026-11-09.

## Context

Many people keep their tree in Gramps Desktop and want relatives to see it on the web. With an export file, each change needs a new export and a new import. A direct read of the Gramps database removes these steps.

Gramps 6.0 keeps a tree in one SQLite file. Each object is a JSON document. Older versions keep Python pickles, which a Go program must not read: a pickle can run code.

## Decision

- Heirfold reads the SQLite database of Gramps 6.0 or later with `heirfold serve --gramps PATH`. Older trees give a clear error. Gramps 6 upgrades a tree when it opens that tree.
- Heirfold opens the file read-only and never writes to it. One read transaction gives one consistent state, also while Gramps is open.
- The sync goes one way: Gramps is the source. Heirfold checks the size and the time of the file every 3 seconds. After a change it reads the tree, compares a fingerprint of the content, and replaces its own tree only if the content changed.
- A tree that follows Gramps takes no upload. The import screen is off in this mode.
- A browser asks for the revision number of the tree every 10 seconds and loads the tree again after a change.

## Check against Gramps

The reader must give the same documents as the XML export of Gramps itself. Two checks compare a direct read with that export:

| Tree | Objects | Result |
| --- | --- | --- |
| The example tree of Gramps 6.0.8 | 12,536 | Equal |
| A small tree of invented people that uses every feature of the format | 38 | Equal. This tree is the test data of the repository. |

`TestOracle` in `internal/grampsdb` repeats the check on any tree.

The tables of type names in `internal/grampsdb/types.go` come from the format definition of Gramps (GPL-2.0-or-later). [ADR 0005](0005-license.md) covers the license.

## Photos and scans

- The server shows a file only if its real path lies inside the media directory (`--media-dir`). A tree can name any path, so a path outside that directory is refused. Symbolic links that leave the directory are refused too.
- In the Gramps mode the media path of the tree is the default, because the administrator named that tree. An uploaded file never sets the directory.
- Pictures go out as JPEG in three sizes. The original goes out as a download only, never inline.
- A guest cannot open a private file. A guest also cannot open a file that a living person leads to: the files of that person, of their events, families and citations, and of the sources behind those citations.

## Consequences

- Editing stays in Gramps for a tree in this mode. A two-way sync needs writes to the Gramps database, and Heirfold never writes to it ([ADR 0008](0008-editing.md)).
- Each change replaces the whole tree in the Heirfold database. For a tree of 2,000 people this takes less than a second.
- Rendered pictures live in memory only (96 MB at most). A restart renders them again.
- Heirfold reads JPEG, PNG and GIF as pictures. The viewer also shows PDF, Word and text files ([ADR 0009](0009-document-view.md)). Other files, such as TIFF, show as a download.
