/** True in the static demo build. */
declare const __DEMO__: boolean;

declare module "*.css";

interface ViewTransitionLike {
  finished: Promise<void>;
}

interface Document {
  startViewTransition?: (callback: () => Promise<void> | void) => ViewTransitionLike;
}

/** The folder with the picture decoders of the PDF reader, relative to the page. */
declare const __PDF_DECODERS__: string;

declare module "pdfjs-dist/legacy/build/pdf.worker.mjs" {
  export const WorkerMessageHandler: unknown;
}
