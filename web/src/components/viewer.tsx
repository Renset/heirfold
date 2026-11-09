import { useEffect, useRef, useState } from "preact/hooks";
import { mediaFile, mediaImage } from "../api/client";
import type { MediaView } from "../api/types";
import { locale, t } from "../i18n";
import { formatDate } from "../lib/format";
import { kindLabel, type Preview, previewOf } from "../lib/preview";
import { DocView } from "./docview";
import { Icon } from "./icons";
import { type PanZoomControls, usePanZoom } from "./panzoom";
import { type PdfControls, PdfView } from "./pdfview";

type ControlsRef = { current: PanZoomControls };

/** The picture on a surface that the reader can drag and pinch. */
function Stage({
  src,
  width,
  height,
  label,
  onReady,
}: {
  src: string;
  width: number;
  height: number;
  label: string;
  onReady: (controls: ControlsRef) => void;
}) {
  const svg = useRef<SVGSVGElement>(null);
  const layer = useRef<SVGGElement>(null);
  const controls = usePanZoom(
    svg,
    layer,
    { minX: 0, minY: 0, maxX: width, maxY: height },
    { x: width / 2, y: height / 2 },
    0.5,
    true,
  );
  useEffect(() => {
    onReady(controls);
  }, [controls, onReady]);
  return (
    // biome-ignore lint/a11y/noNoninteractiveTabindex: the picture takes arrow keys for panning
    <svg ref={svg} class="viewer-stage" tabIndex={0} role="img" aria-label={label}>
      <g ref={layer}>
        <image href={src} width={width} height={height} />
      </g>
    </svg>
  );
}

function fileKind(item: MediaView): string {
  return kindLabel(item) || t("fileGeneric");
}

/** The static demo has no server, so it shows only pictures. */
function previewFor(item: MediaView): Preview {
  const preview = previewOf(item);
  return __DEMO__ && preview !== "image" ? "none" : preview;
}

/** Shows photos, page scans and documents on the whole screen, one at a time. */
export function Viewer({
  items,
  start,
  onClose,
}: {
  items: MediaView[];
  start: number;
  onClose: () => void;
}) {
  const [index, setIndex] = useState(Math.min(Math.max(start, 0), items.length - 1));
  const [size, setSize] = useState<{ id: string; width: number; height: number } | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  // The file that the viewer could not turn into a view.
  const [broken, setBroken] = useState<string | null>(null);
  const [pages, setPages] = useState<{ id: string; page: number; count: number } | null>(null);
  const pdf = useRef<PdfControls | null>(null);
  const panel = useRef<HTMLDivElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const zoom = useRef<ControlsRef | null>(null);
  const item = items[index];
  const src = item?.image ? mediaImage(item.id, "l") : "";

  // The picture needs its size before the stage can place it.
  useEffect(() => {
    if (!item || !src) {
      return;
    }
    let active = true;
    const probe = new Image();
    probe.onload = () => {
      if (active) {
        setSize({ id: item.id, width: probe.naturalWidth, height: probe.naturalHeight });
      }
    };
    probe.onerror = () => {
      if (active) {
        setFailed(item.id);
      }
    };
    probe.src = src;
    return () => {
      active = false;
    };
  }, [item, src]);

  useEffect(() => {
    const before = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    document.documentElement.classList.add("viewer-open");
    close.current?.focus();
    return () => {
      document.documentElement.classList.remove("viewer-open");
      before?.focus();
    };
  }, []);

  useEffect(() => {
    const step = (delta: number): void => {
      setIndex((current) => Math.min(Math.max(current + delta, 0), items.length - 1));
    };
    const onKey = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
        return;
      }
      // On the picture the arrow keys move the picture. In a document they scroll.
      const onStage =
        event.target instanceof Element && event.target.closest(".viewer-stage, .viewer-scroll");
      if (!onStage && event.key === "ArrowLeft") {
        step(-1);
      } else if (!onStage && event.key === "ArrowRight") {
        step(1);
      } else if (event.key === "Tab" && panel.current) {
        // Keep the keyboard inside the viewer while it covers the page.
        const stops = [
          ...panel.current.querySelectorAll<HTMLElement>("button:not(:disabled), a[href], [tabindex]"),
        ];
        const first = stops[0];
        const last = stops[stops.length - 1];
        if (first && last && event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (first && last && !event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [items.length, onClose]);

  if (!item) {
    return null;
  }
  const title = item.description || t("fileUntitled");
  const date = formatDate(item.date, locale.value);
  const ready = size && size.id === item.id && failed !== item.id;
  const many = items.length > 1;
  const preview = broken === item.id ? "none" : previewFor(item);
  const pdfPages = preview === "pdf" && pages && pages.id === item.id ? pages : null;

  return (
    <div class="viewer" ref={panel} role="dialog" aria-modal="true" aria-label={title}>
      <header class="viewer-bar">
        <button ref={close} type="button" class="tool" aria-label={t("close")} onClick={onClose}>
          <Icon name="x" />
        </button>
        <div class="viewer-title">
          <h2>{title}</h2>
          {(date || pdfPages) && (
            <p aria-live={pdfPages ? "polite" : undefined}>
              {[date, pdfPages && t("pdfPageOf", { page: pdfPages.page, count: pdfPages.count })]
                .filter(Boolean)
                .join(" · ")}
            </p>
          )}
        </div>
        {!__DEMO__ && (
          <a class="tool" href={mediaFile(item.id)} download aria-label={t("downloadOriginal")}>
            <Icon name="download-simple" />
          </a>
        )}
      </header>
      <div class="viewer-body">
        {item.image && ready && size && (
          <Stage
            src={src}
            width={size.width}
            height={size.height}
            label={title}
            onReady={(controls) => {
              zoom.current = controls;
            }}
          />
        )}
        {item.image && !ready && failed !== item.id && (
          <div class="spinner" role="status" aria-label={t("loading")} />
        )}
        {preview === "pdf" && (
          <PdfView
            key={item.id}
            id={item.id}
            label={title}
            onReady={(controls) => {
              pdf.current = controls;
            }}
            onPage={(page, count) => setPages({ id: item.id, page, count })}
            onFail={() => setBroken(item.id)}
          />
        )}
        {preview === "document" && (
          <DocView key={item.id} id={item.id} label={title} onFail={() => setBroken(item.id)} />
        )}
        {(preview === "none" || failed === item.id) && (
          <div class="viewer-file">
            <span class="viewer-kind">{fileKind(item)}</span>
            <p>
              {item.image
                ? t("fileMissing")
                : broken === item.id
                  ? t("filePreviewFailed")
                  : t("fileNoPreview")}
            </p>
            {!__DEMO__ && failed !== item.id && (
              <a class="button filled" href={mediaFile(item.id)} download>
                <Icon name="download-simple" />
                {t("downloadOriginal")}
              </a>
            )}
          </div>
        )}
      </div>
      <footer class="viewer-bar">
        {many && (
          <button
            type="button"
            class="tool"
            aria-label={t("viewerPrevious")}
            disabled={index === 0}
            onClick={() => setIndex(index - 1)}
          >
            <Icon name="caret-left" />
          </button>
        )}
        {many && (
          <span class="viewer-count" aria-live="polite">
            {t("viewerCount", { index: index + 1, count: items.length })}
          </span>
        )}
        {many && (
          <button
            type="button"
            class="tool"
            aria-label={t("viewerNext")}
            disabled={index === items.length - 1}
            onClick={() => setIndex(index + 1)}
          >
            <Icon name="caret-right" />
          </button>
        )}
        <span class="viewer-space" />
        {pdfPages && (
          <>
            <button
              type="button"
              class="tool"
              aria-label={t("zoomOut")}
              onClick={() => pdf.current?.zoomBy(0.77)}
            >
              <Icon name="minus" />
            </button>
            <button
              type="button"
              class="tool"
              aria-label={t("zoomIn")}
              onClick={() => pdf.current?.zoomBy(1.3)}
            >
              <Icon name="plus" />
            </button>
            <button type="button" class="tool" aria-label={t("fitPage")} onClick={() => pdf.current?.fit()}>
              <Icon name="corners-out" />
            </button>
          </>
        )}
        {item.image && ready && (
          <>
            <button
              type="button"
              class="tool"
              aria-label={t("zoomOut")}
              onClick={() => zoom.current?.current.zoomBy(0.77)}
            >
              <Icon name="minus" />
            </button>
            <button
              type="button"
              class="tool"
              aria-label={t("zoomIn")}
              onClick={() => zoom.current?.current.zoomBy(1.3)}
            >
              <Icon name="plus" />
            </button>
            <button
              type="button"
              class="tool"
              aria-label={t("fitImage")}
              onClick={() => zoom.current?.current.fit()}
            >
              <Icon name="corners-out" />
            </button>
          </>
        )}
      </footer>
    </div>
  );
}

/** One tile of a photo or a file. */
export function Thumb({ item, onOpen }: { item: MediaView; onOpen: () => void }) {
  const [failed, setFailed] = useState(false);
  const src = item.image ? mediaImage(item.id, "m", item.region) : "";
  const label = item.description || t("fileUntitled");
  return (
    <button
      type="button"
      class="thumb"
      onClick={onOpen}
      aria-label={t("openFile", { name: label })}
      title={label}
    >
      {src && !failed ? (
        <img src={src} alt="" loading="lazy" decoding="async" onError={() => setFailed(true)} />
      ) : (
        <span class="thumb-file">
          <span class="viewer-kind">{fileKind(item)}</span>
          <span class="thumb-name">{label}</span>
        </span>
      )}
    </button>
  );
}

/** A grid of tiles that opens the viewer. */
export function Thumbs({ items }: { items: MediaView[] }) {
  const [open, setOpen] = useState<number | null>(null);
  if (items.length === 0) {
    return null;
  }
  return (
    <>
      <div class="thumbs">
        {items.map((item, position) => (
          <Thumb key={item.id} item={item} onOpen={() => setOpen(position)} />
        ))}
      </div>
      {open !== null && <Viewer items={items} start={open} onClose={() => setOpen(null)} />}
    </>
  );
}
