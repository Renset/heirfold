# Contributing

Thank you for your interest in Heirfold.

## The most important rule

**Never put real family data in this project.** This covers code, tests, issues, pull requests, screenshots, logs and CI output. Use `heirfold fixture` to create invented people. CI fails if a family-tree file enters the history.

## Set up

You need Go (the version in `go.mod`), Node.js (the version in `web/.node-version`) and `make`.

1. Build the binary:

   ```bash
   make build
   ```

2. Write a test tree of invented people:

   ```bash
   ./heirfold fixture --locale en --out /tmp/demo.gramps
   ```

3. Import the tree into a local data directory:

   ```bash
   ./heirfold import --data /tmp/heirfold-dev /tmp/demo.gramps
   ```

4. Start the server. It prints a link for the first account.

   ```bash
   ./heirfold serve --data /tmp/heirfold-dev
   ```

For frontend work, run `npm run watch` in `web/` and rebuild the Go binary when you need the embedded files.

## Before you open a pull request

Run these commands:

```bash
make test
```

```bash
make check
```

Keep each pull request to one change. Add a test for each behaviour change.

## Rules for code

- Every read of family data for a viewer goes through `store.View`. A test must cover each new field that can hold private data.
- Log route patterns and error classes. Never log names, IDs of people, tokens or passwords.
- Do not use `dangerouslySetInnerHTML`, `innerHTML`, `eval` or inline styles and scripts.
- Do not load anything from another origin.
- Explain each new dependency in the pull request.
- Pin each GitHub Action by commit SHA, and give each job the smallest token permissions.
- Do not copy code from another project without naming the source and its license in the pull request. See `docs/adr/0005-license.md`.

## Translations

Interface strings live in `web/src/i18n`. Each language must have every key. The TypeScript compiler checks this.

## License

By contributing, you agree that your contribution is licensed under AGPL-3.0-or-later.
