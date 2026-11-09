package store

// migrations holds the schema changes in order. PRAGMA user_version stores
// the number of migrations that the database has.
var migrations = []string{
	`
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) WITHOUT ROWID;

-- object keeps every genealogical object as a lossless JSON document.
CREATE TABLE object (
  kind    TEXT    NOT NULL,
  handle  TEXT    NOT NULL,
  gid     TEXT    NOT NULL,
  private INTEGER NOT NULL DEFAULT 0 CHECK (private IN (0, 1)),
  change  INTEGER NOT NULL DEFAULT 0,
  doc     TEXT    NOT NULL CHECK (json_valid(doc)),
  PRIMARY KEY (kind, handle)
) WITHOUT ROWID;
CREATE UNIQUE INDEX object_gid ON object (kind, gid);

-- person_index holds what the tree views need. Each card exists twice:
-- card_full can use private facts, card_public cannot.
CREATE TABLE person_index (
  handle      TEXT    PRIMARY KEY,
  gid         TEXT    NOT NULL UNIQUE,
  private     INTEGER NOT NULL CHECK (private IN (0, 1)),
  living      INTEGER NOT NULL CHECK (living IN (0, 1)),
  gender      TEXT    NOT NULL,
  card_full   TEXT    NOT NULL CHECK (json_valid(card_full)),
  card_public TEXT    NOT NULL CHECK (json_valid(card_public))
) WITHOUT ROWID;

CREATE TABLE family_index (
  handle         TEXT    PRIMARY KEY,
  gid            TEXT    NOT NULL UNIQUE,
  private        INTEGER NOT NULL CHECK (private IN (0, 1)),
  rel            TEXT    NOT NULL DEFAULT '',
  father         TEXT    NOT NULL DEFAULT '',
  mother         TEXT    NOT NULL DEFAULT '',
  married_full   TEXT    NOT NULL DEFAULT '',
  married_public TEXT    NOT NULL DEFAULT ''
) WITHOUT ROWID;

CREATE TABLE family_child (
  family  TEXT    NOT NULL,
  child   TEXT    NOT NULL,
  ord     INTEGER NOT NULL,
  private INTEGER NOT NULL CHECK (private IN (0, 1)),
  PRIMARY KEY (family, child)
) WITHOUT ROWID;
CREATE INDEX family_child_child ON family_child (child);

-- media_index holds what the server needs to show a file. living is set when
-- a living person leads to the file, so a guest cannot open it. gallery is
-- set for a file that somebody added in the photo gallery.
CREATE TABLE media_index (
  handle      TEXT    PRIMARY KEY,
  gid         TEXT    NOT NULL UNIQUE,
  private     INTEGER NOT NULL CHECK (private IN (0, 1)),
  living      INTEGER NOT NULL CHECK (living IN (0, 1)),
  path        TEXT    NOT NULL,
  mime        TEXT    NOT NULL DEFAULT '',
  checksum    TEXT    NOT NULL DEFAULT '',
  description TEXT    NOT NULL DEFAULT '',
  date        TEXT    NOT NULL DEFAULT '',
  gallery     INTEGER NOT NULL DEFAULT 0 CHECK (gallery IN (0, 1))
) WITHOUT ROWID;

-- A person signs in with a one-time code from e-mail. person is the handle of
-- the person in the tree who owns the account; it is empty when nobody linked
-- the account to a person. own_branch lets a family member change the own
-- branch of the tree: the person of the account, the descendants and the
-- partners.
CREATE TABLE user (
  id            INTEGER PRIMARY KEY,
  email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
  display_name  TEXT    NOT NULL,
  role          INTEGER NOT NULL,
  lang          TEXT    NOT NULL DEFAULT '',
  invited_by    INTEGER,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  last_login_at INTEGER,
  person        TEXT    NOT NULL DEFAULT '',
  own_branch    INTEGER NOT NULL DEFAULT 0 CHECK (own_branch IN (0, 1))
);

-- session stores only a hash of the secret token. id names the session in
-- the list of devices. auth_at is the time of the last sign-in code.
CREATE TABLE session (
  token_hash   BLOB    PRIMARY KEY,
  id           TEXT    NOT NULL UNIQUE,
  user_id      INTEGER NOT NULL REFERENCES user (id) ON DELETE CASCADE,
  label        TEXT    NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  auth_at      INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX session_user ON session (user_id);

-- login_code holds a sign-in code that waits for its owner. attempt_hash is
-- the hash of a secret that only the browser that asked for the code keeps.
CREATE TABLE login_code (
  attempt_hash BLOB    PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES user (id) ON DELETE CASCADE,
  code_hash    BLOB    NOT NULL,
  tries        INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX login_code_user ON login_code (user_id);

CREATE TABLE audit (
  id       INTEGER PRIMARY KEY,
  at       INTEGER NOT NULL,
  actor_id INTEGER,
  actor    TEXT    NOT NULL DEFAULT '',
  action   TEXT    NOT NULL,
  target   TEXT    NOT NULL DEFAULT '',
  detail   TEXT    NOT NULL DEFAULT '',
  ip       TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX audit_at ON audit (at);

-- change_set is the journal of the edits made in Heirfold. One set is one
-- action of one person. change_item keeps each object before and after the
-- action, so an editor can see the history and undo an action.
CREATE TABLE change_set (
  id        INTEGER PRIMARY KEY,
  at        INTEGER NOT NULL,
  actor_id  INTEGER,
  actor     TEXT    NOT NULL DEFAULT '',
  action    TEXT    NOT NULL,
  subject   TEXT    NOT NULL DEFAULT '',
  undoes    INTEGER,
  undone_by INTEGER
);
CREATE INDEX change_set_subject ON change_set (subject);

CREATE TABLE change_item (
  set_id INTEGER NOT NULL REFERENCES change_set (id) ON DELETE CASCADE,
  kind   TEXT    NOT NULL,
  handle TEXT    NOT NULL,
  gid    TEXT    NOT NULL,
  before TEXT,
  after  TEXT,
  PRIMARY KEY (set_id, kind, handle)
) WITHOUT ROWID;

-- face holds the faces that the server found on the pictures. It is no part
-- of the tree: an export does not carry it, and the journal does not record
-- it. The places are percent of the stored picture. vector is the list of
-- numbers of the face for the comparison of faces: 128 floats of 32 bits.
CREATE TABLE face (
  id        INTEGER PRIMARY KEY,
  media     TEXT    NOT NULL,
  x1        REAL    NOT NULL,
  y1        REAL    NOT NULL,
  x2        REAL    NOT NULL,
  y2        REAL    NOT NULL,
  score     REAL    NOT NULL,
  vector    BLOB,
  dismissed INTEGER NOT NULL DEFAULT 0 CHECK (dismissed IN (0, 1))
);
CREATE INDEX face_media ON face (media);

-- face_scan records which pictures the server searched for faces.
CREATE TABLE face_scan (
  media    TEXT    PRIMARY KEY,
  path     TEXT    NOT NULL,
  checksum TEXT    NOT NULL DEFAULT '',
  width    INTEGER NOT NULL DEFAULT 0,
  height   INTEGER NOT NULL DEFAULT 0,
  vectors  INTEGER NOT NULL DEFAULT 0 CHECK (vectors IN (0, 1)),
  at       INTEGER NOT NULL,
  error    TEXT    NOT NULL DEFAULT ''
) WITHOUT ROWID;

-- media_info holds what the file of a picture says about the picture: the
-- time on the clock of the camera, the place, and the town or the village of
-- that place with its country. It is no part of the tree: the server reads
-- it from the file and can read it again.
CREATE TABLE media_info (
  media    TEXT    PRIMARY KEY,
  path     TEXT    NOT NULL,
  checksum TEXT    NOT NULL DEFAULT '',
  taken    TEXT    NOT NULL DEFAULT '',
  lat      REAL,
  lon      REAL,
  town     TEXT    NOT NULL DEFAULT '',
  town_ru  TEXT    NOT NULL DEFAULT '',
  country  TEXT    NOT NULL DEFAULT '',
  at       INTEGER NOT NULL
) WITHOUT ROWID;
`,
}
