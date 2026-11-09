import { signal } from "@preact/signals";
import type { ComponentChildren } from "preact";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { edit, mediaFile, mediaImage, mediaOrientation, server } from "../api/client";
import type {
  Card,
  Dated,
  FoundFace,
  FoundFaces,
  GalleryFace,
  GalleryPhoto,
  PhotoPlace,
  Region,
} from "../api/types";
import { CropDialog } from "../components/cropper";
import { DateInput } from "../components/date-input";
import { Icon } from "../components/icons";
import { PersonPicker } from "../components/person-picker";
import { PlacePicker } from "../components/place-picker";
import { Avatar, Button, EmptyState, errorText, Field, Spinner, useAction } from "../components/ui";
import { Viewer } from "../components/viewer";
import { MapCredit, WorldMap } from "../components/worldmap";
import { locale, t } from "../i18n";
import { type DateDraft, dateFromDraft, draftFromDate } from "../lib/dates";
import { formatDate, fullName } from "../lib/format";
import {
  filterGallery,
  formatPlace,
  hasPerson,
  mapLink,
  placeName,
  placeOptions,
  takenDate,
  yearOptions,
} from "../lib/gallery";
import { toDisplay, toStored } from "../lib/orient";
import { go, link } from "../lib/router";
import { canPortrait, me, treeEdited } from "../state";

interface Loaded {
  photos: GalleryPhoto[] | null;
  /** The server has a folder for pictures. */
  files: boolean;
  error: string | null;
  reload: () => Promise<void>;
}

/** Loads the gallery from the server. */
function useGallery(): Loaded {
  const [photos, setPhotos] = useState<GalleryPhoto[] | null>(null);
  const [files, setFiles] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const reload = async (): Promise<void> => {
    try {
      const answer = await server.gallery();
      setPhotos(answer.photos);
      setFiles(answer.files);
      setError(null);
    } catch (caught) {
      setError(errorText(caught));
    }
  };
  useEffect(() => {
    void reload();
  }, []);
  return { photos, files, error, reload };
}

function cardName(card: Card): string {
  return fullName(card, locale.value) || t("unnamed");
}

function personName(face: GalleryFace): string {
  return cardName(face.person);
}

// What the list shows. The choice lives while the app is open, so the page
// of one photo steps through the same list, and a return keeps the filter.
/** Only the photos where nobody is marked. */
const unmarkedOnly = signal(false);
/** The people who must all be on a photo. */
const chosenPeople = signal<string[]>([]);
/** The year of the photos. "none" shows the photos without a year. */
const chosenYear = signal<number | "none" | null>(null);
/** The place of the photos, as a key from placeKey. "none" shows the photos without a place. */
const chosenPlace = signal<string | null>(null);
/** The list shows the photos as tiles or as places on a map. */
const layout = signal<"grid" | "map">("grid");

/** The words for a place: the settlement and the country, or two numbers far from every settlement. */
function placeWords(place: PhotoPlace): string {
  return (
    placeName(place, locale.value) ||
    // Two places after the point are about one kilometre: enough for a place without a name.
    formatPlace(
      place,
      { north: t("geoNorth"), south: t("geoSouth"), east: t("geoEast"), west: t("geoWest") },
      2,
    )
  );
}

/** The photos that pass the filter of the page. */
function filterPhotos(photos: GalleryPhoto[]): GalleryPhoto[] {
  return filterGallery(photos, {
    unmarked: unmarkedOnly.value,
    people: chosenPeople.value,
    year: chosenYear.value,
    place: chosenPlace.value,
  });
}

function togglePerson(person: string): void {
  const now = chosenPeople.value;
  chosenPeople.value = now.includes(person) ? now.filter((other) => other !== person) : [...now, person];
  unmarkedOnly.value = false;
}

interface PersonOption {
  card: Card;
  /** The number of shown photos with this person. */
  count: number;
}

/**
 * The filter by people. It lists the people who are on the shown photos, so
 * each next choice makes the list of photos shorter and never empty.
 */
function PeopleFilter({ photos, shown }: { photos: GalleryPhoto[]; shown: GalleryPhoto[] }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const chosen = chosenPeople.value;
  const options = useMemo(() => {
    const found = new Map<string, PersonOption>();
    const add = (list: GalleryPhoto[], counted: boolean): void => {
      for (const photo of list) {
        for (const face of photo.faces) {
          const option = found.get(face.person.id) ?? { card: face.person, count: 0 };
          option.count += counted ? 1 : 0;
          found.set(face.person.id, option);
        }
      }
    };
    add(shown, true);
    // A chosen person stays in the list, also when no photo is left.
    add(
      photos.filter((photo) => chosen.some((person) => hasPerson(photo, person))),
      false,
    );
    return [...found.values()]
      .filter((option) => option.count > 0 || chosen.includes(option.card.id))
      .sort((a, b) => b.count - a.count || cardName(a.card).localeCompare(cardName(b.card), locale.value));
  }, [photos, shown, chosen, locale.value]);
  const needle = query.trim().toLocaleLowerCase(locale.value);
  const listed = needle
    ? options.filter((option) => cardName(option.card).toLocaleLowerCase(locale.value).includes(needle))
    : options;
  const chosenCards = chosen.map(
    (person) => options.find((option) => option.card.id === person)?.card ?? ({ id: person } as Card),
  );

  return (
    <>
      <button
        type="button"
        class="filter-toggle"
        aria-expanded={open}
        aria-controls="gallery-people"
        onClick={() => setOpen(!open)}
      >
        <Icon name="users-three" />
        {t("galleryPeople")}
        {chosen.length > 0 && <span class="filter-number">{chosen.length}</span>}
      </button>
      {chosenCards.length > 0 && (
        <ul class="filter-chips" aria-label={t("galleryPeopleChosen")}>
          {chosenCards.map((card) => (
            <li key={card.id}>
              <button
                type="button"
                class="chip"
                aria-label={t("galleryPersonOff", { name: cardName(card) })}
                onClick={() => togglePerson(card.id)}
              >
                {cardName(card)}
                <Icon name="x" />
              </button>
            </li>
          ))}
        </ul>
      )}
      {open && (
        <div class="people-panel" id="gallery-people">
          {options.length === 0 ? (
            <p class="row-empty">{t("galleryPeopleNone")}</p>
          ) : (
            <>
              {options.length > 8 && (
                <input
                  type="search"
                  class="people-search"
                  placeholder={t("galleryPeopleSearch")}
                  aria-label={t("galleryPeopleSearch")}
                  value={query}
                  onInput={(event) => setQuery(event.currentTarget.value)}
                />
              )}
              <ul class="people-options">
                {listed.map((option) => {
                  const on = chosen.includes(option.card.id);
                  return (
                    <li key={option.card.id}>
                      <button
                        type="button"
                        class="people-option"
                        aria-pressed={on}
                        onClick={() => togglePerson(option.card.id)}
                      >
                        <Avatar card={option.card} size="s" />
                        <span class="row-text">{cardName(option.card)}</span>
                        <span class="people-count">{option.count}</span>
                        <span class="people-check">{on && <Icon name="check" />}</span>
                      </button>
                    </li>
                  );
                })}
              </ul>
              {listed.length === 0 && <p class="row-empty">{t("galleryPeopleNoMatch")}</p>}
              <p class="group-note">{t("galleryPeopleHelp")}</p>
            </>
          )}
        </div>
      )}
    </>
  );
}

function photoTitle(photo: GalleryPhoto): string {
  return photo.description || t("photoUntitled");
}

/** The photo gallery: every picture of the tree, the newest first. */
export function GalleryView() {
  const { photos, files, error, reload } = useGallery();
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [failures, setFailures] = useState<string[]>([]);
  const canEdit = Boolean(me.value?.edit);

  const upload = async (event: Event): Promise<void> => {
    const input = event.currentTarget as HTMLInputElement;
    const chosen = [...(input.files ?? [])];
    input.value = "";
    if (chosen.length === 0) {
      return;
    }
    const failed: string[] = [];
    let revision = 0;
    let created = "";
    setFailures([]);
    for (const [index, file] of chosen.entries()) {
      setProgress({ done: index, total: chosen.length });
      try {
        const result = await edit.galleryAdd(file);
        revision = result.revision;
        created = result.created ?? "";
      } catch (caught) {
        failed.push(t("galleryUploadFailed", { name: file.name, error: errorText(caught) }));
      }
    }
    setProgress(null);
    setFailures(failed);
    if (revision) {
      await treeEdited(revision);
    }
    await reload();
    // After one photo the next step is to mark the people on it.
    if (chosen.length === 1 && created && failed.length === 0) {
      go(link("gallery", created, "edit"));
    }
  };

  const all = photos ?? [];
  const shown = filterPhotos(all);
  const filtered =
    unmarkedOnly.value ||
    chosenPeople.value.length > 0 ||
    chosenYear.value !== null ||
    chosenPlace.value !== null;
  const reset = (): void => {
    unmarkedOnly.value = false;
    chosenPeople.value = [];
    chosenYear.value = null;
    chosenPlace.value = null;
  };
  // The list of places and the map count the photos that pass the other filters.
  const beforePlace = filterGallery(all, {
    unmarked: unmarkedOnly.value,
    people: chosenPeople.value,
    year: chosenYear.value,
  });
  const places = placeOptions(beforePlace);
  const noPlace = beforePlace.filter((photo) => !photo.place).length;
  const anyPlace = all.some((photo) => photo.place);
  const onMap = layout.value === "map" && anyPlace;
  // The list of years counts the photos that pass the other filters.
  const years = yearOptions(
    filterGallery(all, {
      unmarked: unmarkedOnly.value,
      people: chosenPeople.value,
      place: chosenPlace.value,
    }),
  );
  return (
    <div class="gallery">
      <header class="page-head">
        <h1>{t("galleryTitle")}</h1>
      </header>
      <p class="page-lead">{canEdit ? t("galleryLead") : t("galleryLeadMember")}</p>
      {canEdit && files && (
        <div class="page-actions">
          <label class="button filled file-button">
            <Icon name="upload-simple" />
            {t("galleryUpload")}
            <input
              type="file"
              accept="image/jpeg,image/png"
              multiple
              disabled={progress !== null}
              onChange={(event) => void upload(event)}
            />
          </label>
          {progress && (
            <p role="status">{t("galleryUploading", { done: progress.done + 1, total: progress.total })}</p>
          )}
        </div>
      )}
      {failures.length > 0 && (
        <ul class="gallery-failures" role="alert">
          {failures.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
      )}
      {photos === null && !error && (
        <div class="center-state">
          <Spinner />
        </div>
      )}
      {error && <EmptyState icon="warning-circle" title={error} />}
      {photos && photos.length === 0 && <EmptyState icon="image" title={t("galleryEmpty")} />}
      {photos && photos.length > 0 && (
        <>
          <div class="gallery-filter">
            {anyPlace && (
              <div class="segmented compact" role="group" aria-label={t("galleryView")}>
                <button
                  type="button"
                  aria-pressed={!onMap}
                  onClick={() => {
                    layout.value = "grid";
                  }}
                >
                  {t("galleryViewGrid")}
                </button>
                <button
                  type="button"
                  aria-pressed={onMap}
                  onClick={() => {
                    layout.value = "map";
                    chosenPlace.value = null;
                  }}
                >
                  {t("galleryViewMap")}
                </button>
              </div>
            )}
            <button
              type="button"
              class="filter-toggle"
              aria-pressed={unmarkedOnly.value}
              onClick={() => {
                unmarkedOnly.value = !unmarkedOnly.value;
                chosenPeople.value = [];
              }}
            >
              {t("galleryUnmarked")}
            </button>
            {(years.length > 0 || chosenYear.value !== null) && (
              <select
                class="filter-select"
                aria-label={t("galleryYear")}
                value={chosenYear.value === null ? "" : String(chosenYear.value)}
                onChange={(event) => {
                  const value = event.currentTarget.value;
                  chosenYear.value = value === "" ? null : value === "none" ? "none" : Number(value);
                }}
              >
                <option value="">{t("galleryYearAll")}</option>
                {years.map((option) => (
                  <option key={option.year} value={String(option.year)}>
                    {option.year === "none"
                      ? t("galleryYearNone", { count: option.count })
                      : t("galleryYearOption", { year: option.year, count: option.count })}
                  </option>
                ))}
                {chosenYear.value !== null && !years.some((option) => option.year === chosenYear.value) && (
                  <option value={String(chosenYear.value)}>
                    {chosenYear.value === "none"
                      ? t("galleryYearNone", { count: 0 })
                      : t("galleryYearOption", { year: chosenYear.value, count: 0 })}
                  </option>
                )}
              </select>
            )}
            {!onMap && (places.length > 0 || chosenPlace.value !== null) && (
              <select
                class="filter-select"
                aria-label={t("galleryPlace")}
                value={chosenPlace.value ?? ""}
                onChange={(event) => {
                  chosenPlace.value = event.currentTarget.value || null;
                }}
              >
                <option value="">{t("galleryPlaceAll")}</option>
                {places.map((option) => (
                  <option key={option.key} value={option.key}>
                    {t("galleryPlaceOption", { name: placeWords(option.place), count: option.count })}
                  </option>
                ))}
                {noPlace > 0 && <option value="none">{t("galleryPlaceNone", { count: noPlace })}</option>}
                {chosenPlace.value !== null &&
                  chosenPlace.value !== "none" &&
                  !places.some((option) => option.key === chosenPlace.value) && (
                    <option value={chosenPlace.value}>{t("galleryPlaceGone")}</option>
                  )}
              </select>
            )}
            <PeopleFilter photos={all} shown={shown} />
            <span class="filter-count" role="status">
              {t("galleryCount", { count: shown.length })}
            </span>
          </div>
          {onMap ? (
            <div class="gallery-map">
              {places.length === 0 ? (
                <div class="gallery-none">
                  <p>{t("mapEmpty")}</p>
                  {filtered && <Button onClick={reset}>{t("galleryReset")}</Button>}
                </div>
              ) : (
                <>
                  <WorldMap
                    interactive
                    label={t("galleryMapLabel", { count: places.length })}
                    marks={places.map((option) => ({
                      id: option.key,
                      lat: option.lat,
                      lon: option.lon,
                      label: placeWords(option.place),
                      count: option.count,
                    }))}
                    onPick={(key) => {
                      chosenPlace.value = key;
                      layout.value = "grid";
                    }}
                  />
                  <MapCredit />
                  <p class="gallery-map-note">
                    {t("mapHint")}
                    {noPlace > 0 && ` ${t("mapNoPlace", { count: noPlace })}`}
                  </p>
                </>
              )}
            </div>
          ) : shown.length === 0 ? (
            <div class="gallery-none">
              <p>{filtered ? t("galleryNothing") : t("galleryNoPhotos")}</p>
              {filtered && <Button onClick={reset}>{t("galleryReset")}</Button>}
            </div>
          ) : (
            <ul class="gallery-grid">
              {shown.map((photo) => (
                <li key={photo.id}>
                  <a class="gallery-tile" href={link("gallery", photo.id)} title={photoTitle(photo)}>
                    <img
                      src={mediaImage(photo.id, "m")}
                      alt={photoTitle(photo)}
                      loading="lazy"
                      decoding="async"
                    />
                    {photo.faces.length > 0 && (
                      <span class="gallery-count" title={t("galleryMarked", { count: photo.faces.length })}>
                        <Icon name="users-three" />
                        {photo.faces.length}
                      </span>
                    )}
                    {photo.hidden && canEdit && (
                      <span class="gallery-hidden" title={t("galleryHiddenMark")}>
                        <Icon name="lock-simple" />
                      </span>
                    )}
                  </a>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </div>
  );
}

/** A square around a point of the picture, as a first place for the frame of a face. */
function regionAround(x: number, y: number, aspect: number): Region {
  // 18% of the short side, in percent of each side.
  const w = aspect >= 1 ? 18 / aspect : 18;
  const h = aspect >= 1 ? 18 : 18 * aspect;
  const left = Math.min(Math.max(x * 100 - w / 2, 0), 100 - w);
  const top = Math.min(Math.max(y * 100 - h / 2, 0), 100 - h);
  return {
    x1: Math.round(left),
    y1: Math.round(top),
    x2: Math.round(left + w),
    y2: Math.round(top + h),
  };
}

/** The place of a frame on the upright picture, from a stored part of the picture. */
function boxStyle(region: Region, orientation: number): Record<string, string> {
  const shown = toDisplay(region, orientation);
  return {
    left: `${shown.x1}%`,
    top: `${shown.y1}%`,
    width: `${shown.x2 - shown.x1}%`,
    height: `${shown.y2 - shown.y1}%`,
  };
}

/** One face that the server found: the face, the people it is like, and what to do with it. */
function FoundRow({
  photo,
  face,
  number,
  onMark,
  onOther,
  onSkip,
}: {
  photo: string;
  face: FoundFace;
  number: number;
  onMark: (person: string) => Promise<void>;
  onOther: () => void;
  onSkip: () => Promise<void>;
}) {
  const action = useAction();
  return (
    <div class="row found-row">
      <img class="found-face" src={mediaImage(photo, "s", face.region)} alt={t("foundFace", { number })} />
      <span class="row-text">
        {face.people.length > 0 ? (
          <>
            <small>{t("foundMaybe")}</small>
            <span class="found-people">
              {face.people.map((person) => (
                <Button
                  key={person.id}
                  busy={action.busy}
                  onClick={() => void action.run(() => onMark(person.id))}
                >
                  {fullName(person, locale.value) || t("unnamed")}
                </Button>
              ))}
            </span>
          </>
        ) : (
          <small>{t("foundUnknown")}</small>
        )}
        {action.error && (
          <small class="field-error" role="alert">
            {action.error}
          </small>
        )}
      </span>
      <span class="face-actions">
        <Button kind="plain" onClick={onOther}>
          {face.people.length > 0 ? t("foundOther") : t("foundChoose")}
        </Button>
        <Button kind="plain" busy={action.busy} onClick={() => void action.run(onSkip)}>
          {t("foundSkip")}
        </Button>
      </span>
    </div>
  );
}

interface Marking {
  person: string;
  region?: Region;
  /** The mark exists, and only its frame changes. */
  fixed: boolean;
}

/** The date of a draft. Empty fields of a date mean that the photo has no date. */
function draftDate(draft: DateDraft): Dated | null {
  const empty = (point: DateDraft["first"]): boolean => !point.day && !point.month && !point.year.trim();
  if (draft.kind !== "none" && draft.kind !== "text" && empty(draft.first) && empty(draft.second)) {
    return {};
  }
  return dateFromDraft(draft);
}

/** The form for the title, the date and the place of a photo, and for who can see it. */
function PhotoForm({ photo, onDone }: { photo: GalleryPhoto; onDone: (revision: number) => Promise<void> }) {
  const [title, setTitle] = useState(photo.description ?? "");
  const [open, setOpen] = useState(!photo.hidden);
  // The form opens with the date that the page shows: the date that somebody
  // wrote, or else the day from the file. Without any date it opens the
  // fields of an exact date, so a person sees where to type one.
  const fromFile = !photo.dated && photo.taken ? photo.taken.slice(0, 10) : "";
  const firstDraft = useMemo(
    () =>
      photo.dated
        ? draftFromDate(photo.dated)
        : fromFile
          ? draftFromDate({ dateval: { val: fromFile } })
          : { ...draftFromDate(undefined), kind: "exact" as const },
    [photo.id, photo.dated, fromFile],
  );
  const [draft, setDraft] = useState(firstDraft);
  const [dateError, setDateError] = useState(false);
  // The place that an editor chose. A place from the file is not a choice.
  const chosenPlace = photo.place?.manual ? photo.place : null;
  const [place, setPlace] = useState<PhotoPlace | null>(chosenPlace);
  const [confirm, setConfirm] = useState(false);
  const action = useAction();
  const date = draftDate(draft);
  const dateChanged = JSON.stringify(date) !== JSON.stringify(draftDate(firstDraft));
  const placeChanged = JSON.stringify(place) !== JSON.stringify(chosenPlace);
  const changed =
    title.trim() !== (photo.description ?? "") || open !== !photo.hidden || dateChanged || placeChanged;
  return (
    <section class="group">
      <h2>{t("photoForm")}</h2>
      <div class="group-body">
        <form
          class="form padded"
          onSubmit={(event) => {
            event.preventDefault();
            setDateError(!date);
            if (!date) {
              return;
            }
            void action.run(async () => {
              const result = await edit.galleryUpdate(
                photo.id,
                title.trim(),
                !open,
                dateChanged ? date : undefined,
                placeChanged ? place : undefined,
              );
              await onDone(result.revision);
            });
          }}
        >
          <Field label={t("photoName")} help={t("photoNameHelp")}>
            <input maxLength={120} value={title} onInput={(event) => setTitle(event.currentTarget.value)} />
          </Field>
          <div>
            <DateInput draft={draft} onChange={setDraft} />
            <p class="field-help">{fromFile ? t("photoDateFromFile") : t("photoDateHelp")}</p>
            {dateError && (
              <p class="field-error" role="alert">
                {t("errorDate")}
              </p>
            )}
          </div>
          <PlacePicker
            label={t("photoPlace")}
            help={
              !place && photo.place && !photo.place.manual
                ? t("photoPlaceFromFile", { name: placeWords(photo.place) })
                : t("photoPlaceHelp")
            }
            value={place}
            describe={placeWords}
            onChange={setPlace}
          />
          <div>
            <label class="check">
              <input
                type="checkbox"
                checked={open}
                onChange={(event) => setOpen(event.currentTarget.checked)}
              />
              <span>{t("photoGuests")}</span>
            </label>
            <p class="field-help">{t("photoGuestsHelp")}</p>
          </div>
          {action.error && (
            <p class="field-error" role="alert">
              {action.error}
            </p>
          )}
          <div class="actions">
            <Button type="submit" kind={changed ? "filled" : "tinted"} busy={action.busy} disabled={!changed}>
              {t("save")}
            </Button>
            {photo.uploaded &&
              (confirm ? (
                <Button
                  kind="danger"
                  busy={action.busy}
                  onClick={() =>
                    void action.run(async () => {
                      const result = await edit.galleryRemove(photo.id);
                      await treeEdited(result.revision);
                      go(link("gallery"));
                    })
                  }
                >
                  {t("photoRemoveConfirm")}
                </Button>
              ) : (
                <Button kind="quiet-danger" onClick={() => setConfirm(true)}>
                  {t("galleryRemove")}
                </Button>
              ))}
          </div>
        </form>
      </div>
    </section>
  );
}

/** What is known about a photo: when and where it was made, with what, and who added it. */
function PhotoFacts({ photo }: { photo: GalleryPhoto }) {
  const taken = takenDate(photo.taken);
  const rows: { label: string; value: ComponentChildren }[] = [];
  // The date that an editor wrote stands under the title of the page.
  const written = formatDate(photo.date, locale.value);
  if (taken) {
    rows.push({
      label: written ? t("factFileTime") : t("factTaken"),
      value: new Intl.DateTimeFormat(locale.value, {
        dateStyle: "long",
        timeStyle: "short",
        timeZone: "UTC",
      }).format(taken),
    });
  }
  if (photo.place) {
    rows.push({ label: t("factPlace"), value: placeWords(photo.place) });
  }
  if (photo.addedBy) {
    const when = photo.addedAt
      ? new Intl.DateTimeFormat(locale.value, { dateStyle: "long" }).format(new Date(photo.addedAt * 1000))
      : "";
    rows.push({ label: t("factAddedBy"), value: when ? `${photo.addedBy}, ${when}` : photo.addedBy });
  }
  if (rows.length === 0) {
    return null;
  }
  return (
    <section class="group">
      <h2>{t("photoAbout")}</h2>
      <dl class="group-body facts">
        {rows.map((row) => (
          <div class="fact" key={row.label}>
            <dt>{row.label}</dt>
            <dd>{row.value}</dd>
          </div>
        ))}
        {photo.place && (
          <div class="fact-map">
            <WorldMap
              size="small"
              label={t("photoMapLabel", { name: placeWords(photo.place) })}
              marks={[
                { id: photo.id, lat: photo.place.lat, lon: photo.place.lon, label: placeWords(photo.place) },
              ]}
            />
            <MapCredit />
            <a class="fact-link" href={mapLink(photo.place)} target="_blank" rel="noopener noreferrer">
              {t("factMap")}
            </a>
          </div>
        )}
      </dl>
    </section>
  );
}

/**
 * One photo with the people who are marked on it. The page opens for looking:
 * the picture, the people and the download. An editor turns on the edit mode
 * to mark faces and to change the title.
 */
export function PhotoView({ id, editing: asked }: { id: string; editing?: boolean }) {
  const { photos, error, reload } = useGallery();
  const [aspect, setAspect] = useState(0);
  const [marking, setMarking] = useState<Marking | null>(null);
  const [full, setFull] = useState(false);
  // The marks are stored on the pixels of the file. The page shows the upright picture.
  const [orientation, setOrientation] = useState(1);
  const [found, setFound] = useState<FoundFaces | null>(null);
  // The reader asked to see the frames of the marked faces.
  const [frames, setFrames] = useState(false);
  // The person whose row is under the pointer. The frame of that face shows.
  const [lit, setLit] = useState("");
  const picture = useRef<HTMLImageElement>(null);
  const action = useAction();
  const canEdit = Boolean(me.value?.edit);
  const editing = canEdit && Boolean(asked);
  const self = me.value?.user.person ?? "";

  useEffect(() => {
    let active = true;
    setOrientation(1);
    void mediaOrientation(id).then((value) => {
      if (active) {
        setOrientation(value);
      }
    });
    return () => {
      active = false;
    };
  }, [id]);

  // In the edit mode an editor sees the faces that the server found. While
  // the server still searches the photo, the page asks again.
  const loadFound = async (): Promise<FoundFaces | null> => {
    if (!editing) {
      return null;
    }
    try {
      const answer = await server.foundFaces(id);
      setFound(answer);
      return answer;
    } catch {
      setFound(null);
      return null;
    }
  };
  useEffect(() => {
    let active = true;
    let timer = 0;
    let tries = 0;
    setFound(null);
    const ask = async (): Promise<void> => {
      const answer = await loadFound();
      tries++;
      if (active && answer?.on && !answer.scanned && tries < 40) {
        timer = window.setTimeout(() => void ask(), 2500);
      }
    };
    void ask();
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [id, editing]);

  if (error) {
    return (
      <div class="center-state">
        <EmptyState icon="warning-circle" title={error} />
      </div>
    );
  }
  if (photos === null) {
    return (
      <div class="center-state">
        <Spinner />
      </div>
    );
  }
  // The arrows step through the list that the gallery shows. A photo outside that list steps through all pictures.
  const listed = filterPhotos(photos);
  const walk = listed.some((item) => item.id === id) ? listed : photos;
  const index = walk.findIndex((item) => item.id === id);
  const photo = walk[index];
  if (!photo) {
    return (
      <div class="center-state">
        <EmptyState icon="image" title={t("photoMissing")}>
          <a class="button" href={link("gallery")}>
            {t("galleryTitle")}
          </a>
        </EmptyState>
      </div>
    );
  }
  const title = photoTitle(photo);
  const date = formatDate(photo.date, locale.value);
  const previous = walk[index - 1];
  const next = walk[index + 1];
  // The arrows keep the mode of the page.
  const page = (other: string): string => (editing ? link("gallery", other, "edit") : link("gallery", other));

  const done = async (revision: number): Promise<void> => {
    await treeEdited(revision);
    await reload();
    await loadFound();
  };
  const open = editing ? (found?.faces ?? []) : [];
  const marked = photo.faces.some((face) => face.region);

  /** A press on the picture starts a mark at that place. */
  const pressed = (event: MouseEvent): void => {
    const rect = picture.current?.getBoundingClientRect();
    if (!editing || !aspect || !rect?.width || !rect.height) {
      return;
    }
    const x = (event.clientX - rect.left) / rect.width;
    const y = (event.clientY - rect.top) / rect.height;
    setMarking({ person: "", region: toStored(regionAround(x, y, aspect), orientation), fixed: false });
  };

  const frameClass = ["photo-frame", editing ? "can-mark" : "", editing || frames ? "frames" : ""]
    .filter(Boolean)
    .join(" ");

  return (
    <div class="gallery photo-page">
      <a class="back" href={link("gallery")}>
        <Icon name="caret-left" />
        {t("galleryTitle")}
      </a>
      <header class="page-head photo-head">
        <div>
          <h1>{title}</h1>
          {date && <p class="page-lead">{date}</p>}
        </div>
        <div class="photo-steps">
          <a
            class="tool"
            aria-label={t("viewerPrevious")}
            aria-disabled={!previous}
            href={previous ? page(previous.id) : undefined}
          >
            <Icon name="caret-left" />
          </a>
          <span class="viewer-count">{t("viewerCount", { index: index + 1, count: walk.length })}</span>
          <a
            class="tool"
            aria-label={t("viewerNext")}
            aria-disabled={!next}
            href={next ? page(next.id) : undefined}
          >
            <Icon name="caret-right" />
          </a>
        </div>
      </header>

      <div class="photo-stage">
        <div class={frameClass}>
          {/* biome-ignore lint/a11y/useKeyWithClickEvents: the button below starts a mark without a pointer */}
          <img
            ref={picture}
            key={photo.id}
            src={mediaImage(photo.id, "l")}
            alt={title}
            onLoad={(event) =>
              setAspect(event.currentTarget.naturalWidth / event.currentTarget.naturalHeight)
            }
            onClick={pressed}
          />
          {photo.faces.map((face) =>
            face.region ? (
              <a
                key={face.person.id}
                class={lit === face.person.id ? "face-box lit" : "face-box"}
                href={link("people", face.person.id)}
                aria-label={t("faceOnPhoto", { name: personName(face) })}
                style={boxStyle(face.region, orientation)}
              >
                <span class="face-name">{personName(face)}</span>
              </a>
            ) : null,
          )}
          {open.map((face, position) => (
            <button
              key={face.id}
              type="button"
              class="face-box found"
              aria-label={t("foundFace", { number: position + 1 })}
              style={boxStyle(face.region, orientation)}
              onClick={() =>
                setMarking({ person: face.people[0]?.id ?? "", region: face.region, fixed: false })
              }
            />
          ))}
        </div>
      </div>

      {editing ? (
        <>
          <div class="page-actions photo-actions">
            <Button kind="filled" onClick={() => setMarking({ person: "", fixed: false })}>
              {t("photoMark")}
            </Button>
            <a class="button" href={link("gallery", photo.id)}>
              {t("done")}
            </a>
          </div>
          <p class="group-note photo-hint">{t("photoMarkHint")}</p>
        </>
      ) : (
        <div class="page-actions photo-actions">
          <Button onClick={() => setFull(true)}>{t("photoFull")}</Button>
          {marked && (
            <button
              type="button"
              class="button plain"
              aria-pressed={frames}
              onClick={() => setFrames(!frames)}
            >
              {frames ? t("photoFramesOff") : t("photoFramesOn")}
            </button>
          )}
          <a class="button plain" href={mediaFile(photo.id)} download>
            <Icon name="download-simple" />
            {t("downloadOriginal")}
          </a>
          {canEdit && (
            <a class="button plain photo-edit-link" href={link("gallery", photo.id, "edit")}>
              {t("edit")}
            </a>
          )}
        </div>
      )}

      {(editing || photo.faces.length > 0) && (
        <section class="group">
          <h2>{t("photoFaces")}</h2>
          <div class="group-body">
            {photo.faces.length === 0 && <p class="row-empty">{t("photoNoFaces")}</p>}
            {photo.faces.map((face) => {
              const isPortrait = face.person.photo?.id === photo.id;
              // Without the edit mode a reader chooses only the own portrait.
              const offerPortrait =
                Boolean(face.region) &&
                !isPortrait &&
                canPortrait(face.person.id) &&
                (editing || face.person.id === self);
              return (
                // biome-ignore lint/a11y/noStaticElementInteractions: the pointer on a row only shows the frame of that face
                <div
                  class="row face-row"
                  key={face.person.id}
                  onMouseEnter={() => setLit(face.person.id)}
                  onMouseLeave={() => setLit("")}
                >
                  <Avatar card={face.person} size="s" />
                  <span class="row-text">
                    <a href={link("people", face.person.id)}>{personName(face)}</a>
                    {editing && isPortrait && <small>{t("faceIsPortrait")}</small>}
                  </span>
                  {(offerPortrait || editing) && (
                    <span class="face-actions">
                      {offerPortrait && (
                        <Button
                          kind="plain"
                          busy={action.busy}
                          onClick={() =>
                            void action.run(async () => {
                              const result = await edit.setPhoto(
                                face.person.id,
                                photo.id,
                                face.region ?? null,
                                true,
                              );
                              await done(result.revision);
                            })
                          }
                        >
                          {face.person.id === self ? t("faceMyPortrait") : t("photoMakePortrait")}
                        </Button>
                      )}
                      {editing && (
                        <Button
                          kind="plain"
                          onClick={() =>
                            setMarking({ person: face.person.id, region: face.region, fixed: true })
                          }
                        >
                          {t("photoMove")}
                        </Button>
                      )}
                      {editing && (
                        <Button
                          kind="quiet-danger"
                          busy={action.busy}
                          onClick={() =>
                            void action.run(async () => {
                              const result = await edit.unmarkFace(photo.id, face.person.id);
                              await done(result.revision);
                            })
                          }
                        >
                          {t("photoUnmark")}
                        </Button>
                      )}
                    </span>
                  )}
                </div>
              );
            })}
            {action.error && (
              <p class="field-error padded" role="alert">
                {action.error}
              </p>
            )}
          </div>
          {editing && photo.hidden && <p class="group-note">{t("galleryHiddenMark")}</p>}
        </section>
      )}

      {!editing && <PhotoFacts photo={photo} />}

      {editing && found?.on && (
        <section class="group">
          <h2>{t("foundTitle")}</h2>
          <div class="group-body">
            {!found.scanned && <p class="row-empty">{t("foundSearching")}</p>}
            {found.scanned && open.length === 0 && <p class="row-empty">{t("foundNone")}</p>}
            {open.map((face, position) => (
              <FoundRow
                key={face.id}
                photo={photo.id}
                face={face}
                number={position + 1}
                onMark={async (person) => {
                  const result = await edit.markFace(photo.id, person, face.region);
                  await done(result.revision);
                }}
                onOther={() => setMarking({ person: "", region: face.region, fixed: false })}
                onSkip={async () => {
                  await server.dismissFace(photo.id, face.id);
                  await loadFound();
                }}
              />
            ))}
          </div>
          {found.scanned && !found.recognition && <p class="group-note">{t("foundNoRecognition")}</p>}
        </section>
      )}

      {editing && <PhotoForm key={photo.id} photo={photo} onDone={done} />}

      {marking && (
        <CropDialog
          media={photo.id}
          region={marking.region}
          face
          ready={Boolean(marking.person)}
          onClose={() => setMarking(null)}
          onSave={async (region) => {
            if (!region || !marking.person) {
              return;
            }
            const result = await edit.markFace(photo.id, marking.person, region);
            setMarking(null);
            await done(result.revision);
          }}
        >
          <PersonPicker
            label={t("faceWho")}
            value={marking.person}
            locked={marking.fixed}
            onChange={(person) => setMarking({ ...marking, person })}
          />
        </CropDialog>
      )}
      {full && (
        <Viewer
          items={[
            {
              id: photo.id,
              description: photo.description,
              mime: "image/jpeg",
              image: true,
              date: photo.date,
            },
          ]}
          start={0}
          onClose={() => setFull(false)}
        />
      )}
    </div>
  );
}
