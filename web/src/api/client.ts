import type {
  AccessUser,
  AuditEntry,
  Bundle,
  ChangeEntry,
  Dated,
  DocumentView,
  EditPerson,
  EditResult,
  EventInput,
  FoundFaces,
  FoundPlace,
  GalleryPhoto,
  Graph,
  ImportPreview,
  Me,
  PersonDetail,
  PersonInput,
  PhotoPlace,
  Region,
  RelationKind,
  Role,
  SessionInfo,
  SyncState,
} from "./types";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
  ) {
    super(code);
  }
}

async function send(path: string, init?: RequestInit): Promise<Response> {
  let response: Response;
  try {
    response = await fetch(path, { credentials: "same-origin", ...init });
  } catch {
    throw new ApiError(0, "network");
  }
  if (!response.ok) {
    let code = "internal";
    try {
      code = ((await response.json()) as { error?: string }).error ?? code;
    } catch {
      // The body is not JSON. The generic code stays.
    }
    throw new ApiError(response.status, code);
  }
  return response;
}

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  return (await (await send(path, init)).json()) as T;
}

function post<T>(path: string, body: unknown): Promise<T> {
  return json<T>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

function remove(path: string): Promise<void> {
  return send(path, { method: "DELETE" }).then(() => undefined);
}

function put<T>(path: string, body: unknown): Promise<T> {
  return json<T>(path, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

function removeFor<T>(path: string): Promise<T> {
  return json<T>(path, { method: "DELETE" });
}

function patch<T>(path: string, body: unknown): Promise<T> {
  return json<T>(path, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

/** The read operations that every data source has. */
export interface DataSource {
  graph(): Promise<Graph>;
  person(id: string): Promise<PersonDetail>;
}

export const server = {
  status: () => json<{ needsSetup: boolean; codesInLog?: boolean }>("/api/v1/status"),
  me: () => json<Me>("/api/v1/me"),
  /** Asks the server to send a sign-in code. The answer does not say whether the address has an account. */
  requestCode: (email: string, lang: string) =>
    post<{ minutes: number }>("/api/v1/auth/code", { email, lang }),
  verifyCode: (code: string) => post<Me>("/api/v1/auth/verify", { code }),
  logout: () => post<unknown>("/api/v1/auth/logout", {}),
  setup: (token: string, email: string, displayName: string, lang: string) =>
    post<Me>("/api/v1/setup", { token, email, displayName, lang }),
  updateMe: (change: { displayName?: string; lang?: string; person?: string }) =>
    patch<Me>("/api/v1/me", change),
  sessions: () => json<{ sessions: SessionInfo[] }>("/api/v1/me/sessions"),
  gallery: () => json<{ photos: GalleryPhoto[]; files: boolean }>("/api/v1/gallery"),
  /** The faces of a photo that the server found and that have no mark. */
  foundFaces: (photo: string) => json<FoundFaces>(`/api/v1/gallery/${id(photo)}/found`),
  /** Finds places for a photo by the start of a name. */
  findPlaces: (text: string) =>
    json<{ places: FoundPlace[] }>(`/api/v1/places/search?q=${encodeURIComponent(text)}`).then(
      (answer) => answer.places,
    ),
  /** The towns and the villages inside a part of the globe, the largest first. A close map names them. */
  placesAround: (box: { south: number; west: number; north: number; east: number }) =>
    json<{ places: FoundPlace[] }>(
      `/api/v1/places/around?south=${box.south}&west=${box.west}&north=${box.north}&east=${box.east}`,
    ).then((answer) => answer.places),
  /** The settlement of a point that an editor pressed on the map, or null far from every settlement. */
  placeNearest: (lat: number, lon: number) =>
    json<{ place: FoundPlace | null }>(`/api/v1/places/nearest?lat=${lat}&lon=${lon}`).then(
      (answer) => answer.place,
    ),
  /** Says that a found face needs no mark. */
  dismissFace: (photo: string, face: number) => remove(`/api/v1/gallery/${id(photo)}/found/${face}`),
  endSession: (id: string) => remove(`/api/v1/me/sessions/${encodeURIComponent(id)}`),
  endOtherSessions: () => post<unknown>("/api/v1/me/sessions/end-others", {}),

  graph: () => json<Graph>("/api/v1/tree"),
  person: (id: string) => json<PersonDetail>(`/api/v1/people/${encodeURIComponent(id)}`),

  uploadImport: (file: File) =>
    json<ImportPreview>("/api/v1/imports", {
      method: "POST",
      headers: { "Content-Type": "application/octet-stream" },
      body: file,
    }),
  commitImport: (id: string, replace: boolean) =>
    post<{ living: number }>(`/api/v1/imports/${encodeURIComponent(id)}/commit`, { replace }),
  cancelImport: (id: string) => remove(`/api/v1/imports/${encodeURIComponent(id)}`),

  users: () => json<{ users: AccessUser[] }>("/api/v1/users"),
  invite: (email: string, displayName: string, role: Role, lang: string, person = "", ownBranch = false) =>
    post<AccessUser>("/api/v1/users", { email, displayName, role, lang, person, ownBranch }),
  inviteAgain: (id: number) => post<unknown>(`/api/v1/users/${id}/invite`, {}),
  updateUser: (
    id: number,
    change: { role?: Role; email?: string; displayName?: string; person?: string; ownBranch?: boolean },
  ) => patch<AccessUser>(`/api/v1/users/${id}`, change),
  deleteUser: (id: number) => remove(`/api/v1/users/${id}`),
  audit: () => json<{ entries: AuditEntry[] }>("/api/v1/audit"),
  sync: () => json<SyncState>("/api/v1/sync"),
  syncNow: () => post<SyncState>("/api/v1/sync", {}),
};

const id = encodeURIComponent;

/** The operations that change the tree. Only an editor can call them. */
export const edit = {
  person: (person: string) => json<EditPerson>(`/api/v1/edit/people/${id(person)}`),
  places: () => json<{ places: { id: string; name: string }[] }>("/api/v1/edit/places"),
  createPerson: (person: PersonInput, relation?: { person: string; as: RelationKind; family?: string }) =>
    post<EditResult>("/api/v1/edit/people", { person, relation: relation ?? null }),
  updatePerson: (person: string, rev: number, input: PersonInput) =>
    put<EditResult>(`/api/v1/edit/people/${id(person)}`, { rev, person: input }),
  deletePerson: (person: string) => removeFor<EditResult>(`/api/v1/edit/people/${id(person)}`),
  link: (person: string, as: RelationKind, other: string, family?: string) =>
    post<EditResult>("/api/v1/edit/links", { person, as, other, family: family ?? "" }),
  unlink: (family: string, person: string) =>
    removeFor<EditResult>(`/api/v1/edit/families/${id(family)}/members/${id(person)}`),
  setFamilyType: (family: string, rel: string) =>
    put<EditResult>(`/api/v1/edit/families/${id(family)}`, { rel }),
  addEvent: (owner: "people" | "families", ownerId: string, event: EventInput) =>
    post<EditResult>(`/api/v1/edit/${owner}/${id(ownerId)}/events`, { rev: 0, event }),
  updateEvent: (event: string, rev: number, input: EventInput, person: string) =>
    put<EditResult>(`/api/v1/edit/events/${id(event)}?person=${id(person)}`, { rev, event: input }),
  removeEvent: (owner: "people" | "families", ownerId: string, event: string) =>
    removeFor<EditResult>(`/api/v1/edit/${owner}/${id(ownerId)}/events/${id(event)}`),
  /** `hidden` makes a note for editors and administrators only. Only an editor can send it. */
  addNote: (person: string, text: string, hidden?: boolean) =>
    post<EditResult>(`/api/v1/edit/people/${id(person)}/notes`, {
      rev: 0,
      text,
      ...(hidden === undefined ? {} : { hidden }),
    }),
  updateNote: (note: string, rev: number, text: string, person: string, hidden?: boolean) =>
    put<EditResult>(`/api/v1/edit/notes/${id(note)}?person=${id(person)}`, {
      rev,
      text,
      ...(hidden === undefined ? {} : { hidden }),
    }),
  removeNote: (person: string, note: string) =>
    removeFor<EditResult>(`/api/v1/edit/people/${id(person)}/notes/${id(note)}`),
  addPhoto: (person: string, file: File) =>
    json<EditResult>(`/api/v1/edit/people/${id(person)}/photo`, {
      method: "POST",
      headers: { "Content-Type": "application/octet-stream" },
      body: file,
    }),
  /** Sets the part of a picture that the person shows. `primary` makes the picture the portrait. */
  setPhoto: (person: string, media: string, region: Region | null, primary = false) =>
    put<EditResult>(`/api/v1/edit/people/${id(person)}/media/${id(media)}`, { region, primary }),
  /** Leaves the person without a portrait. The pictures stay. */
  clearPortrait: (person: string) => removeFor<EditResult>(`/api/v1/edit/people/${id(person)}/portrait`),
  removeMedia: (person: string, media: string) =>
    removeFor<EditResult>(`/api/v1/edit/people/${id(person)}/media/${id(media)}`),
  /** Adds a picture to the gallery. It has no person yet. */
  galleryAdd: (file: File) =>
    json<EditResult>("/api/v1/edit/gallery", {
      method: "POST",
      headers: { "Content-Type": "application/octet-stream" },
      body: file,
    }),
  /**
   * Changes the title of a photo and who sees it. `date` and `place` go to
   * the server only when the editor changed them; a place of null takes the
   * chosen place away.
   */
  galleryUpdate: (
    photo: string,
    description: string,
    hidden: boolean,
    date?: Dated,
    place?: PhotoPlace | null,
  ) =>
    put<EditResult>(`/api/v1/edit/gallery/${id(photo)}`, {
      description,
      hidden,
      ...(date === undefined ? {} : { date }),
      ...(place === undefined
        ? {}
        : {
            place:
              place === null
                ? { remove: true }
                : {
                    lat: place.lat,
                    lon: place.lon,
                    name: place.name ?? "",
                    nameRu: place.nameRu ?? "",
                    country: place.country ?? "",
                  },
          }),
    }),
  galleryRemove: (photo: string) => removeFor<EditResult>(`/api/v1/edit/gallery/${id(photo)}`),
  /** Says where a person is on a photo. A second mark of the same person moves the first. */
  markFace: (photo: string, person: string, region: Region) =>
    put<EditResult>(`/api/v1/edit/gallery/${id(photo)}/faces/${id(person)}`, { region }),
  unmarkFace: (photo: string, person: string) =>
    removeFor<EditResult>(`/api/v1/edit/gallery/${id(photo)}/faces/${id(person)}`),
  setHome: (person: string) => put<EditResult>("/api/v1/edit/home", { person }),
  history: (person?: string) =>
    json<{ entries: ChangeEntry[] }>(`/api/v1/history${person ? `?person=${id(person)}` : ""}`),
  undo: (change: number) => post<EditResult>(`/api/v1/history/${change}/undo`, {}),
};

/**
 * Returns the address of a picture in one of three sizes. The static demo has
 * no files, so there the result is empty.
 */
export function mediaImage(id: string, size: "s" | "m" | "l", region?: Region): string {
  if (__DEMO__) {
    return "";
  }
  const params = new URLSearchParams({ size });
  if (region) {
    params.set("region", `${region.x1},${region.y1},${region.x2},${region.y2}`);
  }
  return `/api/v1/media/${encodeURIComponent(id)}/image?${params.toString()}`;
}

/** Returns the address that downloads the original file. */
export function mediaFile(id: string): string {
  return `/api/v1/media/${encodeURIComponent(id)}/file`;
}

/** How the camera held a picture: an EXIF orientation from 1 to 8. A stored part of a picture needs it. */
export async function mediaOrientation(media: string): Promise<number> {
  try {
    const info = await json<{ orientation: number }>(`/api/v1/media/${encodeURIComponent(media)}/info`);
    return info.orientation;
  } catch {
    // Without the answer the picture counts as upright.
    return 1;
  }
}

/** The text of a Word or text file. */
export function mediaDocument(id: string, signal?: AbortSignal): Promise<DocumentView> {
  return json<DocumentView>(`/api/v1/media/${encodeURIComponent(id)}/document`, { signal });
}

/** A read-only source backed by one static JSON bundle. The public demo uses it. */
export async function loadBundle(url: string): Promise<DataSource> {
  const response = await fetch(url);
  if (!response.ok) {
    throw new ApiError(response.status, "not_found");
  }
  const bundle = (await response.json()) as Bundle;
  return {
    graph: () => Promise.resolve(bundle.graph),
    person: (id) => {
      const detail = bundle.people[id];
      return detail ? Promise.resolve(detail) : Promise.reject(new ApiError(404, "not_found"));
    },
  };
}
