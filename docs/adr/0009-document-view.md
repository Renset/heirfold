# ADR 0009: Show PDF, Word and text files in the viewer

Status: accepted, 2026-11-10.

## Context

A family tree holds more than photos: scans of records as PDF, research reports as Word files, lists as text. A viewer that shows only pictures offers every other file as a download. On a phone a download of a Word file often opens nothing. So the viewer must show Word, PDF and text files itself.

The Content Security Policy of Heirfold is strict on purpose ([SECURITY.md](../../SECURITY.md)): `default-src 'none'`, scripts and styles only from the server, Trusted Types with no policy, no frames. A media file is content that an editor adds, so the viewer must not let a file run script or place markup on the page.

## Decision

**Word and text files: the server reads the file and sends data.** The package `internal/docview` reads a `.docx` file with the standard library. It keeps the text, the headings, the lists with their numbers, the tables, the links, the footnotes and the endnotes. It also keeps the colors: the color of the text, the marker color, the background of a paragraph and of a table cell, and the size of the text relative to the body text, from the styles of the file and from the text itself. It drops the pictures, the fonts and the page layout, and it says in the result that the file has pictures. The route `GET /api/v1/media/{id}/document` sends this as JSON. The page builds its own elements from the data. A plain text file goes the same way; the reader accepts UTF-8, UTF-16 and Windows-1251.

- The reader has limits: 32 MB for one XML part after decompression, 2 MB of text, 20 000 blocks, three levels of tables. A longer file shows its start and a note.
- A link keeps its address only if it starts with `http:`, `https:` or `mailto:`.
- A color leaves the server only as six hexadecimal digits, and the page checks the form again before it sets the color through the CSSOM. The page shows a Word file on a light sheet in both themes, because the colors of the file expect white paper.
- The access rules are those of the file download: the same check of the role, of living people and of the media directory.

**PDF files: PDF.js draws the pages on canvases.** PDF.js (Apache-2.0) is the reader that Firefox uses. A native PDF frame of the browser was the other choice. It needs `frame-src`, it does not work in Chrome on Android, and on an iPhone it shows the first page only.

- PDF.js loads as a separate file of about 1.7 MB (about 0.5 MB compressed), and only when a PDF opens.
- The policy permits no worker threads and no WebAssembly, and it stays so. PDF.js runs on the page itself (its main-thread mode). The option `useWasm: false` makes it use its JavaScript decoders. The two decoders for scanned pages (JBIG2 and CCITT fax, JPEG 2000) are copied from the package into `assets/pdfjs-VERSION/` at build time.
- The viewer draws only the pages near the screen and frees the others. The canvas of a page has a limit of 12 million pixels, which every phone accepts.
- The viewer draws no text layer and no form fields. So text in a PDF cannot be selected, and a PDF cannot place elements on the page.
- The version of `pdfjs-dist` is exact in `package.json`. Dependabot proposes updates.

**Other files stay downloads.** Old Word files (`.doc`), OpenDocument, spreadsheets and the like show a card with a download button.

## Consequences

- The binary grows by about 2.4 MB.
- A long PDF with heavy pages makes the page slow for a moment, because the decoding runs on the page and not on a worker thread. This is the price of the strict policy.
- A PDF with a password, a broken PDF and a Word file that the reader cannot read show the download card.
- The view of a Word file is a reading view with the colors of the file. It does not look like the printed page.
- The static demo has no server and no document view.
