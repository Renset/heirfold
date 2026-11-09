import type { ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { mediaDocument } from "../api/client";
import type { DocBlock, DocRun, DocumentView } from "../api/types";
import { t } from "../i18n";

// The server sends only these kinds of links. The page checks them again.
const safeLink = /^(https?:|mailto:)/i;
const hexColor = /^#[0-9A-F]{6}$/;

/** A color of the file, if it has the form that the server sends. */
function color(value?: string): string | undefined {
  return value && hexColor.test(value) ? value : undefined;
}

/** The background of a block or a cell. */
function fillStyle(value?: string): { backgroundColor: string } | undefined {
  const fill = color(value);
  return fill ? { backgroundColor: fill } : undefined;
}

function RunView({ run, onNote }: { run: DocRun; onNote: (number: number) => void }): ComponentChildren {
  if (run.note) {
    const number = run.note;
    return (
      <sup>
        <button
          type="button"
          class="doc-ref"
          aria-label={t("docNoteLink", { number })}
          onClick={() => onNote(number)}
        >
          {number}
        </button>
      </sup>
    );
  }
  let node: ComponentChildren = run.text ?? "";
  if (run.script === "sup") {
    node = <sup>{node}</sup>;
  } else if (run.script === "sub") {
    node = <sub>{node}</sub>;
  }
  if (run.link && safeLink.test(run.link)) {
    node = (
      <a href={run.link} target="_blank" rel="noopener noreferrer">
        {node}
      </a>
    );
  }
  const names = [run.bold && "b", run.italic && "i", run.underline && "u", run.strike && "s"].filter(Boolean);
  const style: { color?: string; backgroundColor?: string; fontSize?: string } = {};
  const text = color(run.color);
  const fill = color(run.fill);
  if (text) {
    style.color = text;
  }
  if (fill) {
    style.backgroundColor = fill;
    names.push("mark");
  }
  if (run.scale && run.scale >= 0.5 && run.scale <= 3) {
    style.fontSize = `${run.scale}em`;
  }
  const styled = Object.keys(style).length > 0;
  return names.length > 0 || styled ? (
    <span class={names.join(" ") || undefined} style={styled ? style : undefined}>
      {node}
    </span>
  ) : (
    node
  );
}

function Runs({ runs, onNote }: { runs?: DocRun[]; onNote: (number: number) => void }) {
  return (
    <>
      {(runs ?? []).map((run, index) => (
        <RunView key={index} run={run} onNote={onNote} />
      ))}
    </>
  );
}

function Blocks({ blocks, onNote }: { blocks: DocBlock[]; onNote: (number: number) => void }) {
  return (
    <>
      {blocks.map((block, index) => (
        <BlockView key={index} block={block} onNote={onNote} />
      ))}
    </>
  );
}

function BlockView({ block, onNote }: { block: DocBlock; onNote: (number: number) => void }) {
  const filled = fillStyle(block.fill);
  const names = [block.align && `doc-${block.align}`, filled && "doc-filled"].filter(Boolean).join(" ");
  const align = names || undefined;
  switch (block.kind) {
    case "h": {
      // The viewer has its own heading, so the headings of the file start one step lower.
      const level = Math.min(Math.max(block.level ?? 1, 1), 4);
      const Tag = `h${level + 2}` as "h3" | "h4" | "h5" | "h6";
      return (
        <Tag class={align} style={filled}>
          <Runs runs={block.runs} onNote={onNote} />
        </Tag>
      );
    }
    case "item":
      return (
        <p
          class={filled ? "doc-item doc-filled" : "doc-item"}
          style={filled}
          data-level={Math.min(Math.max(block.level ?? 0, 0), 8)}
        >
          <span class="doc-marker">{block.marker}</span>
          <span>
            <Runs runs={block.runs} onNote={onNote} />
          </span>
        </p>
      );
    case "table":
      return (
        <div class="doc-table">
          <table>
            <tbody>
              {(block.rows ?? []).map((row, r) => (
                <tr key={r}>
                  {row.cells.map((cell, c) => (
                    <td
                      key={c}
                      colSpan={cell.span && cell.span > 1 ? cell.span : undefined}
                      style={fillStyle(cell.fill)}
                    >
                      <Blocks blocks={cell.blocks} onNote={onNote} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "text":
      return <pre class="doc-pre">{block.runs?.[0]?.text ?? ""}</pre>;
    default:
      return block.runs && block.runs.length > 0 ? (
        <p class={align} style={filled}>
          <Runs runs={block.runs} onNote={onNote} />
        </p>
      ) : (
        <p class="doc-gap" />
      );
  }
}

/** Shows the text of a Word or text file. The server reads the file. */
export function DocView({ id, label, onFail }: { id: string; label: string; onFail: () => void }) {
  const [doc, setDoc] = useState<DocumentView | null>(null);
  const frame = useRef<HTMLDivElement>(null);
  const fail = useRef(onFail);
  fail.current = onFail;

  useEffect(() => {
    const control = new AbortController();
    setDoc(null);
    mediaDocument(id, control.signal)
      .then(setDoc)
      .catch(() => {
        if (!control.signal.aborted) {
          fail.current();
        }
      });
    return () => control.abort();
  }, [id]);

  if (!doc) {
    return <div class="spinner" role="status" aria-label={t("loading")} />;
  }
  const toNote = (number: number): void => {
    frame.current?.querySelector(`[data-note="${number}"]`)?.scrollIntoView({ block: "center" });
  };
  const notes = doc.notes ?? [];
  return (
    // biome-ignore lint/a11y/noNoninteractiveTabindex: the keyboard scrolls the text
    <div class="viewer-scroll" ref={frame} tabIndex={0} role="document" aria-label={label}>
      {/* A Word file shows on a light sheet in both themes, so its colors look as in Word. */}
      <article class={doc.kind === "docx" ? "doc doc-paper" : "doc"}>
        {doc.truncated && <p class="doc-info">{t("docTruncated")}</p>}
        {!!doc.pictures && <p class="doc-info">{t("docPictures")}</p>}
        {doc.blocks.length === 0 && <p class="doc-info">{t("docEmpty")}</p>}
        <Blocks blocks={doc.blocks} onNote={toNote} />
        {notes.length > 0 && (
          <section class="doc-notes">
            <h3>{t("docNotes")}</h3>
            {notes.map((note) => (
              <div class="doc-note" key={note.number} data-note={note.number}>
                <span class="doc-marker">{note.number}</span>
                <div>
                  <Blocks blocks={note.blocks} onNote={toNote} />
                </div>
              </div>
            ))}
          </section>
        )}
      </article>
    </div>
  );
}
