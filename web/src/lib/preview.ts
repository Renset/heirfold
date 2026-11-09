import type { MediaView } from "../api/types";

/** How the viewer shows a file. */
export type Preview = "image" | "pdf" | "document" | "none";

const wordType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document";

function mimeOf(item: Pick<MediaView, "mime">): string {
  return (item.mime ?? "").trim().toLowerCase();
}

export function previewOf(item: Pick<MediaView, "mime" | "image">): Preview {
  if (item.image) {
    return "image";
  }
  switch (mimeOf(item)) {
    case "application/pdf":
      return "pdf";
    case wordType:
    case "text/plain":
      return "document";
    default:
      return "none";
  }
}

/** A short name of the file type for a tile, or nothing for an unknown type. */
export function kindLabel(item: Pick<MediaView, "mime">): string {
  const mime = mimeOf(item);
  switch (mime) {
    case wordType:
      return "DOCX";
    case "application/msword":
      return "DOC";
    case "application/vnd.oasis.opendocument.text":
      return "ODT";
    case "text/plain":
      return "TXT";
  }
  const kind = mime.split("/").pop() ?? "";
  return /^[a-z0-9]{1,5}$/.test(kind) ? kind.toUpperCase() : "";
}

export interface PageBox {
  /** The size of the page on the screen, in CSS pixels. */
  width: number;
  height: number;
  /** The scale from PDF units to canvas pixels. */
  scale: number;
}

/** The largest canvas that phones draw without an error, in pixels. */
export const maxCanvasPixels = 12_000_000;

/**
 * The size of a PDF page for a frame of the given width. With zoom 1 the page
 * fills the width. The canvas has more pixels than the screen box on a dense
 * screen, up to the limit of the canvas.
 */
export function pageBox(
  page: { width: number; height: number },
  frameWidth: number,
  zoom: number,
  ratio: number,
  limit = maxCanvasPixels,
): PageBox {
  const fit = Math.max(frameWidth, 1) / Math.max(page.width, 1);
  const width = Math.max(1, Math.round(page.width * fit * zoom));
  const height = Math.max(1, Math.round(page.height * fit * zoom));
  let density = Math.min(Math.max(ratio, 1), 3);
  const pixels = width * height * density * density;
  if (pixels > limit) {
    density *= Math.sqrt(limit / pixels);
  }
  return { width, height, scale: (width / Math.max(page.width, 1)) * density };
}

/** The page that covers the middle of the frame. tops holds the top edge of each page. */
export function pageAt(tops: number[], middle: number): number {
  let index = 0;
  for (let i = 0; i < tops.length; i++) {
    if ((tops[i] ?? 0) <= middle) {
      index = i;
    }
  }
  return index;
}
