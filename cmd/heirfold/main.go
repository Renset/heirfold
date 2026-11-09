// Command heirfold is a self-hosted family tree server in one binary.
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/faces"
	"github.com/Renset/heirfold/internal/fixture"
	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/grampsdb"
	"github.com/Renset/heirfold/internal/mail"
	"github.com/Renset/heirfold/internal/server"
	"github.com/Renset/heirfold/internal/snapshot"
	"github.com/Renset/heirfold/internal/store"
	"github.com/Renset/heirfold/internal/treesync"
)

// version is set at build time.
var version = "dev"

const usage = `Heirfold is a self-hosted family tree server.

Usage:
  heirfold serve       Start the server
  heirfold import      Import a .gramps file or a Gramps tree
  heirfold gramps-list List the Gramps trees on this computer
  heirfold export      Export the tree as a .gramps file
  heirfold user-add    Add a user or invite the first administrator
  heirfold user-list   List the users
  heirfold user-email  Change the e-mail address of a user
  heirfold fixture     Write a tree of invented people to a .gramps file
  heirfold snapshot    Write a static read-only JSON bundle
  heirfold healthcheck Check a running server
  heirfold version     Print the version

Run "heirfold <command> -h" for the options of a command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "serve":
		err = runServe(ctx, args)
	case "import":
		err = runImport(ctx, args)
	case "gramps-list":
		err = runGrampsList()
	case "export":
		err = runExport(ctx, args)
	case "user-add":
		err = runUserAdd(ctx, args)
	case "user-list":
		err = runUserList(ctx, args)
	case "user-email":
		err = runUserEmail(ctx, args)
	case "fixture":
		err = runFixture(args)
	case "snapshot":
		err = runSnapshot(ctx, args)
	case "healthcheck":
		err = runHealthcheck(args)
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func envBool(name string) bool {
	value := strings.ToLower(os.Getenv(name))
	return value == "1" || value == "true" || value == "yes"
}

func defaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "heirfold-data"
	}
	return filepath.Join(dir, "Heirfold")
}

func dbPath(dataDir string) string {
	return filepath.Join(dataDir, "heirfold.db")
}

func dataFlag(fs *flag.FlagSet) *string {
	return fs.String("data", env("HEIRFOLD_DATA", defaultDataDir()), "directory for the database (HEIRFOLD_DATA)")
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && addr.IsLoopback()
}

func parseProxies(text string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(part); err == nil {
			out = append(out, prefix)
			continue
		}
		addr, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("bad proxy address %q", part)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", env("HEIRFOLD_LISTEN", "127.0.0.1:8080"), "address and port (HEIRFOLD_LISTEN)")
	data := dataFlag(fs)
	certFile := fs.String("tls-cert", env("HEIRFOLD_TLS_CERT", ""), "TLS certificate file (HEIRFOLD_TLS_CERT)")
	keyFile := fs.String("tls-key", env("HEIRFOLD_TLS_KEY", ""), "TLS key file (HEIRFOLD_TLS_KEY)")
	behindProxy := fs.Bool("behind-https-proxy", envBool("HEIRFOLD_BEHIND_HTTPS_PROXY"), "a reverse proxy handles HTTPS (HEIRFOLD_BEHIND_HTTPS_PROXY)")
	insecure := fs.Bool("insecure-http", envBool("HEIRFOLD_INSECURE_HTTP"), "permit plain HTTP on a non-loopback address; sessions are not protected (HEIRFOLD_INSECURE_HTTP)")
	publicHost := fs.String("public-host", env("HEIRFOLD_PUBLIC_HOST", ""), "the only host name to accept, for example family.example.org (HEIRFOLD_PUBLIC_HOST)")
	proxies := fs.String("trusted-proxies", env("HEIRFOLD_TRUSTED_PROXIES", ""), "comma-separated proxy addresses whose X-Forwarded-For to trust (HEIRFOLD_TRUSTED_PROXIES)")
	grampsPath := fs.String("gramps", env("HEIRFOLD_GRAMPS", ""), "Gramps tree to follow: its directory or its sqlite.db file; Heirfold only reads it (HEIRFOLD_GRAMPS)")
	grampsInterval := fs.Duration("gramps-interval", 3*time.Second, "time between two checks of the Gramps tree")
	dropEdits := fs.Bool("gramps-replace-edits", false, "with --gramps: let the Gramps tree replace a tree that people edited in Heirfold; a copy is saved first")
	mediaDir := fs.String("media-dir", env("HEIRFOLD_MEDIA_DIR", ""), "directory with the photos and scans; only files inside it are shown (HEIRFOLD_MEDIA_DIR)")
	noFaces := fs.Bool("no-faces", envBool("HEIRFOLD_NO_FACES"), "do not search the pictures for faces (HEIRFOLD_NO_FACES)")
	mapTiles := fs.String("map-tiles", env("HEIRFOLD_MAP_TILES", ""), "address of a tile service for a map with streets, such as https://tile.openstreetmap.org/{z}/{x}/{y}.png; that service then sees which parts of the world the family looks at (HEIRFOLD_MAP_TILES)")
	mapCredit := fs.String("map-credit", env("HEIRFOLD_MAP_CREDIT", "© OpenStreetMap contributors"), "the line under a map that names the makers of the tiles (HEIRFOLD_MAP_CREDIT)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return fmt.Errorf("bad listen address: %w", err)
	}
	loopback := isLoopback(host)
	useTLS := *certFile != "" && *keyFile != ""
	if !loopback && !useTLS && !*behindProxy && !*insecure {
		return errors.New("plain HTTP on a public address exposes sign-in codes and sessions: use --tls-cert and --tls-key, or --behind-https-proxy, or --insecure-http")
	}
	trusted, err := parseProxies(*proxies)
	if err != nil {
		return err
	}
	if *behindProxy && len(trusted) == 0 {
		fmt.Fprintln(os.Stderr, "WARNING: --behind-https-proxy without --trusted-proxies: every client looks like the proxy, so one client can use up the sign-in limits of all. Name the proxy with --trusted-proxies.")
	}
	secure := useTLS || *behindProxy
	cfg := server.Config{DataDir: *data, SecureCookies: secure, HSTS: secure, TrustedProxies: trusted, Version: version}
	switch {
	case *publicHost != "":
		cfg.AllowedHosts = []string{*publicHost}
	case loopback:
		cfg.AllowedHosts = []string{"localhost", "127.0.0.1", "::1"}
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := store.Open(ctx, dbPath(*data))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	if err := st.RefreshLiving(ctx); err != nil {
		return err
	}
	if err := st.DeleteExpired(ctx); err != nil {
		return err
	}
	var syncer *treesync.Syncer
	if *grampsPath != "" {
		// Gramps becomes the main copy, and its tree replaces the tree here.
		// Edits that people made in Heirfold are not in Gramps.
		edits, err := st.CountChanges(ctx)
		if err != nil {
			return err
		}
		if edits > 0 {
			if !*dropEdits {
				return fmt.Errorf("this tree has %d edits that people made in Heirfold, and Gramps does not have them: "+
					"run \"heirfold export\" and import the file into Gramps first, or start with --gramps-replace-edits", edits)
			}
			path, err := st.Backup(ctx, filepath.Join(*data, "backups"))
			if err != nil {
				return err
			}
			fmt.Printf("The tree with the edits from Heirfold is saved in %s\n", path)
		}
		syncer, err = treesync.New(st, treesync.Options{Path: *grampsPath, Interval: *grampsInterval}, log)
		if err != nil {
			return err
		}
		if _, err := syncer.Sync(ctx, false); err != nil {
			// Gramps can be in the middle of a change. The next check tries again.
			log.Warn("first sync from Gramps failed", "error", err)
		}
		cfg.Sync = syncer
	}
	cfg.MediaDir = *mediaDir
	cfg.NoFaces = *noFaces
	if *mapTiles != "" {
		if err := server.CheckMapTiles(*mapTiles); err != nil {
			return err
		}
		cfg.MapTiles, cfg.MapCredit = *mapTiles, *mapCredit
	}
	if cfg.MediaDir == "" && syncer != nil {
		// The administrator named this tree, so its own media path is trusted.
		cfg.MediaDir = treeMediaDir(ctx, st)
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	base := scheme + "://" + *listen
	if *publicHost != "" {
		base = scheme + "://" + *publicHost
	}
	cfg.BaseURL = base
	cfg.Mail, err = mailFromEnv(log)
	if err != nil {
		return err
	}
	if cfg.MediaDir == "" && syncer == nil {
		// Heirfold keeps the main copy, so it also keeps the pictures that
		// editors add.
		cfg.MediaDir = filepath.Join(*data, "media")
		if err := os.MkdirAll(cfg.MediaDir, 0o700); err != nil {
			return err
		}
	}
	app, err := server.New(cfg, st, log)
	if err != nil {
		return err
	}
	if syncer != nil {
		go syncer.Run(ctx)
	}
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	go app.Run(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()

	fmt.Printf("Heirfold %s\nData directory: %s\nAddress: %s\n", version, *data, base)
	if syncer != nil {
		fmt.Printf("Gramps tree: %s (read only, checked every %s)\n", syncer.File(), *grampsInterval)
	}
	switch cfg.Mail.Name() {
	case "log":
		fmt.Println("Mail: not set, so each sign-in code goes to this log (HEIRFOLD_SMTP_HOST or HEIRFOLD_RESEND_KEY)")
		if !loopback {
			fmt.Println("WARNING: without mail, only a person who reads this log can sign in.")
		}
	default:
		fmt.Printf("Mail: %s\n", cfg.Mail.Name())
	}
	if cfg.MapTiles != "" {
		fmt.Printf("Map: tiles from %s. That service sees which parts of the world the family looks at.\n", cfg.MapTiles)
	} else {
		fmt.Println("Map: the map of the program. The server asks no map service (--map-tiles adds streets)")
	}
	if cfg.MediaDir != "" {
		fmt.Printf("Media directory: %s\n", cfg.MediaDir)
		switch model := filepath.Join(*data, "models", faces.RecognizerFile); {
		case cfg.NoFaces:
			fmt.Println("Faces: off (--no-faces)")
		case syncer != nil:
			fmt.Println("Faces: off, because the tree follows Gramps and takes no marks")
		default:
			if _, err := os.Stat(model); err == nil {
				fmt.Println("Faces: the server finds faces on the photos and says who a face is like")
			} else {
				fmt.Printf("Faces: the server finds faces on the photos. To let it say who a face is like, put the model file here: %s\n", model)
			}
		}
	} else {
		fmt.Println("Media directory: not set, so photos and scans are off (--media-dir)")
	}
	needsSetup, err := app.NeedsSetup(ctx)
	if err != nil {
		return err
	}
	if needsSetup {
		fmt.Printf("\nFirst run. Open this link to create the administrator account:\n  %s/#/setup/%s\nThe link works only until the first account exists.\n\n", base, app.SetupToken())
	}
	if useTLS {
		err = httpServer.ListenAndServeTLS(*certFile, *keyFile)
	} else {
		err = httpServer.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	data := dataFlag(fs)
	replace := fs.Bool("replace", false, "replace an existing tree")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: heirfold import [--data DIR] [--replace] FILE-OR-GRAMPS-TREE")
	}
	result, err := readTree(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, dbPath(*data))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	if has, err := st.HasTree(ctx); err != nil {
		return err
	} else if has && !*replace {
		return errors.New("a tree already exists; use --replace")
	}
	if has, err := st.HasTree(ctx); err != nil {
		return err
	} else if has {
		path, err := st.Backup(ctx, filepath.Join(*data, "backups"))
		if err != nil {
			return err
		}
		fmt.Printf("The old tree is saved in %s\n", path)
	}
	living, err := st.ReplaceTree(ctx, result.Tree)
	if err != nil {
		return err
	}
	if err := st.AddAudit(ctx, 0, "cli", "import.commit", "", fmt.Sprintf("people=%d", len(result.Tree.People)), ""); err != nil {
		return err
	}
	counts, err := json.Marshal(result.Tree.Counts())
	if err != nil {
		return err
	}
	fmt.Printf("Imported: %s\nTreated as living: %d\nWarnings: %d\n", counts, living, result.WarningsTotal)
	return nil
}

// readTree reads a .gramps file, or a Gramps tree directory or database.
func readTree(ctx context.Context, path string) (*gramps.Result, error) {
	info, err := os.Stat(path) // #nosec G703 -- the operator gives this path on the command line.
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return grampsdb.Read(ctx, path, gramps.DefaultLimits)
	}
	file, err := os.Open(path) // #nosec G304,G703 -- the operator gives this path on the command line.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	magic := make([]byte, 16)
	if n, _ := io.ReadFull(file, magic); n == len(magic) && string(magic) == "SQLite format 3\x00" {
		return grampsdb.Read(ctx, path, gramps.DefaultLimits)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return gramps.Parse(file, gramps.DefaultLimits)
}

// treeMediaDir returns the media path that the tree itself names, if that
// directory exists.
func treeMediaDir(ctx context.Context, st *store.Store) string {
	path, err := st.MediaPath(ctx)
	if err != nil || path == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(path, "~"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		return ""
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return ""
	}
	return path
}

// grampsHomes lists the directories where Gramps keeps its trees.
func grampsHomes() []string {
	var out []string
	add := func(parts ...string) {
		for _, part := range parts {
			if part == "" {
				return
			}
		}
		out = append(out, filepath.Join(append(parts, "gramps", "grampsdb")...))
	}
	add(os.Getenv("GRAMPSHOME"))
	if dir, err := os.UserConfigDir(); err == nil {
		add(dir)
	}
	add(os.Getenv("XDG_DATA_HOME"))
	if home, err := os.UserHomeDir(); err == nil {
		add(home, ".local", "share")
		out = append(out, filepath.Join(home, ".gramps", "grampsdb"))
	}
	return out
}

func runGrampsList() error {
	seen := map[string]bool{}
	found := 0
	for _, home := range grampsHomes() {
		entries, err := os.ReadDir(home)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			dir := filepath.Join(home, entry.Name())
			if !entry.IsDir() || seen[dir] {
				continue
			}
			seen[dir] = true
			note := ""
			if _, err := grampsdb.Locate(dir); err != nil {
				note = "  (not readable: Heirfold needs the SQLite backend of Gramps 6)"
			}
			name := grampsdb.TreeName(filepath.Join(dir, "sqlite.db"))
			if name == "" {
				name = entry.Name()
			}
			fmt.Printf("%s\n  %s%s\n", name, dir, note)
			found++
		}
	}
	if found == 0 {
		fmt.Println("No Gramps trees found. Give the path of a tree to --gramps.")
	}
	return nil
}

func runExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	data := dataFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: heirfold export [--data DIR] FILE")
	}
	st, err := store.Open(ctx, dbPath(*data))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	tree, err := st.ExportTree(ctx)
	if err != nil {
		return err
	}
	return writeGramps(fs.Arg(0), func(w io.Writer) error { return gramps.Write(w, tree) })
}

func writeGramps(path string, write func(io.Writer) error) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- the operator gives this path on the command line.
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(file)
	if err := write(gz); err != nil {
		_ = gz.Close()
		_ = file.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// secretFromEnv reads a secret from the variable, or from the file that the
// variable with the suffix _FILE names. A secret never comes from a flag,
// because the process list shows flags.
func secretFromEnv(name string) (string, error) {
	if value := os.Getenv(name); value != "" {
		return value, nil
	}
	path := os.Getenv(name + "_FILE")
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path) // #nosec G304,G703 -- the operator names this file.
	if err != nil {
		return "", fmt.Errorf("%s_FILE: %w", name, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// mailFromEnv builds the transport for letters from the environment.
func mailFromEnv(log *slog.Logger) (mail.Sender, error) {
	key, err := secretFromEnv("HEIRFOLD_RESEND_KEY")
	if err != nil {
		return nil, err
	}
	host := os.Getenv("HEIRFOLD_SMTP_HOST")
	if host == "" && key == "" {
		return mail.Log{Logger: log}, nil
	}
	from, err := mail.ParseFrom(os.Getenv("HEIRFOLD_MAIL_FROM"))
	if err != nil {
		return nil, fmt.Errorf("HEIRFOLD_MAIL_FROM must hold the sender of the letters, for example \"Family tree <tree@example.org>\": %w", err)
	}
	if host == "" {
		return &mail.Resend{Key: key, From: from}, nil
	}
	password, err := secretFromEnv("HEIRFOLD_SMTP_PASSWORD")
	if err != nil {
		return nil, err
	}
	port := env("HEIRFOLD_SMTP_PORT", "465")
	return &mail.SMTP{
		Host: host, Port: port, User: os.Getenv("HEIRFOLD_SMTP_USER"), Password: password, From: from,
		// Port 465 starts with TLS. Other ports upgrade with STARTTLS.
		ImplicitTLS: port == "465",
	}, nil
}

func runUserAdd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("user-add", flag.ContinueOnError)
	data := dataFlag(fs)
	address := fs.String("email", "", "e-mail address; the person signs in with a code that Heirfold sends there")
	name := fs.String("name", "", "display name")
	roleName := fs.String("role", "guest", "guest, member, editor or admin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	role, ok := auth.ParseRole(*roleName)
	email := auth.NormalizeEmail(*address)
	if !ok || role < auth.RoleGuest || !auth.ValidEmail(email) {
		return errors.New("usage: heirfold user-add --email ADDRESS [--name NAME] [--role guest|member|editor|admin]")
	}
	st, err := store.Open(ctx, dbPath(*data))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	display := *name
	if display == "" {
		display, _, _ = strings.Cut(email, "@")
	}
	user, err := st.CreateUser(ctx, email, display, role, "", 0)
	if err != nil {
		return err
	}
	if err := st.AddAudit(ctx, 0, "cli", "user.add", fmt.Sprint(user.ID), user.RoleName, ""); err != nil {
		return err
	}
	fmt.Printf("Added user %d with the role %s. The person signs in with a code from e-mail.\n", user.ID, user.RoleName)
	return nil
}

func runUserList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("user-list", flag.ContinueOnError)
	data := dataFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(ctx, dbPath(*data))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	users, err := st.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		state := ""
		if user.LastLoginAt == nil {
			state = "  (has not signed in)"
		}
		fmt.Printf("%d\t%s\t%s\t%s%s\n", user.ID, user.RoleName, user.Email, user.DisplayName, state)
	}
	return nil
}

func runUserEmail(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("user-email", flag.ContinueOnError)
	data := dataFlag(fs)
	id := fs.Int64("id", 0, "number of the user, from heirfold user-list")
	address := fs.String("email", "", "new e-mail address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	email := auth.NormalizeEmail(*address)
	if *id <= 0 || !auth.ValidEmail(email) {
		return errors.New("usage: heirfold user-email --id NUMBER --email ADDRESS")
	}
	st, err := store.Open(ctx, dbPath(*data))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	user, err := st.SetUserEmail(ctx, *id, email)
	if err != nil {
		return err
	}
	if err := st.AddAudit(ctx, 0, "cli", "user.email", fmt.Sprint(user.ID), "", ""); err != nil {
		return err
	}
	fmt.Printf("Changed the address of user %d. All sessions of this user ended.\n", user.ID)
	return nil
}

func runFixture(args []string) error {
	fs := flag.NewFlagSet("fixture", flag.ContinueOnError)
	locale := fs.String("locale", "en", "language of the invented names: ru or en")
	out := fs.String("out", "", "output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("usage: heirfold fixture --out FILE [--locale ru|en]")
	}
	tree := fixture.Generate(fixture.DefaultOptions(*locale))
	return writeGramps(*out, func(w io.Writer) error { return gramps.Write(w, tree) })
}

func runSnapshot(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	data := dataFlag(fs)
	roleName := fs.String("role", "guest", "guest or member")
	out := fs.String("out", "", "output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	role, ok := auth.ParseRole(*roleName)
	if !ok || *out == "" {
		return errors.New("usage: heirfold snapshot --out FILE [--role guest|member]")
	}
	st, err := store.Open(ctx, dbPath(*data))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	bundle, err := snapshot.Build(ctx, st.View(role.Scope()), role.String())
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	return os.WriteFile(*out, encoded, 0o600)
}

func runHealthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	url := fs.String("url", env("HEIRFOLD_HEALTH_URL", "http://127.0.0.1:8080/healthz"), "address of the health endpoint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, *url, http.NoBody)
	if err != nil {
		return err
	}
	response, err := client.Do(request) // #nosec G107,G704 -- the operator gives this address.
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", response.StatusCode)
	}
	return nil
}
