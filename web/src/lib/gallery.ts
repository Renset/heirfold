import type { GalleryPhoto, PhotoPlace } from "../api/types";

/** What the list of the gallery shows. */
export interface GalleryFilter {
  /** Only the photos where nobody is marked. */
  unmarked: boolean;
  /** The IDs of the people who must all be on a photo. */
  people: string[];
  /** The year of the photo. "none" shows the photos without a year. Without it every year passes. */
  year?: number | "none" | null;
  /** The key of the place of the photo, from placeKey. "none" shows the photos without a place. */
  place?: string | null;
}

export function hasPerson(photo: GalleryPhoto, person: string): boolean {
  return photo.faces.some((face) => face.person.id === person);
}

/** Returns the photos that pass the filter. Every chosen person must be on the photo. */
export function filterGallery(photos: GalleryPhoto[], filter: GalleryFilter): GalleryPhoto[] {
  return photos.filter(
    (photo) =>
      (!filter.unmarked || photo.faces.length === 0) &&
      filter.people.every((person) => hasPerson(photo, person)) &&
      inYear(photo, filter.year) &&
      inPlace(photo, filter.place),
  );
}

function inPlace(photo: GalleryPhoto, place: GalleryFilter["place"]): boolean {
  if (place === undefined || place === null) {
    return true;
  }
  return place === "none" ? !photo.place : photo.place !== undefined && placeKey(photo.place) === place;
}

/**
 * The key that groups the photos of one place: the settlement, or, far from
 * every settlement, a square of a tenth of a degree.
 */
export function placeKey(place: PhotoPlace): string {
  return place.name
    ? `${place.country ?? ""}|${place.name}`
    : `@${place.lat.toFixed(1)},${place.lon.toFixed(1)}`;
}

/** The name of the settlement in the language of the page, with the country. Empty for a place without a settlement. */
export function placeName(place: Partial<PhotoPlace>, lang: string): string {
  const town = lang === "ru" && place.nameRu ? place.nameRu : (place.name ?? "");
  if (!town) {
    return "";
  }
  let country = "";
  if (place.country) {
    try {
      country = new Intl.DisplayNames([lang], { type: "region" }).of(place.country) ?? "";
    } catch {
      country = "";
    }
  }
  return country ? `${town}, ${country}` : town;
}

/** One place of the gallery: its key, a photo place that names it, where its mark stands, and the number of photos. */
export interface PlaceOption {
  key: string;
  place: PhotoPlace;
  /** The middle of the points of the photos. */
  lat: number;
  lon: number;
  count: number;
}

/** Returns the places of the photos, the place with the most photos first. */
export function placeOptions(photos: GalleryPhoto[]): PlaceOption[] {
  const found = new Map<string, PlaceOption>();
  for (const photo of photos) {
    if (!photo.place) {
      continue;
    }
    const key = placeKey(photo.place);
    const option = found.get(key);
    if (option) {
      option.lat += photo.place.lat;
      option.lon += photo.place.lon;
      option.count++;
    } else {
      found.set(key, { key, place: photo.place, lat: photo.place.lat, lon: photo.place.lon, count: 1 });
    }
  }
  return [...found.values()]
    .map((option) => ({ ...option, lat: option.lat / option.count, lon: option.lon / option.count }))
    .sort((a, b) => b.count - a.count || a.key.localeCompare(b.key));
}

function inYear(photo: GalleryPhoto, year: GalleryFilter["year"]): boolean {
  if (year === undefined || year === null) {
    return true;
  }
  return year === "none" ? !photo.year : photo.year === year;
}

/** One choice of the filter by years, with the number of photos. */
export interface YearOption {
  year: number | "none";
  count: number;
}

/** Returns the years of the photos, the newest first, and the photos without a year last. */
export function yearOptions(photos: GalleryPhoto[]): YearOption[] {
  const counts = new Map<number, number>();
  let none = 0;
  for (const photo of photos) {
    if (photo.year) {
      counts.set(photo.year, (counts.get(photo.year) ?? 0) + 1);
    } else {
      none++;
    }
  }
  const out: YearOption[] = [...counts.entries()]
    .sort((a, b) => b[0] - a[0])
    .map(([year, count]) => ({ year, count }));
  if (none > 0 && out.length > 0) {
    out.push({ year: "none", count: none });
  }
  return out;
}

/** Turns the time from a file, "2006-01-02T15:04:05", into a date without a change of the clock. */
export function takenDate(taken: string | undefined): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})$/.exec(taken ?? "");
  if (!match) {
    return null;
  }
  const [year, month, day, hour, minute, second] = match.slice(1).map(Number) as [
    number,
    number,
    number,
    number,
    number,
    number,
  ];
  return new Date(Date.UTC(year, month - 1, day, hour, minute, second));
}

/** Writes a place as two numbers with the letters of the halves of the globe. */
export function formatPlace(
  place: { lat: number; lon: number },
  letters: { north: string; south: string; east: string; west: string },
  digits = 5,
): string {
  const part = (value: number, plus: string, minus: string): string =>
    `${Math.abs(value).toFixed(digits)}° ${value >= 0 ? plus : minus}`;
  return `${part(place.lat, letters.north, letters.south)}, ${part(place.lon, letters.east, letters.west)}`;
}

/** The link to a map for a place. The reader opens it; the app itself sends no place to any service. */
export function mapLink(place: { lat: number; lon: number }): string {
  const lat = place.lat.toFixed(5);
  const lon = place.lon.toFixed(5);
  return `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lon}#map=14/${lat}/${lon}`;
}

/** The kilometres between two points that are near to each other. */
export function kmBetween(a: { lat: number; lon: number }, b: { lat: number; lon: number }): number {
  const x = (a.lon - b.lon) * Math.cos((((a.lat + b.lat) / 2) * Math.PI) / 180);
  const y = a.lat - b.lat;
  return 111.2 * Math.hypot(x, y);
}
