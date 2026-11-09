import type { Region } from "../api/types";

// A part of a picture is stored on the pixels of the file. A camera can
// store a picture on its side and say so in the file; the server then turns
// the picture before it shows it. So a frame that a person draws on the
// upright picture must go back to the stored pixels, and a stored frame must
// go to the upright picture. The numbers 1 to 8 are the EXIF orientations.

type Point = [number, number];

/** A place on the upright picture, from a place on the stored pixels. In percent. */
function shown([x, y]: Point, orientation: number): Point {
  switch (orientation) {
    case 2:
      return [100 - x, y];
    case 3:
      return [100 - x, 100 - y];
    case 4:
      return [x, 100 - y];
    case 5:
      return [y, x];
    case 6:
      return [100 - y, x];
    case 7:
      return [100 - y, 100 - x];
    case 8:
      return [y, 100 - x];
    default:
      return [x, y];
  }
}

/** A place on the stored pixels, from a place on the upright picture. In percent. */
function stored([x, y]: Point, orientation: number): Point {
  switch (orientation) {
    case 5:
      return [y, x];
    case 6:
      return [y, 100 - x];
    case 7:
      return [100 - y, 100 - x];
    case 8:
      return [100 - y, x];
    default:
      // The orientations 1 to 4 are their own reverse.
      return shown([x, y], orientation);
  }
}

function box(a: Point, b: Point): Region {
  return {
    x1: Math.min(a[0], b[0]),
    y1: Math.min(a[1], b[1]),
    x2: Math.max(a[0], b[0]),
    y2: Math.max(a[1], b[1]),
  };
}

/** Moves a stored part of a picture to the upright picture. */
export function toDisplay(region: Region, orientation: number): Region {
  return box(shown([region.x1, region.y1], orientation), shown([region.x2, region.y2], orientation));
}

/** Moves a part of the upright picture to the stored pixels. */
export function toStored(region: Region, orientation: number): Region {
  return box(stored([region.x1, region.y1], orientation), stored([region.x2, region.y2], orientation));
}
