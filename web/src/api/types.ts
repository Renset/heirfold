// These types mirror the JSON that the Go server sends.

export type DateForm = "exact" | "before" | "after" | "about" | "range" | "span" | "text";

export interface DatePoint {
  y: number;
  m?: number;
  d?: number;
}

export interface DateView {
  form: DateForm;
  start?: DatePoint;
  stop?: DatePoint;
  text?: string;
  calendar?: string;
  quality?: string;
}

/** A part of an image, in percent of its width and height. */
export interface Region {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

export interface Photo {
  id: string;
  region?: Region;
}

export interface MediaView {
  id: string;
  description?: string;
  mime?: string;
  /** The server can show this file as a picture. */
  image?: boolean;
  date?: DateView;
  region?: Region;
}

/** A person who is marked on a photo of the gallery. */
export interface GalleryFace {
  person: Card;
  /** The part of the picture with the face, in percent. */
  region?: Region;
}

/** Where a photo was made: the point from the file, and the name of the town or the village near it. */
export interface PhotoPlace {
  lat: number;
  lon: number;
  /** The usual international name of the settlement. Missing for a place far from every settlement. */
  name?: string;
  /** The Russian name of the settlement. */
  nameRu?: string;
  /** The code of the country in two letters. */
  country?: string;
  /** An editor chose the place. It wins over the place from the file. */
  manual?: boolean;
}

/** One answer of the search for a place: a settlement of the list of the server, or a place of the tree. */
export interface FoundPlace {
  name: string;
  nameRu?: string;
  country?: string;
  lat: number;
  lon: number;
  /** The place is a place of the family tree. */
  tree?: boolean;
  /** The number of people who live there. */
  people?: number;
}

/** One picture of the photo gallery. */
export interface GalleryPhoto {
  id: string;
  description?: string;
  date?: DateView;
  /** Guests cannot see the photo. */
  hidden?: boolean;
  /** The file came through Heirfold, so the photo can leave the gallery. */
  uploaded?: boolean;
  /** The time of the picture on the clock of the camera, as "2006-01-02T15:04:05". It comes from the file. */
  taken?: string;
  /** The year for the filter: from the date that an editor wrote, or else from the file. */
  year?: number;
  /** Where the camera was, if the file says it. */
  place?: PhotoPlace;
  /** The date of the photo in the form of the tree, for the edit form. */
  dated?: Dated;
  /** Who brought the file through Heirfold, and when (seconds). */
  addedBy?: string;
  addedAt?: number;
  faces: GalleryFace[];
}

/** A face that the server found on a photo and that has no mark yet. */
export interface FoundFace {
  id: number;
  /** A square around the face, in percent of the stored picture. */
  region: Region;
  /** The people whose marked faces are like this face, the most alike first. */
  people: Card[];
}

/** What the server found on one photo. */
export interface FoundFaces {
  /** False while the server did not search the photo yet. */
  scanned: boolean;
  faces: FoundFace[];
  /** The server searches photos for faces. */
  on: boolean;
  /** The server can say who a face is like. */
  recognition: boolean;
}

/** One piece of text of a document with one format. */
export interface DocRun {
  text?: string;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strike?: boolean;
  script?: "sup" | "sub";
  link?: string;
  /** Colors as #RRGGBB: of the text and behind the text. */
  color?: string;
  fill?: string;
  /** The size of the text relative to the body text. */
  scale?: number;
  /** The number of a note. Such a run has no text. */
  note?: number;
}

export interface DocCell {
  span?: number;
  fill?: string;
  blocks: DocBlock[];
}

export interface DocBlock {
  kind: "p" | "h" | "item" | "table" | "text";
  level?: number;
  marker?: string;
  align?: "center" | "end" | "justify";
  fill?: string;
  runs?: DocRun[];
  rows?: { cells: DocCell[] }[];
}

/** The text of a Word or text file, as the server reads it. */
export interface DocumentView {
  kind: "docx" | "text";
  blocks: DocBlock[];
  notes?: { number: number; blocks: DocBlock[] }[];
  pictures?: number;
  truncated?: boolean;
}

export interface Card {
  id: string;
  given?: string;
  /** The surname that the person carried last: from the latest marriage, or else from the primary name. */
  surname?: string;
  /** The surname at birth, when it differs from `surname`. */
  birthSurname?: string;
  /** `surname` comes from a name from a marriage. */
  married?: boolean;
  patronymic?: string;
  gender?: string;
  birth?: DateView;
  death?: DateView;
  living?: boolean;
  photo?: Photo;
}

export interface GraphFamily {
  id: string;
  father?: string;
  mother?: string;
  children?: string[];
  rel?: string;
  married?: DateView;
}

export interface Graph {
  home?: string;
  people: Card[];
  families: GraphFamily[];
}

export interface PlaceBrief {
  id: string;
  name: string;
  lat?: number;
  long?: number;
}

export interface NoteStyle {
  name: string;
  value?: string;
  ranges: { start: number; end: number }[];
}

export interface NoteView {
  id: string;
  type?: string;
  text: string;
  styles?: NoteStyle[];
  /** Only editors and administrators read the note. */
  hidden?: boolean;
}

export interface EventView {
  id: string;
  type: string;
  role?: string;
  date?: DateView;
  /** Orders events in time: year, month and day as one number. Missing for an event without a date. */
  sort?: number;
  place?: PlaceBrief;
  description?: string;
  cause?: string;
  notes?: NoteView[];
  citations?: string[];
  media?: MediaView[];
}

export interface NameView {
  type?: string;
  given?: string;
  surname?: string;
  patronymic?: string;
  nick?: string;
  title?: string;
  suffix?: string;
}

export interface CitationView {
  id: string;
  page?: string;
  confidence: number;
  date?: DateView;
  sourceId?: string;
  title?: string;
  author?: string;
  pubinfo?: string;
  notes?: NoteView[];
  media?: MediaView[];
}

export interface Parents {
  familyId: string;
  father?: Card;
  mother?: Card;
  siblings?: Card[];
}

export interface Union {
  familyId: string;
  rel?: string;
  spouse?: Card;
  events?: EventView[];
  children?: Card[];
  notes?: NoteView[];
  media?: MediaView[];
}

export interface PersonDetail extends Card {
  private?: boolean;
  names?: NameView[];
  events?: EventView[];
  parents?: Parents[];
  unions?: Union[];
  associations?: { rel?: string; person: Card }[];
  attributes?: { type: string; value: string }[];
  links?: { href: string; description?: string }[];
  notes?: NoteView[];
  citations?: CitationView[];
  media?: MediaView[];
  tags?: { name: string; color?: string }[];
}

export type Role = "disabled" | "guest" | "member" | "editor" | "admin";

export interface User {
  id: number;
  email: string;
  displayName: string;
  role: Role;
  createdAt: number;
  lastLoginAt?: number;
  /** The ID of the person in the tree who owns the account. */
  person?: string;
  /** An administrator let this family member change the own branch. */
  ownBranch?: boolean;
}

export interface Me {
  user: User;
  viewPrivate: boolean;
  viewLiving: boolean;
  /** The user can change the tree now. False while the tree follows Gramps. */
  edit: boolean;
  /** The user can change the own branch now: the person of the account, the descendants and the partners. */
  editOwn: boolean;
  /** The user can choose the own portrait now. */
  pictures: boolean;
  /** The user can download the whole tree. */
  export: boolean;
  /** The user can open the photo gallery. */
  gallery: boolean;
  /** The server gives the pictures of a map with streets. */
  mapTiles?: boolean;
  /** The line that names the makers of those pictures. */
  mapCredit?: string;
  /** The language that the user chose, or an empty text. */
  lang: string;
  /** The user can invite people. */
  invite: boolean;
  manageUsers: boolean;
  importTree: boolean;
  /** The user can manage the tree. With a Gramps tree, importTree is false. */
  manageTree: boolean;
  /** "gramps" when the tree follows a Gramps database, else "heirfold". */
  source: "gramps" | "heirfold";
  /** The time of the last sign-in code of this session, in seconds. */
  authAt: number;
}

/** One line of the list of people with access. */
export interface AccessUser {
  id: number;
  displayName: string;
  role: Role;
  /** Empty when the viewer has no right to see the address. */
  email?: string;
  createdAt: number;
  lastLoginAt?: number;
  /** The person has not signed in yet. */
  pending?: boolean;
  /** The viewer sent this invitation. */
  mine?: boolean;
  self?: boolean;
  /** The ID of the person in the tree who owns the account. */
  person?: string;
  /** This family member can change the own branch. */
  ownBranch?: boolean;
}

/** One signed-in browser of the user. */
export interface SessionInfo {
  id: string;
  label: string;
  createdAt: number;
  lastSeenAt: number;
  current?: boolean;
}

export interface SyncStatus {
  source: string;
  name?: string;
  checkedAt?: number;
  syncedAt?: number;
  counts?: Counts;
  living: number;
  warnings: number;
  error?: string;
}

export interface SyncState {
  /** A number that grows with every change of the tree. */
  revision: number;
  source: "gramps" | "upload";
  status?: SyncStatus;
}

export interface AuditEntry {
  id: number;
  at: number;
  actor: string;
  action: string;
  target?: string;
  detail?: string;
  ip?: string;
}

export interface Counts {
  people: number;
  families: number;
  events: number;
  places: number;
  sources: number;
  citations: number;
  repositories: number;
  notes: number;
  media: number;
  tags: number;
}

export interface ImportWarning {
  code: string;
  kind?: string;
  object?: string;
  target?: string;
}

export interface ImportPreview {
  id: string;
  counts: Counts;
  warnings: ImportWarning[];
  warningsTotal: number;
  replaces?: Counts;
}

export interface Bundle {
  format: number;
  role: string;
  graph: Graph;
  people: Record<string, PersonDetail>;
}

/** A date in the form that the tree stores. At most one field is set. */
export interface DateVal {
  val: string;
  type?: string;
  quality?: string;
  cformat?: string;
  dualdated?: boolean;
  newyear?: string;
}

export interface DatePair {
  start: string;
  stop: string;
  quality?: string;
  cformat?: string;
  dualdated?: boolean;
  newyear?: string;
}

export interface Dated {
  dateval?: DateVal;
  daterange?: DatePair;
  datespan?: DatePair;
  datestr?: { val: string };
}

/** One name of a person in the edit form. index is -1 for a new name. */
export interface NameInput {
  index: number;
  type: string;
  given: string;
  surname: string;
  patronymic: string;
  suffix: string;
  title: string;
  nick: string;
  call: string;
}

export interface PersonInput {
  gender: string;
  private: boolean;
  names: NameInput[];
}

export interface PlaceInput {
  id?: string;
  name?: string;
}

export interface EventInput {
  type: string;
  date: Dated;
  place: PlaceInput | null;
  description: string;
  private: boolean;
}

export interface EditEvent {
  id: string;
  rev: number;
  type: string;
  role?: string;
  date: Dated;
  /** Orders events in time, as in EventView. */
  sort?: number;
  place?: PlaceInput;
  description?: string;
  private?: boolean;
  /** The number of people and families that have this event. */
  shared: number;
}

export interface EditNote {
  id: string;
  rev: number;
  type?: string;
  text: string;
  /** Only editors and administrators read the note. */
  hidden?: boolean;
}

export interface EditFamily {
  id: string;
  rel?: string;
  events: EditEvent[];
}

export interface EditPerson {
  id: string;
  rev: number;
  gender: string;
  private: boolean;
  home: boolean;
  names: NameInput[];
  events: EditEvent[];
  notes: EditNote[];
  families: EditFamily[];
}

/** The answer of the server to an edit. */
export interface EditResult {
  changeId?: number;
  subject?: string;
  created?: string;
  revision: number;
}

export type RelationKind = "parent" | "spouse" | "child" | "sibling";

/** One line of the journal of edits. */
export interface ChangeEntry {
  id: number;
  at: number;
  actor: string;
  action: string;
  subject?: string;
  objects: number;
  undoes?: number;
  undone?: boolean;
}
