# ADR 0008: Edit the tree in Heirfold, with one main copy

Status: accepted, 2026-11-10.

## Context

A family needs full work with the tree in the web interface. Heirfold can follow a Gramps database one way ([ADR 0006](0006-gramps-database.md)). This decision says how web edits and Gramps live together.

Gramps Web keeps its own Gramps database on the server. Gramps Desktop is a second copy, and the Gramps Web Sync add-on merges the two by time stamps. Its documentation says that a deletion on one side deletes on the other and that the add-on is not a backup. Two main copies always need a merge, and a wrong merge loses family data.

A direct write into the Gramps database from Go would depend on the private storage format of Gramps, with its index tables and caches. So the choice is a sync through the public interfaces of Gramps, or no sync.

## Decision

**One copy is the main copy at any time.** The administrator chooses it at start.

| Mode | Start | Main copy | Edits in Heirfold | Exchange with Gramps |
| --- | --- | --- | --- | --- |
| View | `heirfold serve --gramps PATH` | Gramps | Off | Heirfold reads the database and follows it. |
| Edit | `heirfold serve` | Heirfold | On for editors | Gramps XML: export from Heirfold, import into Gramps, and the other way. |

- Heirfold never writes to a Gramps database. It has no merge and no own sync protocol.
- In the edit mode each action of an editor is one entry of a journal. The entry keeps each changed object before and after the action, with the editor and the time.
- An editor can undo an entry while every object of the entry still has the state that the entry left. Later edits of the same objects must be undone first. An undo is a new entry, so an undo can be undone.
- Each edit checks the state that the editor saw. If another editor changed the record in the meantime, the edit is refused and the page shows the new state.
- All edits run one after the other in one transaction each. After an edit, Heirfold rebuilds the tables that the views read from the whole tree. This is simple and always consistent. It costs some milliseconds for a family tree.
- A change of the mode is a deliberate act. Heirfold refuses to follow a Gramps database while its own tree has edits from Heirfold, unless the administrator gives `--gramps-replace-edits`. Before any replacement of a tree, Heirfold saves the old tree as a Gramps XML file in `backups` in the data directory and keeps the newest 10 files.
- Editors add pictures as JPEG or PNG. Heirfold checks the format from the bytes, names the file by its hash, and keeps it in `heirfold` inside the media directory.
- A portrait is the first picture of a person, as in Gramps, unless a mark on the link says that the picture is no portrait (ADR 0010). An editor chooses the portrait among the pictures and chooses the square part that the portrait shows. Heirfold stores that part as the region of the link between the person and the picture, in whole percent, which is the Gramps format. The file never changes.
- A family member with the right for the own branch uses the same edit routes as an editor. The store checks such an edit after it ran and before it is stored: it compares the tree before and after. It refuses a change of a person or a family outside the branch, a removed person, family or link, a person from outside who is linked into the branch, a changed privacy mark, a change of an event, a note or a file that somebody outside the branch uses, and any change of other records or of the settings of the tree. The check does not read the request, so a new edit operation cannot go around it. Such a user sees and takes back the own journal entries only.
- The page of a person lists the events of the person and of the couples of the person together. A wedding belongs to the couple in the data, but a reader looks for it among the events of a life.

## What an editor can change now

People (names, gender, privacy), links between people (parents, spouses, children, brothers and sisters), events of people and of couples with dates and places, notes, portraits, and the person at the centre of the tree. Sources, citations, repositories, tags and places as objects are read-only for now. [The parity table](../parity.md) has the list.

## Consequences

- A family chooses: research in Gramps and show the tree in Heirfold, or keep the tree in Heirfold and let relatives help.
- A tree that moves between the two programs goes through Gramps XML, the public exchange format of Gramps. Heirfold tests a lossless round trip of that format.
- An object that nothing points to after an edit, for example a place of a removed event, stays in the tree. Gramps has a tool that removes unused objects.
- If people later ask for a sync with Gramps Desktop, the plan is to speak the part of the Gramps Web API that the stock Gramps Web Sync add-on uses. A person then syncs with the tool that Gramps already has. That work needs tests against the real add-on and gets its own ADR.
