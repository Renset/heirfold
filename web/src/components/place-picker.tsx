import { useEffect, useState } from "preact/hooks";
import { server } from "../api/client";
import type { FoundPlace, PhotoPlace } from "../api/types";
import { locale, t } from "../i18n";
import { kmBetween, placeName } from "../lib/gallery";
import { Button } from "./ui";
import { MapCredit, WorldMap } from "./worldmap";

/** A press this near to the chosen place, in kilometres, moves the point and keeps the name. */
const SAME_PLACE = 5;

/**
 * Chooses the place of a photo: a town or a village of the list of the
 * server, or a place of the tree. It shows the chosen place, or a search
 * field. The server looks for the place by the start of its name. A press on
 * the map sets the exact point: near the chosen place it keeps the name, and
 * far from it the server names the settlement of the new point.
 */
export function PlacePicker({
  label,
  help,
  value,
  describe,
  onChange,
}: {
  label: string;
  help?: string;
  /** Writes a place as words: the settlement, or two numbers for a place without one. */
  describe: (place: PhotoPlace) => string;
  /** The chosen place, or null when none is chosen. */
  value: PhotoPlace | null;
  onChange: (place: PhotoPlace | null) => void;
}) {
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<FoundPlace[] | null>(null);
  const [failed, setFailed] = useState(false);
  // The map without a chosen place: the person looks for the place by eye.
  const [byMap, setByMap] = useState(false);
  const text = query.trim();

  const pressed = (lat: number, lon: number): void => {
    if (value?.name && kmBetween(value, { lat, lon }) <= SAME_PLACE) {
      onChange({ ...value, lat, lon });
      return;
    }
    // The point shows at once. The name comes when the server answers.
    onChange({ lat, lon, manual: true });
    server
      .placeNearest(lat, lon)
      .then((found) => {
        if (found) {
          onChange({
            lat,
            lon,
            name: found.name,
            nameRu: found.nameRu,
            country: found.country,
            manual: true,
          });
        }
      })
      .catch(() => {
        // The point stays without a name.
      });
  };

  // The search waits until the person stops typing for a moment.
  useEffect(() => {
    setFailed(false);
    if (text.length < 2) {
      setHits(null);
      return;
    }
    let active = true;
    const timer = window.setTimeout(() => {
      server
        .findPlaces(text)
        .then((found) => {
          if (active) {
            setHits(found);
          }
        })
        .catch(() => {
          if (active) {
            setHits(null);
            setFailed(true);
          }
        });
    }, 220);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [text]);

  if (value) {
    return (
      <div class="picker">
        <span class="field-label">{label}</span>
        <div class="picker-chosen">
          <span class="row-text">
            <span>{describe(value)}</span>
          </span>
          <Button
            kind="plain"
            onClick={() => {
              setByMap(false);
              onChange(null);
            }}
          >
            {t("pickerClear")}
          </Button>
        </div>
        <WorldMap
          size="medium"
          label={t("placeMapLabel", { name: describe(value) })}
          marks={[{ id: "chosen", lat: value.lat, lon: value.lon, label: describe(value) }]}
          onPoint={pressed}
        />
        <MapCredit />
        <p class="field-help">{t("placeMapHelp")}</p>
      </div>
    );
  }
  return (
    <div class="picker">
      <label class="field">
        <span class="field-label">{label}</span>
        <input
          type="text"
          value={query}
          autocomplete="off"
          placeholder={t("placePlaceholder")}
          onInput={(event) => setQuery(event.currentTarget.value)}
        />
      </label>
      {hits && hits.length > 0 && (
        <div class="picker-hits">
          {hits.map((place) => (
            <button
              type="button"
              class="row link"
              key={`${place.name}|${place.country ?? ""}|${place.lat}|${place.lon}`}
              onClick={() => {
                setQuery("");
                setHits(null);
                onChange({
                  lat: place.lat,
                  lon: place.lon,
                  name: place.name,
                  nameRu: place.nameRu,
                  country: place.country,
                  manual: true,
                });
              }}
            >
              <span class="row-text">
                <span>{placeName(place, locale.value) || place.name}</span>
              </span>
              {place.tree && <span class="row-value">{t("placeFromTree")}</span>}
            </button>
          ))}
        </div>
      )}
      {hits && hits.length === 0 && <p class="row-empty">{t("placeNotFound")}</p>}
      {failed && (
        <p class="field-error" role="alert">
          {t("placeSearchFailed")}
        </p>
      )}
      {help && <p class="field-help">{help}</p>}
      {byMap ? (
        <>
          <WorldMap size="medium" label={t("placeMapEmpty")} marks={[]} onPoint={pressed} />
          <MapCredit />
          <p class="field-help">{t("placeMapPick")}</p>
        </>
      ) : (
        <Button kind="plain" onClick={() => setByMap(true)}>
          {t("placeByMap")}
        </Button>
      )}
    </div>
  );
}
