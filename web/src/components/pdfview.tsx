import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { mediaFile } from "../api/client";
import { t } from "../i18n";
import type { PDFDocumentProxy, RenderTask } from "../lib/pdf";
import { pageAt, pageBox } from "../lib/preview";

export interface PdfControls {
  zoomBy: (factor: number) => void;
  fit: () => void;
}

interface Size {
  width: number;
  height: number;
}

/** The space around the pages in CSS pixels. The style sheet uses the same value. */
const gutter = 12;
/** On a wide screen a page at full width is too large to read. */
const maxFitWidth = 1100;
const minZoom = 0.5;
const maxZoom = 6;

function PdfPage({
  doc,
  number,
  size,
  frame,
  frameWidth,
  zoom,
  onSize,
}: {
  doc: PDFDocumentProxy;
  number: number;
  size: Size;
  frame: HTMLElement | null;
  frameWidth: number;
  zoom: number;
  onSize: (number: number, size: Size) => void;
}) {
  const holder = useRef<HTMLDivElement>(null);
  const [near, setNear] = useState(false);
  const box = pageBox(size, frameWidth, zoom, window.devicePixelRatio || 1);
  const report = useRef(onSize);
  report.current = onSize;

  // Only the pages near the screen have a picture. A long file stays light.
  useEffect(() => {
    const element = holder.current;
    if (!element || !frame) {
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => setNear(entries.some((entry) => entry.isIntersecting)),
      {
        root: frame,
        rootMargin: "150% 0px",
      },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [frame]);

  useEffect(() => {
    const element = holder.current;
    if (!element) {
      return;
    }
    if (!near) {
      element.replaceChildren();
      return;
    }
    let active = true;
    let task: RenderTask | undefined;
    void (async () => {
      const page = await doc.getPage(number);
      if (!active) {
        return;
      }
      const base = page.getViewport({ scale: 1 });
      if (Math.abs(base.width - size.width) > 0.5 || Math.abs(base.height - size.height) > 0.5) {
        // The first guess of the size was wrong. The next pass draws the page.
        report.current(number, { width: base.width, height: base.height });
        return;
      }
      const viewport = page.getViewport({ scale: box.scale });
      const canvas = document.createElement("canvas");
      canvas.width = Math.max(1, Math.floor(viewport.width));
      canvas.height = Math.max(1, Math.floor(viewport.height));
      task = page.render({ canvas, viewport });
      await task.promise;
      if (active) {
        // The old picture stays until the new one is ready.
        element.replaceChildren(canvas);
      }
    })().catch(() => {
      // A cancelled or failed page stays empty. The other pages still show.
    });
    return () => {
      active = false;
      task?.cancel();
    };
  }, [doc, number, near, size.width, size.height, box.scale]);

  return (
    <div
      ref={holder}
      class="pdf-page"
      role="img"
      aria-label={t("pdfPage", { number })}
      style={{ width: `${box.width}px`, height: `${box.height}px` }}
    />
  );
}

/** Shows a PDF as a column of pages. */
export function PdfView({
  id,
  label,
  onReady,
  onPage,
  onFail,
}: {
  id: string;
  label: string;
  onReady: (controls: PdfControls | null) => void;
  onPage: (page: number, count: number) => void;
  onFail: () => void;
}) {
  const frame = useRef<HTMLDivElement>(null);
  const [frameElement, setFrameElement] = useState<HTMLElement | null>(null);
  const [doc, setDoc] = useState<PDFDocumentProxy | null>(null);
  const [sizes, setSizes] = useState<Size[]>([]);
  const [frameWidth, setFrameWidth] = useState(0);
  const [zoom, setZoom] = useState(1);
  const zoomNow = useRef(1);
  const anchor = useRef<{ ratio: number; top: number; left: number } | null>(null);
  const callbacks = useRef({ onReady, onPage, onFail });
  callbacks.current = { onReady, onPage, onFail };

  useEffect(() => {
    let active = true;
    let close: (() => void) | undefined;
    void (async () => {
      let openPdf: typeof import("../lib/pdf").openPdf;
      if (__DEMO__) {
        // The static demo has no files, so its build leaves the PDF reader out.
        throw new Error("no PDF view in the demo");
      } else {
        openPdf = (await import("../lib/pdf")).openPdf;
      }
      if (!active) {
        return;
      }
      const task = openPdf(new URL(mediaFile(id), document.baseURI).href);
      close = () => void task.destroy();
      const loaded = await task.promise;
      if (!active) {
        return;
      }
      const first = (await loaded.getPage(1)).getViewport({ scale: 1 });
      if (!active) {
        return;
      }
      setSizes(Array.from({ length: loaded.numPages }, () => ({ width: first.width, height: first.height })));
      setDoc(loaded);
      callbacks.current.onPage(1, loaded.numPages);
    })().catch(() => {
      if (active) {
        callbacks.current.onFail();
      }
    });
    return () => {
      active = false;
      close?.();
    };
  }, [id]);

  // The frame exists only after the file is open.
  useEffect(() => {
    const element = frame.current;
    setFrameElement(element);
    if (!element) {
      return;
    }
    const measure = (): void =>
      setFrameWidth(Math.min(Math.max(element.clientWidth - 2 * gutter, 50), maxFitWidth));
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [doc]);

  useEffect(() => {
    if (!doc) {
      return;
    }
    const change = (next: number): void => {
      const element = frame.current;
      const clamped = Math.min(Math.max(next, minZoom), maxZoom);
      if (!element || clamped === zoomNow.current) {
        return;
      }
      // The point in the middle of the screen stays in the middle.
      anchor.current = {
        ratio: clamped / zoomNow.current,
        top: element.scrollTop + element.clientHeight / 2,
        left: element.scrollLeft + element.clientWidth / 2,
      };
      zoomNow.current = clamped;
      setZoom(clamped);
    };
    callbacks.current.onReady({ zoomBy: (factor) => change(zoomNow.current * factor), fit: () => change(1) });
    return () => callbacks.current.onReady(null);
  }, [doc]);
  useLayoutEffect(() => {
    const element = frame.current;
    const point = anchor.current;
    anchor.current = null;
    if (element && point) {
      element.scrollTop = point.top * point.ratio - element.clientHeight / 2;
      element.scrollLeft = point.left * point.ratio - element.clientWidth / 2;
    }
  }, [zoom]);

  if (!doc) {
    return <div class="spinner" role="status" aria-label={t("loading")} />;
  }

  let waiting = false;
  const onScroll = (): void => {
    if (waiting) {
      return;
    }
    waiting = true;
    requestAnimationFrame(() => {
      waiting = false;
      const element = frame.current;
      if (!element) {
        return;
      }
      const tops = [...element.querySelectorAll<HTMLElement>(".pdf-page")].map((page) => page.offsetTop);
      callbacks.current.onPage(pageAt(tops, element.scrollTop + element.clientHeight / 2) + 1, tops.length);
    });
  };

  return (
    <div
      class="viewer-scroll pdf"
      ref={frame}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: the keyboard scrolls the pages
      tabIndex={0}
      role="document"
      aria-label={label}
      onScroll={onScroll}
    >
      <div class="pdf-pages">
        {(frameWidth > 0 ? sizes : []).map((size, index) => (
          <PdfPage
            key={index}
            doc={doc}
            number={index + 1}
            size={size}
            frame={frameElement}
            frameWidth={frameWidth}
            zoom={zoom}
            onSize={(number, next) =>
              setSizes((current) => current.map((old, position) => (position === number - 1 ? next : old)))
            }
          />
        ))}
      </div>
    </div>
  );
}
