// The PDF reader (PDF.js, Apache License 2.0). The app loads this module only
// when a PDF opens, because it is large.
import { getDocument } from "pdfjs-dist/legacy/build/pdf.mjs";
import { WorkerMessageHandler } from "pdfjs-dist/legacy/build/pdf.worker.mjs";

export type { PDFDocumentLoadingTask, PDFDocumentProxy, RenderTask } from "pdfjs-dist/legacy/build/pdf.mjs";

// The security policy of the app permits no worker threads. PDF.js looks for
// its worker code here and then runs it on the page itself.
(globalThis as { pdfjsWorker?: unknown }).pdfjsWorker = { WorkerMessageHandler };

/** Opens a PDF of this server. The reader asks for the file in parts. */
export function openPdf(url: string) {
  return getDocument({
    url,
    // The policy also blocks WebAssembly.
    useWasm: false,
    wasmUrl: new URL(__PDF_DECODERS__, document.baseURI).href,
    enableXfa: false,
    disableAutoFetch: true,
    verbosity: 0,
  });
}
