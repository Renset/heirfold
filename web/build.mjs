// Builds the frontend with esbuild.
//   node build.mjs           builds into ../internal/web/dist for the Go binary
//   node build.mjs --demo    builds the static demo into dist-demo
//   node build.mjs --watch   rebuilds on change
//   node build.mjs --tests   bundles the unit tests into test-dist
import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { basename, join } from "node:path";
import * as esbuild from "esbuild";

const args = new Set(process.argv.slice(2));
const demo = args.has("--demo");
const watch = args.has("--watch");

if (args.has("--tests")) {
  rmSync("test-dist", { recursive: true, force: true });
  await esbuild.build({
    entryPoints: readdirSync("test")
      .filter((name) => name.endsWith(".test.ts"))
      .map((name) => join("test", name)),
    outdir: "test-dist",
    bundle: true,
    platform: "node",
    format: "esm",
    target: "node22",
    jsx: "automatic",
    jsxImportSource: "preact",
    logLevel: "warning",
  });
  process.exit(0);
}

const out = demo ? "dist-demo" : "../internal/web/dist";

const demoPolicy =
  "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; " +
  "connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'none'; " +
  "require-trusted-types-for 'script'; trusted-types 'none'";

const contract = readFileSync("src/styles/contract.html", "utf8").trim();

function page(script, style) {
  const csp = demo ? `    <meta http-equiv="Content-Security-Policy" content="${demoPolicy}" />\n` : "";
  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
${csp}    <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
    <meta name="referrer" content="no-referrer" />
    <meta name="robots" content="${demo ? "index,follow" : "noindex,nofollow"}" />
    <meta name="color-scheme" content="light dark" />
    <title>Heirfold</title>
    <link rel="manifest" href="manifest.webmanifest" />
    <link rel="icon" href="icons/icon.svg" type="image/svg+xml" />
    <link rel="apple-touch-icon" href="icons/icon-180.png" />
    <link rel="stylesheet" href="${style}" />
    <script type="module" src="${script}"></script>
  </head>
  <body>
    ${contract}
    <div id="app"></div>
    <noscript>Heirfold needs JavaScript. / Для работы Heirfold нужен JavaScript.</noscript>
  </body>
</html>
`;
}

function clean() {
  mkdirSync(out, { recursive: true });
  for (const name of readdirSync(out)) {
    if (name !== ".gitkeep") {
      rmSync(join(out, name), { recursive: true, force: true });
    }
  }
}

const options = {
  entryPoints: { app: "src/main.tsx" },
  outdir: join(out, "assets"),
  bundle: true,
  minify: !watch,
  sourcemap: false,
  format: "esm",
  target: ["es2022", "safari16"],
  jsx: "automatic",
  jsxImportSource: "preact",
  entryNames: watch ? "[name]" : "[name]-[hash]",
  // The PDF reader is large. It loads as a separate file when a PDF opens.
  splitting: true,
  chunkNames: "[name]-[hash]",
  assetNames: "[name]-[hash]",
  loader: { ".woff2": "file" },
  metafile: true,
  legalComments: "none",
  logLevel: "warning",
  define: { __DEMO__: String(demo) },
};

// The PDF reader loads these decoders by name when a PDF needs them. They
// are the versions without WebAssembly, which the security policy blocks.
// The folder name carries the version, because the server caches the assets
// folder for a long time.
const pdfVersion = JSON.parse(readFileSync("node_modules/pdfjs-dist/package.json", "utf8")).version;
const pdfFolder = `pdfjs-${pdfVersion}`;
const pdfDecoders = ["jbig2_nowasm_fallback.js", "openjpeg_nowasm_fallback.js"];
options.define.__PDF_DECODERS__ = JSON.stringify(`assets/${pdfFolder}/`);

function writeShell(metafile) {
  const entry = Object.entries(metafile.outputs).find(([, output]) => output.entryPoint === "src/main.tsx");
  const [script, { cssBundle: style }] = entry;
  const relative = (file) => `assets/${basename(file)}`;
  writeFileSync(join(out, "index.html"), page(relative(script), relative(style)));
  if (existsSync("public")) {
    cpSync("public", out, { recursive: true });
  }
  if (!demo) {
    mkdirSync(join(out, "assets", pdfFolder), { recursive: true });
    for (const name of pdfDecoders) {
      cpSync(join("node_modules/pdfjs-dist/wasm", name), join(out, "assets", pdfFolder, name));
    }
    // The licenses of the reader and of the two decoders ship with the app.
    cpSync("node_modules/pdfjs-dist/LICENSE", join(out, "licenses", "pdfjs-LICENSE.txt"));
    for (const name of [
      "LICENSE_JBIG2",
      "LICENSE_PDFJS_JBIG2",
      "LICENSE_OPENJPEG",
      "LICENSE_PDFJS_OPENJPEG",
    ]) {
      cpSync(join("node_modules/pdfjs-dist/wasm", name), join(out, "licenses", `pdfjs-${name}.txt`));
    }
  }
}

clean();
if (watch) {
  const context = await esbuild.context({
    ...options,
    plugins: [
      {
        name: "shell",
        setup(build) {
          build.onEnd((result) => {
            if (result.metafile) {
              writeShell(result.metafile);
            }
          });
        },
      },
    ],
  });
  await context.watch();
  console.log("Watching for changes");
} else {
  const result = await esbuild.build(options);
  writeShell(result.metafile);
  console.log(`Built into ${out}`);
}
