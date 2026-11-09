# Deployment

Choose one model. The reasons are in [ADR 0004](adr/0004-deployment.md).

| Model | Use it when | Status |
| --- | --- | --- |
| [Personal computer](#personal-computer) | You try Heirfold, or only you use it | Works |
| [Own server](#own-server) | Relatives open it from anywhere | Works |
| [Cloudflare Tunnel and Access](#cloudflare-tunnel-and-access) | Your server has no public address and no relative lives in Russia | Not tested |
| [Encrypted snapshot](#encrypted-snapshot) | You want free static hosting and read-only access | Planned |

## Personal computer

1. Download a binary from the releases page, or run `make build`.
2. Start the server:

   ```bash
   ./heirfold serve
   ```

3. Open the first-run link that the program prints. Create the administrator account.
4. Open **Settings**, then **Import and export**, and upload a `.gramps` file.

The server listens on `127.0.0.1:8080`. Only this computer can reach it.

### Follow a Gramps tree

Heirfold can read the database of Gramps 6 directly. You then need no export file.

1. List the trees on this computer:

   ```bash
   ./heirfold gramps-list
   ```

2. Start the server with the path of a tree and the directory of its photos:

   ```bash
   ./heirfold serve --gramps "/path/to/grampsdb/TREE" --media-dir "/path/to/photos"
   ```

Heirfold opens the database read-only and never changes it. In this mode Gramps holds the main copy, and editing in Heirfold is off. Edit the tree in Gramps. A change appears in the browser in a few seconds, and Gramps can stay open. The details are in [ADR 0006](adr/0006-gramps-database.md).

- Heirfold needs the SQLite backend of Gramps 6.0 or later. Open an older tree in Gramps 6 once to upgrade it.
- Heirfold shows only the files inside `--media-dir`. Without the option it uses the media path of the tree. If the tree has no media path, photos and scans are off.
- On macOS a service that the system starts has no access to the Documents folder. Start the server from a terminal, or give the program access in the system settings.
- The option `--media-dir` also works for a tree that you imported from a file.

## Letters with sign-in codes

A person signs in with a code that Heirfold sends to their e-mail address. A server for a family needs one of these two settings. Without them, each code goes to the server log.

| Variable | Meaning |
| --- | --- |
| `HEIRFOLD_MAIL_FROM` | The sender, for example `Family tree <tree@example.org>` |
| `HEIRFOLD_SMTP_HOST`, `HEIRFOLD_SMTP_PORT` | An SMTP server. Port 465 (the default) starts with TLS. Other ports use STARTTLS. |
| `HEIRFOLD_SMTP_USER`, `HEIRFOLD_SMTP_PASSWORD` | The account of the SMTP server |
| `HEIRFOLD_RESEND_KEY` | A key of Resend. Use it when the host blocks the SMTP ports. |

- Each secret can come from a file: set `HEIRFOLD_SMTP_PASSWORD_FILE` or `HEIRFOLD_RESEND_KEY_FILE` to its path. Do not write a secret into a compose file in a repository.
- Any mail service with SMTP works, also a normal mailbox with an app password. Several services have a free plan that is enough for a family.
- Heirfold refuses an SMTP connection without TLS, except to a server on the same computer.
- Set `--public-host`, so that the letters carry the right address of the site.

## Who edits the tree

One copy of the tree is the main copy ([ADR 0008](adr/0008-editing.md)).

- With `--gramps`, Gramps holds the main copy. Heirfold shows it and changes nothing.
- Without `--gramps`, Heirfold holds the main copy. Tree editors change it in the browser. To work on it in Gramps, export it in **Settings**, **Import and export** and import the file into Gramps.
- Heirfold refuses to start with `--gramps` while its tree has edits from the browser. Export them first, or give `--gramps-replace-edits`. Heirfold then saves the old tree in `backups` in the data directory.
- Pictures that editors add go to `media/heirfold` in the data directory, or to `heirfold` inside `--media-dir`.
- The server searches the pictures for faces, in the background. To let it also say who a face is like, run `make models` and copy `internal/faces/models/face_recognition_sface_2021dec.onnx` into the folder `models` of the data directory, then start the server again. The start-up text says what is on. `--no-faces` turns the search off.
- The map of the program has countries, rivers, main roads and settlements, and the server asks no map service. For a map with streets, name a tile service: `--map-tiles 'https://tile.openstreetmap.org/{z}/{x}/{y}.png'` or `HEIRFOLD_MAP_TILES`. The server then gets the pictures of the map from that service, keeps them in `cache/tiles` in the data directory (at most 1 GB), and gives them to family members. Know the price: the service sees the address of your server and which parts of the world your family looks at. It never sees a relative. Read the rules of the service first; OpenStreetMap asks for light use, and its tiles need the line `© OpenStreetMap contributors` under the map, which `--map-credit` sets.

Commands for accounts:

```bash
./heirfold user-list
```

```bash
./heirfold user-add --email aunt@example.org --role member
```

```bash
./heirfold user-email --id 1 --email keeper@example.org
```

## Own server

CAUTION: Heirfold stops if it listens on a public address with plain HTTP. Do not override this with `--insecure-http` on the internet. Sign-in codes and sessions would travel in clear text.

### Docker Compose

1. Point a DNS record at the server.
2. Open the folder `deploy`.
3. Set your host name:

   ```bash
   export HEIRFOLD_PUBLIC_HOST=family.example.org
   ```

4. Start the services:

   ```bash
   docker compose up -d --build
   ```

5. Read the first-run link:

   ```bash
   docker compose logs heirfold
   ```

### systemd

Use [deploy/heirfold.service](../deploy/heirfold.service). The file holds the steps. Put a reverse proxy with TLS, for example Caddy, in front of it.

### Choosing a host

Relatives in several countries must all reach the server. Test a candidate from each country before you move the family there. On 2026-11-09 these facts applied:

- Cloudflare is throttled in Russia.
- Reports describe failures at OVH, Hetzner and DigitalOcean from Russia.

Heirfold loads nothing from a CDN, so you can change the host and the domain without changing the code.

## Backups

The whole tree and all accounts are in one file, `heirfold.db`, in the data directory.

1. Export a Gramps file on a schedule:

   ```bash
   heirfold export --data /var/lib/heirfold /backup/family.gramps
   ```

2. Copy the export to a second place, and encrypt it there.

CAUTION: An export holds private records and living people. Store it as carefully as the database.

## Cloudflare Tunnel and Access

Cloudflare Tunnel connects a server with no public address. Cloudflare Access adds a login in front of it.

CAUTION: Do not use this model if relatives live in Russia. Cloudflare is throttled there.

1. Start Heirfold with `--behind-https-proxy` and `--public-host family.example.org`.
2. Create a tunnel in the Cloudflare dashboard.
3. Send `family.example.org` to `http://127.0.0.1:8080`.
4. Create an Access application for the same host, with a policy that lists your relatives.

The project has not tested this setup yet.

## Encrypted snapshot

This model is planned. See the roadmap issue. It will publish one encrypted file and a static page that decrypts it in the browser.

CAUTION: Never publish a snapshot without encryption. GitHub Pages is public.

## Security checklist

1. Use HTTPS.
2. Keep the data directory readable only by the service account.
3. Give new relatives the Guest role until you decide otherwise.
4. Point `--media-dir` at a directory that holds only the photos of the tree. Every member can download every file that the tree names inside it.
5. Encrypt backups.
6. Install new releases.
