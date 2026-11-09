// Fails if the frontend source uses a way to turn a string into code or HTML.
// The Content Security Policy blocks these at run time. This check stops them
// at review time.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const forbidden = [
  /dangerouslySetInnerHTML/,
  /\.innerHTML\b/,
  /\.outerHTML\b/,
  /insertAdjacentHTML/,
  /document\.write/,
  /\beval\s*\(/,
  /new\s+Function\s*\(/,
  // The page loads nothing from another origin. One address is a plain link
  // that a reader opens by hand: the map of streets for the place of a photo.
  /https?:\/\/(?!www\.w3\.org\/|github\.com\/Renset\/heirfold|www\.openstreetmap\.org\/\?mlat=)/,
];

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

const files = walk("src").filter((file) => /\.(ts|tsx|css)$/.test(file));
const problems = [];
for (const file of files) {
  readFileSync(file, "utf8")
    .split("\n")
    .forEach((line, index) => {
      for (const pattern of forbidden) {
        if (pattern.test(line)) {
          problems.push(`${file}:${index + 1}: ${pattern}`);
        }
      }
    });
}
if (problems.length > 0) {
  console.error(`Forbidden constructs:\n${problems.join("\n")}`);
  process.exit(1);
}
console.log(`Checked ${files.length} files: no HTML or code sinks, no third-party origins.`);
