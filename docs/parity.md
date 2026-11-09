# Parity with Gramps Web

This table compares Heirfold with the [features of Gramps Web](https://www.grampsweb.org/features/), [its user system](https://www.grampsweb.org/install_setup/users/), [OIDC](https://www.grampsweb.org/install_setup/oidc/), [the blog](https://www.grampsweb.org/user-guide/blog/) and [sync](https://www.grampsweb.org/administration/sync/). The source pages were read on 2026-11-09. The project has not run Gramps Web itself, so the table relies on its documentation.

The Issue column lists the roadmap issue of each open feature. It shows "planned" until the issues exist on GitHub.

**Summary:** 18 of 55 features are done, 8 are partial, and 29 are planned.

## Features of Gramps Web

### Data and exchange

| Feature | Status | Note | Issue |
| --- | --- | --- | --- |
| People, families, events, places, sources, citations, repositories, notes, media | Partial | All are stored and exported. People have pages; other types appear inside them. | planned |
| Import Gramps XML | Done | Plain and compressed `.gramps` files. |  |
| Import preview before writing | Done |  |  |
| Export Gramps XML | Done | Lossless, valid against the DTD. |  |
| Import Gramps package (`.gpkg`) with media | Planned |  | planned |
| Import and export GEDCOM | Planned |  | planned |
| Full backup with media and accounts, and restore | Planned |  | planned |
| Two-way sync with Gramps Desktop | Partial | By design one copy is the main copy: Heirfold follows a Gramps 6 database, or it holds the tree and exchanges Gramps XML. See [ADR 0008](adr/0008-editing.md). | planned |

### Tree views

| Feature | Status | Note | Issue |
| --- | --- | --- | --- |
| Ancestor tree | Done |  |  |
| Descendant tree | Done |  |  |
| Hourglass | Done |  |  |
| Fan chart | Done |  |  |
| Relationship graph | Done | The view «Whole tree» shows a person and everybody who is linked to that person, with all the links, on one canvas. |  |
| Configurable generations | Done |  |  |
| Preview card with key facts | Done |  |  |
| Edit in the chart | Planned |  | planned |

### Time and place

| Feature | Status | Note | Issue |
| --- | --- | --- | --- |
| Zoomable timeline | Planned |  | planned |
| Interactive map, life paths | Planned |  | planned |
| Historical map overlays, time slider | Planned |  | planned |

### Search

| Feature | Status | Note | Issue |
| --- | --- | --- | --- |
| Full-text search in all record types | Partial | People by name only. | planned |
| Wildcards and logical operators | Planned |  | planned |
| Query language in lists | Planned |  | planned |
| Quick filters by text, tag, privacy | Planned |  | planned |
| External search on other sites | Planned |  | planned |

### Editing

| Feature | Status | Note | Issue |
| --- | --- | --- | --- |
| Add, edit and delete records | Partial | People, links between people, events, notes and portraits. Sources, citations, repositories, places and tags are read-only. | planned |
| Revision history with undo | Done | A journal of all edits and a journal for each person. |  |
| Bulk delete, merge duplicates | Planned |  | planned |
| Tags | Partial | Shown on person pages. | planned |
| Bookmarks and recent history | Planned |  | planned |

### Content

| Feature | Status | Note | Issue |
| --- | --- | --- | --- |
| Blog | Planned |  | planned |
| Task management | Planned |  | planned |
| Printable reports | Planned |  | planned |
| Media gallery | Partial | A photo gallery shows the photos of the tree, with upload, marks on faces, filters by people, years and places, and a map of the places that the app draws itself. Photos and page scans also show on person pages, at events and at citations, with a viewer. The viewer also shows PDF files, Word files (.docx) and text files; other files are downloads. No upload of documents in the browser yet. | planned |
| Tag people in photos, face detection | Done | An editor marks faces in the photo gallery. The server finds the faces and, with the recognition model installed, suggests who a face is ([ADR 0010](adr/0010-photo-gallery.md)). All of it runs on the server of the family. |  |
| Text recognition (OCR) | Planned |  | planned |
| Data verification | Planned |  | planned |

### Advanced

| Feature | Status | Note | Issue |
| --- | --- | --- | --- |
| DNA matches, chromosome browser, Y-DNA | Planned |  | planned |
| AI assistant | Planned |  | planned |

### Accounts and access

| Feature | Status | Note | Issue |
| --- | --- | --- | --- |
| Six roles, Guest to Admin | Done | Four roles by design: guest, family member, tree editor, administrator. See [ADR 0007](adr/0007-sign-in.md). |  |
| Private records filtered at the database layer | Done |  |  |
| Sign-in with a password | Done | By design a one-time code from e-mail replaces the password. |  |
| Create the first owner in the browser | Done | A one-time link in the server output. |  |
| Manage users in the browser and on the command line | Done |  |  |
| OpenID Connect sign-in | Planned |  | planned |
| Self-registration with approval | Planned |  | planned |
| Email for invitations and password reset | Done | Invitations and sign-in codes go by SMTP or by the Resend API. No password exists to reset. |  |
| Several trees on one server | Planned |  | planned |

### Everyday use

| Feature | Status | Note | Issue |
| --- | --- | --- | --- |
| Works on phones and desktops | Done |  |  |
| Installable app | Partial | Web manifest only. | planned |
| Keyboard shortcuts | Planned |  | planned |
| Interface languages | Partial | Russian and English. Gramps Web has more than 50. | planned |
| Background tasks and notifications | Planned |  | planned |
| Customisable lists | Planned |  | planned |
| Calendars | Partial | Julian dates display. Others convert approximately. | planned |
| Site name, colours, home text | Planned |  | planned |

## Beyond Gramps Web

| Feature | Status | Issue |
| --- | --- | --- |
| Living people hidden by default for guests | Done |  |
| Invitations by e-mail with a role, also from a tree editor | Done |  |
| An account linked to its person in the tree; the tree opens on that person | Done |  |
| A family member can change the own branch: the person, the descendants and the partners | Done |  |
| A family member can download the whole tree as a Gramps XML file | Done |  |
| Sign-in without a password; a device stays signed in for a year | Done |  |
| List of signed-in devices, sign-out on other devices | Done |  |
| A fresh sign-in code before a change of access | Done |  |
| Automatic copy of the tree before a replacement | Done |  |
| Strict CSP with Trusted Types, no third-party origins | Done |  |
| Audit log | Done | planned |
| One binary, no Python, no Redis | Done |  |
| Direct read of a Gramps Desktop database, read only, with live updates in the browser | Done |  |
| Files of living people hidden from guests | Done |  |
| Security checks in CI | Done |  |
| Passkeys and two-factor sign-in | Planned | planned |
| Show how two people are related | Planned | planned |
| Encrypted read-only snapshot | Planned | planned |
| Browser tests in CI | Planned | planned |
| Accessibility audit | Planned | planned |
| Fuzzing of the parser | Planned | planned |
| Signed container images | Planned | planned |
| Independent security review | Planned | planned |
| Rich text in notes | Planned | planned |
