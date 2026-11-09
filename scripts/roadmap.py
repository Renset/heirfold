"""The roadmap: GitHub issues and the parity table.

Create the issues once, after the repository is public:
    python3 scripts/roadmap.py --issues
Write the parity table (run it again after --issues to add the links):
    python3 scripts/roadmap.py --parity docs/parity.md
"""
import json, subprocess, sys, time, os

REPO = os.environ.get("HEIRFOLD_REPO", "Renset/heirfold")
STATE = os.path.join(os.path.dirname(__file__), "roadmap-issues.json")

MILESTONES = {
    "m1": ("M1: Readable family site", "A relative reads the whole tree safely on a phone."),
    "m2": ("M2: Collaboration", "Family members edit the tree, and every change has a history."),
    "m3": ("M3: Research tools", "Features for the keeper of the tree, and sync with Gramps Desktop."),
    "m4": ("M4: Optional extras", "Features that few families need."),
}

LABELS = {
    "parity": ("1d76db", "Needed for feature parity with Gramps Web"),
    "security": ("b60205", "Security or privacy"),
    "ui": ("c5def5", "Frontend"),
    "backend": ("0e8a16", "Go server"),
    "infra": ("5319e7", "CI, packaging, deployment"),
}

# key, title, milestone, labels, body
ISSUES = [
    ("media", "Store and serve media files with access control", "m1", ["parity", "backend", "ui", "security"],
     "Show photos and documents on person pages.\n\n**Done when:**\n- Files are stored by checksum outside the database.\n- Every file response checks the privacy scope of the media object.\n- Thumbnails exist for images, and the server strips EXIF location data from them.\n- An SVG is never served inline.\n- The photo region of a person is used as the portrait."),
    ("gpkg", "Import Gramps packages (.gpkg) with media", "m1", ["parity", "backend", "security"],
     "**Done when:**\n- The importer reads a `.gpkg` archive and stores its media.\n- Path traversal, symlinks and archive bombs are rejected, with tests.\n- File paths use `os.Root`.\n- The preview lists missing files."),
    ("lists", "Add list and detail pages for all record types", "m1", ["parity", "ui", "backend"],
     "Families, events, places, sources, citations, repositories, notes and media need list and detail pages.\n\n**Done when:** each type has both pages, each read goes through `store.View`, and each new field has a privacy test."),
    ("notes", "Render note styles (bold, italic, links)", "m1", ["parity", "ui", "security"],
     "Gramps notes carry style ranges. Render them through DOM nodes.\n\n**Done when:** styles work with no HTML strings, and links are limited to http, https and mailto."),
    ("fts", "Full-text search with wildcards and logical operators", "m1", ["parity", "backend", "security"],
     "Search all record types, including note text.\n\n**Done when:**\n- SQLite FTS5 powers the search.\n- Results respect the privacy scope, with tests that use canary strings.\n- Wildcards and AND, OR and NOT work."),
    ("passkeys", "Add passkeys and two-factor sign-in", "m1", ["security", "backend", "ui"],
     "**Done when:**\n- WebAuthn passkeys work for registration and sign-in.\n- TOTP exists as a fallback, with recovery codes.\n- The Owner and Admin roles can be required to use a second factor."),
    ("reset", "Let owners reset passwords and end sessions", "m1", ["security", "backend", "ui"],
     "A relative who forgets a password needs a simple way back.\n\n**Done when:** an owner creates a one-time reset link, each user can see and end their own sessions, and the audit log records both."),
    ("howrelated", "Show how two people are related", "m1", ["ui"],
     "Relatives often ask how they are related to a person.\n\n**Done when:** a person page says the relationship to the signed-in user or the home person, in Russian and English, with correct kinship terms."),
    ("e2e", "Run browser tests in CI", "m1", ["infra", "ui"],
     "**Done when:** Playwright covers sign-in, invitation, tree navigation and the role limits on phone and desktop viewports, and it fails on any CSP violation in the console."),
    ("a11y", "Audit accessibility against WCAG 2.2 AA", "m1", ["ui"],
     "**Done when:** automated checks run in CI, the charts have a keyboard path and a text alternative, and large text settings do not break layouts."),
    ("fuzz", "Fuzz the Gramps XML parser", "m1", ["security", "infra"],
     "**Done when:** Go native fuzz targets cover the parser and the date reader, and a short run happens in CI on each pull request."),
    ("audit-log", "Add filters and retention to the audit log", "m1", ["security", "backend", "ui"],
     "**Done when:** an owner can filter by user and action, the retention period is configurable, and the log can be exported."),

    ("editing", "Edit people, families, events and other records", "m2", ["parity", "backend", "ui", "security"],
     "**Done when:**\n- Forms exist for all record types.\n- The Contributor and Editor roles are enforced on the server.\n- Writes keep unknown fields, so export stays lossless.\n- A test confirms that each write path checks the role."),
    ("chartedit", "Build the tree inside the chart", "m2", ["parity", "ui"],
     "**Done when:** a button on each person adds a father, mother, child or spouse, links an existing person or creates a new one, and each change saves at once."),
    ("revisions", "Record every edit and allow undo", "m2", ["parity", "backend", "ui"],
     "**Done when:** each transaction stores who changed what and when, the history shows field differences, and a transaction can be undone."),
    ("blog", "Add a family blog", "m2", ["parity", "ui", "backend"],
     "Store a post as a source with the `Blog` tag and a note, the same way as Gramps Web, so that posts sync with Gramps Desktop.\n\n**Done when:** posts have a title, author, rich text, a gallery, tags and a private switch."),
    ("timeline", "Add a zoomable timeline", "m2", ["parity", "ui"],
     "**Done when:** the timeline shows events, zooms, and filters by person, ancestors, descendants or place."),
    ("map", "Add an interactive map without third-party calls by default", "m2", ["parity", "ui", "security"],
     "**Done when:**\n- The map shows places and people, and connects the events of a life.\n- The operator chooses the tile source. No tile request leaves the server unless the operator enables it, because tile requests reveal which places a family views.\n- The CSP stays strict."),
    ("oidc", "Add OpenID Connect sign-in", "m2", ["parity", "security", "backend"],
     "**Done when:**\n- Custom issuers work with PKCE, and the Google and Microsoft presets exist.\n- Group claims map to roles.\n- New accounts start without access.\n- A login never merges into a local account by email."),
    ("email", "Send email for invitations and password reset", "m2", ["parity", "backend"],
     "Email stays optional. Links still work without it.\n\n**Done when:** SMTP settings exist, and invitations and resets can go by email."),
    ("gedcom", "Import and export GEDCOM", "m2", ["parity", "backend", "security"],
     "**Done when:** GEDCOM 5.5.1 and 7 import with a preview, export round-trips the main fields, and the documentation lists the known losses."),
    ("backup", "Back up and restore everything", "m2", ["parity", "backend", "security"],
     "**Done when:** one archive holds the tree, media, accounts and settings, it can be restored, and the archive can be encrypted."),
    ("snapshot", "Publish an encrypted read-only snapshot", "m2", ["security", "backend", "ui"],
     "Deployment model D from ADR 0004.\n\n**Done when:**\n- The server writes one encrypted file with AES-256-GCM and a key from a strong KDF.\n- A static page decrypts the file in the browser.\n- The tool generates the passphrase and refuses weak ones.\n- The documentation explains that the key cannot be revoked."),
    ("pwa", "Finish the installable app and offline reading", "m2", ["parity", "ui", "security"],
     "**Done when:** the app installs on a home screen, and offline reading is optional and off by default on shared devices."),
    ("tags", "Manage tags in the interface", "m2", ["parity", "ui", "backend"], "**Done when:** tags can be created, coloured, assigned and used as filters."),
    ("bookmarks", "Add bookmarks and recent history", "m2", ["parity", "ui"], "**Done when:** users can bookmark records and return to recently viewed people."),
    ("listfilters", "Customise lists: columns and quick filters", "m2", ["parity", "ui"], "**Done when:** users choose columns and filter by text, tag and privacy."),
    ("relgraph", "Add a relationship graph view", "m2", ["parity", "ui"], "**Done when:** a graph shows all connected relatives within a chosen distance."),
    ("calendars", "Convert all Gramps calendars exactly", "m2", ["parity", "backend", "ui"], "Julian dates display today. Sorting of other calendars is approximate.\n\n**Done when:** Hebrew, French Republican, Persian, Islamic and Swedish dates convert exactly and users can choose how to display them."),
    ("i18n", "Add a translation workflow and more languages", "m2", ["parity", "ui"], "Gramps Web has more than 50 languages. Heirfold has Russian and English.\n\n**Done when:** contributors can add a language without code changes, and at least Ukrainian, German and Spanish exist."),
    ("jobs", "Run long work in the background with notifications", "m2", ["parity", "backend", "ui"], "**Done when:** imports, exports and index rebuilds run in the background, and users can see progress and errors."),
    ("shortcuts", "Add keyboard shortcuts", "m2", ["parity", "ui"], "**Done when:** pressing `?` lists the shortcuts, and each main view has them."),
    ("branding", "Let owners set the site name, colours and home text", "m2", ["parity", "ui", "backend"], "**Done when:** an owner can change these without a rebuild, and the values cannot inject code or styles."),
    ("container", "Publish signed container images", "m2", ["infra", "security"], "**Done when:** a multi-architecture image goes to GHCR on each release, with provenance and an SBOM attached."),

    ("sync", "Sync with Gramps Desktop", "m3", ["parity", "backend", "security"],
     "Gramps Web uses the Gramps Web Sync add-on. Decide between an API compatible with that add-on and a new add-on.\n\n**Done when:** changes flow both ways, conflicts are visible, deletions are confirmed, and media syncs. The sync is documented as not a backup."),
    ("gql", "Add an advanced query language for lists", "m3", ["parity", "backend", "ui"], "**Done when:** a documented query language filters every list, and it respects the privacy scope."),
    ("histmap", "Add historical map overlays and a time slider", "m3", ["parity", "ui"], "**Done when:** a historical map stored as media can be georeferenced and shown, and a slider shows places over time."),
    ("bulk", "Bulk delete and merge duplicates", "m3", ["parity", "ui", "backend"], "**Done when:** list views support multi-select, and two records can be merged with a preview and undo."),
    ("verify", "Check the data for implausible dates and other problems", "m3", ["parity", "backend", "ui"], "**Done when:** a report finds problems such as a child born before a parent."),
    ("tasks", "Add research tasks", "m3", ["parity", "ui", "backend"], "Store tasks as sources, the same way as Gramps Web, so that they are visible in Gramps Desktop.\n\n**Done when:** tasks have a status, priority, tags, rich text and attachments."),
    ("reports", "Generate printable reports", "m3", ["parity", "backend", "ui"], "**Done when:** family reports and charts export to PDF inside the single binary, with no call to another service."),
    ("faces", "Tag people in photos", "m3", ["parity", "ui", "backend"], "**Done when:** a user can mark a face region and link a person. Automatic face detection stays on the server and is optional."),
    ("ocr", "Extract text from scanned documents", "m3", ["parity", "backend"], "**Done when:** text recognition runs on the server for Russian and English and stores its result as a note."),
    ("external", "Add external search links", "m3", ["parity", "ui", "security"], "**Done when:** a person page links to searches on FamilySearch and other sites, and sends only what the user confirms. Living people have no such links by default."),
    ("registration", "Allow self-registration with approval", "m3", ["parity", "backend", "ui"], "Off by default.\n\n**Done when:** a new account has no access until an owner assigns a role."),
    ("multitree", "Host several trees on one server", "m3", ["parity", "backend", "security"], "**Done when:** each tree has its own database file and users, and a test proves that no query crosses trees."),
    ("sec-audit", "Commission an independent security review", "m3", ["security"], "**Done when:** a third party reviews the authorization and the privacy filter, and the report is public."),

    ("dna", "Add DNA matches and a chromosome browser", "m4", ["parity", "ui", "backend", "security"], "DNA data is highly sensitive. Analysis must run on the server of the family.\n\n**Done when:** matches can be uploaded and viewed, and Y-DNA haplogroups can be estimated."),
    ("ai", "Add an optional assistant", "m4", ["parity", "backend", "security"], "Off by default. It must never send data of living people to another service unless the owner turns this on with a clear warning.\n\n**Done when:** a user can ask questions in their language, and the assistant respects the privacy scope."),
]

# Parity table: (group, feature, status, note, issue key or None)
D, P, N = "Done", "Partial", "Planned"
PARITY = [
    ("Data and exchange", "People, families, events, places, sources, citations, repositories, notes, media", P, "All are stored and exported. People have pages; other types appear inside them.", "lists"),
    ("Data and exchange", "Import Gramps XML", D, "Plain and compressed `.gramps` files.", None),
    ("Data and exchange", "Import preview before writing", D, "", None),
    ("Data and exchange", "Export Gramps XML", D, "Lossless, valid against the DTD.", None),
    ("Data and exchange", "Import Gramps package (`.gpkg`) with media", N, "", "gpkg"),
    ("Data and exchange", "Import and export GEDCOM", N, "", "gedcom"),
    ("Data and exchange", "Full backup with media and accounts, and restore", N, "", "backup"),
    ("Data and exchange", "Two-way sync with Gramps Desktop", N, "", "sync"),
    ("Tree views", "Ancestor tree", D, "", None),
    ("Tree views", "Descendant tree", D, "", None),
    ("Tree views", "Hourglass", D, "", None),
    ("Tree views", "Fan chart", D, "", None),
    ("Tree views", "Relationship graph", N, "", "relgraph"),
    ("Tree views", "Configurable generations", D, "", None),
    ("Tree views", "Preview card with key facts", D, "", None),
    ("Tree views", "Edit in the chart", N, "", "chartedit"),
    ("Time and place", "Zoomable timeline", N, "", "timeline"),
    ("Time and place", "Interactive map, life paths", N, "", "map"),
    ("Time and place", "Historical map overlays, time slider", N, "", "histmap"),
    ("Search", "Full-text search in all record types", P, "People by name only.", "fts"),
    ("Search", "Wildcards and logical operators", N, "", "fts"),
    ("Search", "Query language in lists", N, "", "gql"),
    ("Search", "Quick filters by text, tag, privacy", N, "", "listfilters"),
    ("Search", "External search on other sites", N, "", "external"),
    ("Editing", "Add, edit and delete records", N, "", "editing"),
    ("Editing", "Revision history with undo", N, "", "revisions"),
    ("Editing", "Bulk delete, merge duplicates", N, "", "bulk"),
    ("Editing", "Tags", P, "Shown on person pages.", "tags"),
    ("Editing", "Bookmarks and recent history", N, "", "bookmarks"),
    ("Content", "Blog", N, "", "blog"),
    ("Content", "Task management", N, "", "tasks"),
    ("Content", "Printable reports", N, "", "reports"),
    ("Content", "Media gallery", P, "Metadata only, no files yet.", "media"),
    ("Content", "Tag people in photos, face detection", N, "", "faces"),
    ("Content", "Text recognition (OCR)", N, "", "ocr"),
    ("Content", "Data verification", N, "", "verify"),
    ("Advanced", "DNA matches, chromosome browser, Y-DNA", N, "", "dna"),
    ("Advanced", "AI assistant", N, "", "ai"),
    ("Accounts and access", "Six roles, Guest to Admin", P, "All roles exist. Contributor and Editor wait for editing.", "editing"),
    ("Accounts and access", "Private records filtered at the database layer", D, "", None),
    ("Accounts and access", "Sign-in with a password", D, "Argon2id.", None),
    ("Accounts and access", "Create the first owner in the browser", D, "A one-time link in the server output.", None),
    ("Accounts and access", "Manage users in the browser and on the command line", D, "", None),
    ("Accounts and access", "OpenID Connect sign-in", N, "", "oidc"),
    ("Accounts and access", "Self-registration with approval", N, "", "registration"),
    ("Accounts and access", "Email for invitations and password reset", N, "Links work without email.", "email"),
    ("Accounts and access", "Several trees on one server", N, "", "multitree"),
    ("Everyday use", "Works on phones and desktops", D, "", None),
    ("Everyday use", "Installable app", P, "Web manifest only.", "pwa"),
    ("Everyday use", "Keyboard shortcuts", N, "", "shortcuts"),
    ("Everyday use", "Interface languages", P, "Russian and English. Gramps Web has more than 50.", "i18n"),
    ("Everyday use", "Background tasks and notifications", N, "", "jobs"),
    ("Everyday use", "Customisable lists", N, "", "listfilters"),
    ("Everyday use", "Calendars", P, "Julian dates display. Others convert approximately.", "calendars"),
    ("Everyday use", "Site name, colours, home text", N, "", "branding"),
]

# Things that the maintainer asked for and that Gramps Web does not have.
BEYOND = [
    ("Living people hidden by default for guests", D, None),
    ("Invitation links with a role", D, None),
    ("Strict CSP with Trusted Types, no third-party origins", D, None),
    ("Audit log", D, "audit-log"),
    ("One binary, no Python, no Redis", D, None),
    ("Security checks in CI", D, None),
    ("Passkeys and two-factor sign-in", N, "passkeys"),
    ("Password reset by an owner, session list", N, "reset"),
    ("Show how two people are related", N, "howrelated"),
    ("Encrypted read-only snapshot", N, "snapshot"),
    ("Browser tests in CI", N, "e2e"),
    ("Accessibility audit", N, "a11y"),
    ("Fuzzing of the parser", N, "fuzz"),
    ("Signed container images", N, "container"),
    ("Independent security review", N, "sec-audit"),
    ("Rich text in notes", N, "notes"),
]


def gh(*args):
    return subprocess.run(["gh", *args], check=True, capture_output=True, text=True).stdout


def create_issues():
    created = json.load(open(STATE)) if os.path.exists(STATE) else {}
    for name, (color, desc) in LABELS.items():
        subprocess.run(["gh", "label", "create", name, "--repo", REPO, "--color", color, "--description", desc, "--force"], check=True, capture_output=True)
    existing = {m["title"]: m["number"] for m in json.loads(gh("api", f"repos/{REPO}/milestones?state=all&per_page=100"))}
    for key, (title, desc) in MILESTONES.items():
        if title not in existing:
            gh("api", f"repos/{REPO}/milestones", "-f", f"title={title}", "-f", f"description={desc}")
    for key, title, milestone, labels, body in ISSUES:
        if key in created:
            continue
        text = body + "\n\nSee [the parity table](../blob/main/docs/parity.md).\n\nRemember: no real family data in issues, comments or screenshots. Use `heirfold fixture`."
        args = ["issue", "create", "--repo", REPO, "--title", title, "--body", text, "--milestone", MILESTONES[milestone][0]]
        for label in labels:
            args += ["--label", label]
        url = gh(*args).strip()
        created[key] = int(url.rsplit("/", 1)[1])
        json.dump(created, open(STATE, "w"), indent=0)
        print(key, url, flush=True)
        time.sleep(1.2)
    return created


def parity(path):
    created = json.load(open(STATE)) if os.path.exists(STATE) else {}
    def link(key):
        if not key:
            return ""
        n = created.get(key)
        return f"[#{n}](https://github.com/{REPO}/issues/{n})" if n else "planned"
    lines = ["# Parity with Gramps Web", "",
             "This table compares Heirfold with the [features of Gramps Web](https://www.grampsweb.org/features/), [its user system](https://www.grampsweb.org/install_setup/users/), [OIDC](https://www.grampsweb.org/install_setup/oidc/), [the blog](https://www.grampsweb.org/user-guide/blog/) and [sync](https://www.grampsweb.org/administration/sync/). The source pages were read on 2026-11-09. The project has not run Gramps Web itself, so the table relies on its documentation.",
             "",
             "The Issue column lists the roadmap issue of each open feature. It shows \"planned\" until the issues exist on GitHub.",
             "",
             f"**Summary:** {sum(1 for r in PARITY if r[2]==D)} of {len(PARITY)} features are done, {sum(1 for r in PARITY if r[2]==P)} are partial, and {sum(1 for r in PARITY if r[2]==N)} are planned.",
             "", "## Features of Gramps Web", ""]
    group = None
    for g, feature, status, note, key in PARITY:
        if g != group:
            if group is not None:
                lines.append("")
            lines += [f"### {g}", "", "| Feature | Status | Note | Issue |", "| --- | --- | --- | --- |"]
            group = g
        lines.append(f"| {feature} | {status} | {note} | {link(key)} |")
    lines += ["", "## Beyond Gramps Web", "", "| Feature | Status | Issue |", "| --- | --- | --- |"]
    for feature, status, key in BEYOND:
        lines.append(f"| {feature} | {status} | {link(key)} |")
    lines.append("")
    open(path, "w", encoding="utf8").write("\n".join(lines))


if __name__ == "__main__":
    if "--issues" in sys.argv:
        print(len(create_issues()), "issues")
    if "--parity" in sys.argv:
        parity(sys.argv[sys.argv.index("--parity") + 1])
