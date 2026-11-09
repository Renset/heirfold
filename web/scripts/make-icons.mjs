// Draws the app icon and writes it as PNG and SVG. It uses no image library.
// Run it once when the mark changes: node scripts/make-icons.mjs
import { writeFileSync } from "node:fs";
import { deflateSync } from "node:zlib";

const accent = [34, 91, 255];
const white = [255, 255, 255];

// The mark in a 100 by 100 space: three people joined by lines.
const circles = [
  { x: 32, y: 32, r: 11.5 },
  { x: 68, y: 32, r: 11.5 },
  { x: 50, y: 70, r: 11.5 },
];
const segments = [
  [32, 43.5, 32, 51],
  [68, 43.5, 68, 51],
  [32, 51, 68, 51],
  [50, 51, 50, 58.5],
];
const halfWidth = 2.6;
const cornerRadius = 22;

function distanceToSegment(px, py, [x0, y0, x1, y1]) {
  const dx = x1 - x0;
  const dy = y1 - y0;
  const t = Math.max(0, Math.min(1, ((px - x0) * dx + (py - y0) * dy) / (dx * dx + dy * dy)));
  return Math.hypot(px - x0 - t * dx, py - y0 - t * dy);
}

function inMark(x, y) {
  return (
    circles.some((c) => Math.hypot(x - c.x, y - c.y) <= c.r) ||
    segments.some((s) => distanceToSegment(x, y, s) <= halfWidth)
  );
}

function inTile(x, y, rounded) {
  if (!rounded) {
    return true;
  }
  const qx = Math.max(cornerRadius - x, x - (100 - cornerRadius), 0);
  const qy = Math.max(cornerRadius - y, y - (100 - cornerRadius), 0);
  return Math.hypot(qx, qy) <= cornerRadius;
}

function crc32(buffer) {
  let crc = ~0;
  for (const byte of buffer) {
    crc ^= byte;
    for (let k = 0; k < 8; k++) {
      crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
    }
  }
  return ~crc >>> 0;
}

function chunk(type, data) {
  const out = Buffer.alloc(12 + data.length);
  out.writeUInt32BE(data.length, 0);
  out.write(type, 4, "latin1");
  data.copy(out, 8);
  out.writeUInt32BE(crc32(out.subarray(4, 8 + data.length)), 8 + data.length);
  return out;
}

function png(size, rounded) {
  const raw = Buffer.alloc((size * 4 + 1) * size);
  const samples = 4;
  let offset = 0;
  for (let y = 0; y < size; y++) {
    raw[offset++] = 0;
    for (let x = 0; x < size; x++) {
      let tile = 0;
      let mark = 0;
      for (let sy = 0; sy < samples; sy++) {
        for (let sx = 0; sx < samples; sx++) {
          const u = ((x + (sx + 0.5) / samples) / size) * 100;
          const v = ((y + (sy + 0.5) / samples) / size) * 100;
          if (inTile(u, v, rounded)) {
            tile++;
            if (inMark(u, v)) {
              mark++;
            }
          }
        }
      }
      const total = samples * samples;
      const alpha = tile / total;
      const share = tile === 0 ? 0 : mark / tile;
      for (let c = 0; c < 3; c++) {
        raw[offset++] = Math.round(accent[c] + (white[c] - accent[c]) * share);
      }
      raw[offset++] = Math.round(alpha * 255);
    }
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(size, 0);
  header.writeUInt32BE(size, 4);
  header[8] = 8;
  header[9] = 6;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", header),
    chunk("IDAT", deflateSync(raw, { level: 9 })),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
<rect width="100" height="100" rx="${cornerRadius}" fill="rgb(${accent.join(" ")})"/>
<g fill="none" stroke="#fff" stroke-width="${halfWidth * 2}" stroke-linecap="round">
<path d="M32 43.5V51M68 43.5V51M32 51H68M50 51V58.5"/>
</g>
<g fill="#fff">${circles.map((c) => `<circle cx="${c.x}" cy="${c.y}" r="${c.r}"/>`).join("")}</g>
</svg>
`;

writeFileSync("public/icons/icon.svg", svg);
for (const size of [180, 192, 512]) {
  writeFileSync(`public/icons/icon-${size}.png`, png(size, size !== 180));
}
console.log("Icons written.");
