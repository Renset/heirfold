# Security policy

Heirfold holds private family data. Please report security problems privately.

## Report a vulnerability

1. Open the [Security tab](https://github.com/Renset/heirfold/security) of this repository.
2. Click **Report a vulnerability**.
3. Describe the problem and the steps to reproduce it.

Do not open a public issue. Do not include real family data in a report. Use the fixture generator (`heirfold fixture`) for examples.

The maintainer is one person. The target is a first response within 7 days. There is no bug bounty.

## Supported versions

Only the latest release and the `main` branch receive fixes. Heirfold has no stable release yet.

## Security model

| Threat | Protection | Check |
| --- | --- | --- |
| A guest reads living people or private records | Every read goes through a privacy scope in `internal/store`. | Unit tests with canary strings; smoke test |
| XSS | CSP with `script-src 'self'` and Trusted Types with no policy. The frontend has no HTML sinks. | `npm run check`; header tests |
| CSRF | `SameSite=Strict` cookies and `http.CrossOriginProtection` | Server tests |
| A guess of a sign-in code | 8 digits, 5 tries, 10 minutes of life, at most 5 live codes for an account, rate limits for each client and each address. A code that cannot be sent is not stored. | Store and server tests |
| A stranger locks a relative out | A browser that signed in to the account before carries a signed device cookie and has its own allowance of codes and letters. | Server tests |
| A sign-in code used in another browser | A code works only with the secret cookie of the browser that asked for it. | Server tests |
| A question whether an address has an account | The same answer for a known and an unknown address; the work runs after the answer. | Server tests |
| A long session changes who has access | A change of a role or an address, a removal of an account that signed in, an invitation of an editor or an administrator, and the import of a new tree need a sign-in code from the last 30 minutes. An invitation of a guest or a family member and the export do not. | Server tests |
| An editor damages the tree | Each edit is one journal entry with the state before and after. An editor can undo it while later edits do not depend on it. A link that makes a person their own ancestor is refused. Heirfold saves a copy before it replaces a tree. | Store tests |
| Two editors change one record | An update of a person, an event or a note names the state that the editor saw. The server refuses an update of another state. The state has a resolution of one second. | Store tests |
| An uploaded picture carries a script or a path | The server reads the format from the bytes, accepts JPEG and PNG, decodes the whole file, limits the size, the decoded memory and the space of all uploads, and names the file by its hash. | Server tests |
| A header injected into a letter | Checked addresses, no line breaks in headers, a base64 body | Mail tests |
| Stolen database | The database stores only hashes of session tokens and of sign-in codes. It holds no passwords. The tree itself is not encrypted. | Unit tests |
| Malicious import file | No external entities, limits on size and object count, safe handling of gzip bombs | Parser tests |
| A tree names a file outside the media directory | The real path of a file must lie inside `--media-dir`. Symbolic links that leave it are refused. | Server tests |
| A media file carries a script | Pictures go out as JPEG that the server made. Originals go out as downloads only, and only a few safe content types keep their type. | Server tests |
| A Word file or a PDF carries a script or markup | The server reads a Word or text file itself and sends plain data: text, headings, lists, tables, links, notes and colors. A color has the form of six hexadecimal digits. The page builds its own elements from this data, so no markup of the file reaches the browser. Links keep only `http`, `https` and `mailto` addresses. The reader has limits on the size after decompression, on the text and on the number of blocks. A PDF is drawn by PDF.js on canvases, on the page itself. The Content Security Policy stays the same: no worker threads, no WebAssembly, no code from text, no frames. | Reader and server tests, [ADR 0009](docs/adr/0009-document-view.md) |
| A photo of the gallery shows a living person to a guest | A guest has no gallery. A new photo is private, so guests do not get it until an editor opens it. An open photo is still closed for guests while a living person is marked on it. The card that a guest sees names no portrait from such a file. | Store and server tests, [ADR 0010](docs/adr/0010-photo-gallery.md) |
| A family member reads a hidden note | A note with the private flag goes only to editors and administrators. The view of a person, the edit form of the own branch and the file of the tree leave its text out for everybody else, and the edit routes answer "not found" for it. A test signs in with each role and reads all three. |
| A family member changes more than the own portrait | A family member reaches only the routes of the pictures of the person of the own account. The store compares the tree before and after such an edit and accepts only a change of the links from that person to pictures, a new file, and the removal of an own file that is no photo of the gallery. |
| The place of a photo leaves the family | The server reads the place from the file itself and shows it to family members and above, never to a guest. It finds the name of the town in a list inside the program and draws the map from its own files, so by default it asks no service for a name or a map. A link to OpenStreetMap opens only when the reader presses it. An operator can turn on a tile service for a map with streets (`--map-tiles`). Then that service sees the address of the server and the parts of the world that the family looks at, as numbers of map tiles. It sees no relative and no cookie: only the server asks it, with the name of the program. The start-up text of the server says which map is on. |
| The address of a tile service makes the server ask a wrong place | Only the operator sets the address, at the start of the server. A request adds three checked whole numbers to it. The server takes an answer only if it is a picture of at most 2 MB, and it asks at most two tiles at one time. |
| A picture file with a broken EXIF block harms the server | The reader of EXIF is own code. It reads at most 256 KB, checks every offset against the size of the block, and takes numbers and short texts only. A test cuts and breaks a file byte by byte. |
| The numbers of a face leave the family | The server finds and compares faces itself, with models that are files on the server. No picture and no number goes to another service. The numbers are in the database only: no export and no page carries them, and they go away with the photo. Only editors and administrators get the found faces. `--no-faces` turns the search off. | Store and server tests, [ADR 0010](docs/adr/0010-photo-gallery.md) |
| A model file is replaced by another file | The finder is a part of the binary. The server loads the recognition model only if its SHA-256 is the pinned value. The engine refuses a model with an operator that it does not know, and it limits the size of each tensor. | Unit tests |
| A guest opens a photo of a living person | Files that a living person leads to, and files that nothing leads to, are hidden from guests, like private files. | Store and server tests |
| A very large picture exhausts memory | Limits on pixels and on decoded bytes before decoding, a limit on pictures decoded at one time, and a limit on new decodes for each account | Imaging tests |
| Heirfold damages the Gramps database | The database is opened read-only. A test compares the file before and after a read. | Reader tests |
| Clickjacking | `frame-ancestors 'none'` and `X-Frame-Options: DENY` | Header tests |
| DNS rebinding | Host allowlist on the loopback interface or with `--public-host` | Server tests |
| Vulnerable dependencies | `govulncheck`, OSV-Scanner, `npm audit`, Dependabot with a 7-day cooldown | CI |
| Compromised GitHub Actions | Actions pinned by SHA, minimal token permissions, `zizmor` | CI |
| Leaked secrets | `gitleaks` on the whole history, GitHub push protection | CI |
| Real family data in the repository | Ignore rules and a check of the whole history | CI |

## Known limits

- The database file and any future media files are not encrypted at rest. Protect the disk and the backups.
- A sign-in is as safe as the mailbox of the person. 2FA and passkeys do not exist yet. See the roadmap.
- A server without mail settings writes each sign-in code to its log. Use that only on your own computer.
- A letter travels through the mail service that the administrator chose. That service sees the address of each relative and each sign-in code.
- The rate limits live in memory and reset on restart.
- An attacker who knows the address of a relative can use up the code allowance of that account for browsers that never signed in to it, for as long as the attack lasts. A browser that signed in before keeps its own allowance. A relative who signs in for the first time during such an attack must wait.
- An editor learns whether an address already has an account: an invitation to it fails.
- The audit log and the journal of edits name a person by the display name of the account.
- A very large import needs a lot of memory: about 2 GiB at the object limit. Only an administrator can import.
- An administrator can export everything, including private records.
- A tree editor sees the whole tree and can change or delete any person. The journal keeps each change, and an undo brings the old state back.
- The journal of edits keeps old versions of records in the database, also of records that an editor deleted.
- The repository check looks at file types. It cannot find family names inside a text file. The publishing checklist has a manual scan for that.
- The media directory is a trust boundary. A person who can edit the tree can make any file inside it visible to members. Keep only photos of the tree in it.
- The picture decoders of the Go standard library read the files. A very unusual file can use much processor time.
- Heirfold has had no external security audit.
