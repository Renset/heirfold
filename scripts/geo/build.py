#!/usr/bin/env python3
"""Makes the two data files for places and maps from public data.

Heirfold names the place of a photo and draws a map without any request to
another service. So the data lives in the program:

  internal/geo/data/places.tsv.gz  the settlements of the world, for the name
                                   of a place (GeoNames, CC BY 4.0)
  web/public/geo/world.json        the outlines of countries, lakes and
                                   rivers, and the large cities, for the map
                                   (Natural Earth, public domain; GeoNames)
  internal/geo/data/detail.pack    rivers, lakes, the borders of regions and
                                   the main roads at a larger scale, cut into
                                   squares of two degrees for a close map
                                   (Natural Earth 1:10m, public domain)

Input, in one folder:

  cities500.txt            from https://download.geonames.org/export/dump/cities500.zip
  alternateNamesV2.txt     from https://download.geonames.org/export/dump/alternateNamesV2.zip
  ne_50m_admin_0_countries.geojson, ne_50m_lakes.geojson,
  ne_50m_rivers_lake_centerlines.geojson,
  ne_10m_rivers_lake_centerlines.geojson, ne_10m_lakes.geojson,
  ne_10m_admin_1_states_provinces_lines.geojson, ne_10m_roads.geojson
                           from https://github.com/nvkelso/natural-earth-vector/tree/master/geojson

Run: python3 scripts/geo/build.py <folder with the input>
     python3 scripts/geo/build.py <folder with the input> detail
The second form makes only detail.pack. It needs only the four ne_10m files.
"""
import gzip
import io
import json
import os
import struct
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
# A part of a town, and a place where nobody lives now, is no settlement of a photo.
SKIP = {"PPLX", "PPLH", "PPLQ", "PPLW", "PPLCH"}
# The map names the cities with at least this number of people.
CITY_ON_MAP = 100_000
# Coordinates of the map are whole numbers of 1/100 of a degree.
Q = 100


def read_places(folder):
    places = {}
    with open(os.path.join(folder, "cities500.txt"), encoding="utf-8") as f:
        for line in f:
            c = line.rstrip("\n").split("\t")
            if c[7] in SKIP:
                continue
            places[c[0]] = {
                "name": c[1], "ru": "", "cc": c[8], "lat": float(c[4]), "lon": float(c[5]),
                "pop": int(c[14] or 0), "capital": c[7] == "PPLC",
            }
    return places


def add_russian_names(folder, places):
    """Takes the Russian name of each place. A preferred name wins; a name
    that is old or is a nickname does not count."""
    best = {}
    with open(os.path.join(folder, "alternateNamesV2.txt"), encoding="utf-8") as f:
        for line in f:
            c = line.rstrip("\n").split("\t")
            if len(c) < 8 or c[2] != "ru" or c[1] not in places:
                continue
            preferred, short, colloquial, historic = (c[4] == "1", c[5] == "1", c[6] == "1", c[7] == "1")
            if colloquial or historic:
                continue
            name = c[3].strip()
            if not name or "\t" in name:
                continue
            rank = (0 if preferred else 1, 0 if not short else 1)
            if c[1] not in best or rank < best[c[1]][0]:
                best[c[1]] = (rank, name)
    for key, (_, name) in best.items():
        places[key]["ru"] = name


def write_places(places):
    rows = sorted(places.values(), key=lambda p: (p["cc"], p["name"], p["lat"], p["lon"]))
    out = io.StringIO()
    for p in rows:
        ru = "" if p["ru"] == p["name"] else p["ru"]
        out.write(f'{p["name"]}\t{ru}\t{p["cc"]}\t{p["lat"]:.4f}\t{p["lon"]:.4f}\t{p["pop"]}\n')
    path = os.path.join(ROOT, "internal", "geo", "data", "places.tsv.gz")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    # No time and no name in the header, so the same input gives the same file.
    with open(path, "wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, compresslevel=9, mtime=0) as gz:
            gz.write(out.getvalue().encode("utf-8"))
    return len(rows), os.path.getsize(path)


def line(points, closed):
    """Turns a line into whole numbers: the first point, then the steps."""
    out, last = [], None
    for lon, lat in ((round(x * Q), round(y * Q)) for x, y, *_ in points):
        if last is None:
            out += [lon, lat]
        elif (lon, lat) != last:
            out += [lon - last[0], lat - last[1]]
        else:
            continue
        last = (lon, lat)
    if len(out) < (8 if closed else 4):
        return None
    return out


def polygons(geometry):
    if geometry is None:
        return []
    if geometry["type"] == "Polygon":
        return [geometry["coordinates"]]
    if geometry["type"] == "MultiPolygon":
        return geometry["coordinates"]
    return []


def lines(geometry):
    if geometry is None:
        return []
    if geometry["type"] == "LineString":
        return [geometry["coordinates"]]
    if geometry["type"] == "MultiLineString":
        return geometry["coordinates"]
    return []


def load(folder, name):
    with open(os.path.join(folder, name + ".geojson"), encoding="utf-8") as f:
        return json.load(f)["features"]


def write_world(folder, places):
    countries = []
    for feature in load(folder, "ne_50m_admin_0_countries"):
        p = feature["properties"]
        code = next((p[k] for k in ("ISO_A2_EH", "ISO_A2") if p.get(k) and p[k] != "-99"), "")
        # Only the outer line of each part: a hole is a lake or another country, and those are drawn over it.
        rings = [r for r in (line(poly[0], True) for poly in polygons(feature["geometry"])) if r]
        if not rings:
            continue
        countries.append({
            "c": code, "n": p["NAME"], "l": [round(p["LABEL_X"] * Q), round(p["LABEL_Y"] * Q)],
            "z": p.get("MIN_LABEL") or 4, "p": int(p.get("POP_EST") or 0), "r": rings,
        })
    lakes = [r for f in load(folder, "ne_50m_lakes") for r in (line(poly[0], True) for poly in polygons(f["geometry"])) if r]
    rivers = [
        {"k": int(f["properties"].get("scalerank") or 9), "p": r}
        for f in load(folder, "ne_50m_rivers_lake_centerlines")
        for r in (line(part, False) for part in lines(f["geometry"])) if r
    ]
    cities = [
        {"n": p["name"], **({"r": p["ru"]} if p["ru"] and p["ru"] != p["name"] else {}),
         "p": [round(p["lon"] * Q), round(p["lat"] * Q)], "s": p["pop"], **({"c": 1} if p["capital"] else {})}
        for p in sorted(places.values(), key=lambda p: -p["pop"]) if p["pop"] >= CITY_ON_MAP or p["capital"]
    ]
    world = {"q": Q, "countries": countries, "lakes": lakes, "rivers": rivers, "cities": cities}
    path = os.path.join(ROOT, "web", "public", "geo", "world.json")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(world, f, ensure_ascii=False, separators=(",", ":"))
    return len(countries), len(lakes), len(rivers), len(cities), os.path.getsize(path)


# The close map: coordinates are whole numbers of 1/2000 of a degree (about
# 50 metres), and the world is cut into squares of two degrees.
DETAIL_Q = 2000
SQUARE = 2


def square_of(lon, lat):
    return (min(int((lat + 90) // SQUARE), 180 // SQUARE - 1), min(int((lon + 180) // SQUARE), 360 // SQUARE - 1))


def steps(points):
    """Whole numbers: the first point, then the steps. None for less than two points."""
    out, last = [], None
    for lon, lat in points:
        here = (round(lon * DETAIL_Q), round(lat * DETAIL_Q))
        if last is None:
            out += [here[0], here[1]]
        elif here != last:
            out += [here[0] - last[0], here[1] - last[1]]
        else:
            continue
        last = here
    return out if len(out) >= 4 else None


def cut(points):
    """Cuts a line into pieces, one for each square that it passes. A step
    belongs to the square of its first point, so the pieces leave no gap."""
    points = [(p[0], p[1]) for p in points]
    if len(points) < 2:
        return
    piece, here = [points[0]], square_of(*points[0])
    for point in points[1:]:
        piece.append(point)
        if square_of(*point) != here:
            yield here, piece
            piece, here = [point], square_of(*point)
    if len(piece) >= 2:
        yield here, piece


def write_detail(folder):
    squares = {}

    def add(square, kind, item):
        squares.setdefault(square, {}).setdefault(kind, []).append(item)

    for f in load(folder, "ne_10m_rivers_lake_centerlines"):
        rank = int(f["properties"].get("scalerank") or 9)
        for part in lines(f["geometry"]):
            for square, piece in cut(part):
                if (line_ := steps(piece)):
                    add(square, "v", [rank] + line_)
    for f in load(folder, "ne_10m_roads"):
        kind = f["properties"].get("type") or ""
        if kind.startswith("Ferry"):
            continue
        rank = int(f["properties"].get("scalerank") or 9)
        for part in lines(f["geometry"]):
            for square, piece in cut(part):
                if (line_ := steps(piece)):
                    add(square, "d", [rank] + line_)
    for f in load(folder, "ne_10m_admin_1_states_provinces_lines"):
        for part in lines(f["geometry"]):
            for square, piece in cut(part):
                if (line_ := steps(piece)):
                    add(square, "b", line_)
    for f in load(folder, "ne_10m_lakes"):
        for poly in polygons(f["geometry"]):
            ring = [(p[0], p[1]) for p in poly[0]]
            lons, lats = [p[0] for p in ring], [p[1] for p in ring]
            # A lake wider than one square is in the map of the whole world.
            if max(lons) - min(lons) > SQUARE or max(lats) - min(lats) > SQUARE:
                continue
            if (line_ := steps(ring)) and len(line_) >= 8:
                add(square_of((min(lons) + max(lons)) / 2, (min(lats) + max(lats)) / 2), "l", line_)

    blobs = []
    for square in sorted(squares):
        raw = json.dumps(squares[square], separators=(",", ":"), sort_keys=True).encode("utf-8")
        packed = io.BytesIO()
        with gzip.GzipFile(filename="", mode="wb", fileobj=packed, compresslevel=9, mtime=0) as gz:
            gz.write(raw)
        blobs.append((square, packed.getvalue()))
    # The file: a mark, the number of squares, a list (row, column, place,
    # length) and then the squares, each as gzip of JSON.
    head = b"HFMAP1\n" + struct.pack("<I", len(blobs))
    offset = len(head) + 12 * len(blobs)
    table, body = b"", b""
    for (row, column), data in blobs:
        table += struct.pack("<HHII", row, column, offset + len(body), len(data))
        body += data
    path = os.path.join(ROOT, "internal", "geo", "data", "detail.pack")
    with open(path, "wb") as f:
        f.write(head + table + body)
    return len(blobs), os.path.getsize(path)


def main():
    if len(sys.argv) not in (2, 3) or (len(sys.argv) == 3 and sys.argv[2] != "detail"):
        sys.exit(__doc__)
    folder = sys.argv[1]
    if len(sys.argv) == 3:
        print("detail: squares %d, file: %d bytes" % write_detail(folder))
        return
    places = read_places(folder)
    add_russian_names(folder, places)
    count, size = write_places(places)
    print(f"places: {count}, with a Russian name: {sum(1 for p in places.values() if p['ru'])}, file: {size} bytes")
    print("world: countries %d, lakes %d, rivers %d, cities %d, file: %d bytes" % write_world(folder, places))
    print("detail: squares %d, file: %d bytes" % write_detail(folder))


if __name__ == "__main__":
    main()
