# ADR 0010: A photo gallery with marks on faces

Status: accepted, 2026-11-10.

## Context

A family keeps its photos next to its tree. Many photos show several people. A reader wants to know who is on a photo, and wants to see the photo in the card of each of those people. The photo apps of phones also find the faces on a photo and suggest who a face is.

Without a gallery, a picture reaches the tree only as the portrait of one person, through the edit page of that person.

The photos of a family are private. Everybody except guests sees all photos.

## Decision

**A photo of the gallery is a media object of the tree, and a mark on a face is a link from a person to the photo with a part of the picture.** Gramps stores a person in a photo in the same way: a media reference with a region, in whole percent. So the marks pass through a Gramps XML file and through Gramps Desktop without a new format, and the card of a person shows the photo with no more code.

The feature has these parts: the gallery, the marks on faces, the portrait rule, the face search, the data from the file, the places and the map.

### The gallery

- The gallery lists the photos of the tree, the newest first, with the people who are marked on them. `GET /api/v1/gallery` needs the role of a family member or above.
- The gallery holds photos and no scans of documents. A picture is the scan of a document when a source or a citation shows it: a page of a parish book, a letter, a certificate. Every other picture is a photo: a photo of the gallery, a portrait that somebody added in the page of a person, a picture of people from Gramps. So the gallery also shows the portraits that relatives add. A reader finds a scan at its source.
- A media object keeps the fact "came through the gallery" in the attribute `Heirfold gallery`, because Gramps has no field for it and keeps attributes that it does not know. Such a photo stays a photo also when a citation shows it, and it stays in the tree without any person.
- The page of a photo opens for looking: the picture, the people, what is known about the photo, the download. The frames of the faces show under the pointer or by a button. An editor turns on the edit mode with a button; it has the marks, the found faces and the form. The address of the edit mode is the address of the photo with `/edit`.
- A reader narrows the list by people. Every chosen person must be on the photo. The list of people for the next choice holds only the people on the photos that are still shown, so a choice never ends in an empty list.
- An editor adds photos, also many at one time. A photo starts with no person. The same file is one photo.
- A mark is a square frame on one face and the person. A second mark of the same person on the same photo moves the first one. A mark gives the person a picture and no portrait (see "The portrait is a choice").
- A removed mark leaves the photo in the gallery. An editor can remove a photo that came through Heirfold; the journal can bring it back. A picture that came from Gramps stays.
- Every action is an entry of the journal of edits.

### Rights

The people who can change the whole tree add photos and mark faces: tree editors and administrators. A family member reads the gallery. A family member whose account is linked to a person also chooses the own portrait (see "The portrait is a choice").

### Guests

A guest has no gallery. The harder question is a photo in the card of a dead person. The server cannot know who is on a photo that nobody marked completely. So:

- A new photo of the gallery is private. In Heirfold a private record is hidden from guests and visible to family members.
- An editor opens a photo for guests with a switch on the page of the photo. The text of the switch says what to check.
- An open photo still follows the rule of [ADR 0006](0006-gramps-database.md): a guest gets no file that a living person leads to. A mark of a living person closes the photo again.
- The card that a guest sees names no portrait from such a file.

### The turn of the camera

Gramps stores a part of a picture on the pixels of the file. A phone often stores a picture on its side and says so in the file, and the server turns such a picture before it shows it. The route `GET /api/v1/media/{id}/info` tells the page the orientation, and the page moves each frame between the upright picture and the stored pixels. The portrait dialog does the same for such pictures.

### The portrait is a choice

A mark on a face does not make that face the portrait. A mark says who is on a photo. It does not say which picture shows the person best. People choose the own portrait, and editors choose for dead relatives.

- In a tree from Gramps the first picture of a person is the portrait, as in Gramps. Heirfold itself never makes a picture the portrait as a side effect. A link from a person to a picture can carry the attribute `Heirfold portrait` with the value `no`. The portrait is the first picture without that mark.
- A new mark carries it. "Make portrait" takes the mark off and moves the picture to the first place, so Gramps shows the same portrait. "Remove the portrait" puts the mark on every picture of the person: the pictures stay, and nobody is the portrait. When the portrait leaves the person, the next picture does not take its place.
- A picture that somebody adds for a person in the page of that person is the portrait. That upload is the choice.
- Each person chooses the own portrait: a family member whose account is linked to a person can add an own picture, choose and crop the portrait among the own pictures, take the portrait off, and remove an own picture. These are the routes of the pictures of that one person. The store checks such an edit as it checks an edit of a branch: it compares the tree before and after, and it accepts only a change of the links from that person to pictures, a new file, and the removal of a file of that person that is no photo of the gallery.
- An editor and an administrator choose for everybody.

### The server finds faces and says who a face is like

**Models.** The two models come from the OpenCV Zoo. YuNet finds faces and five marks on each face (MIT License, 227 KB). SFace turns a face into 128 numbers; two faces of one person give numbers that are close (Apache License 2.0, 36.9 MB). Some well-known models for faces have licenses for research only, so they are not possible here.

**Engine.** `internal/nn` reads the two ONNX files and computes them in pure Go, with the standard library only. It knows the fifteen operators of the two models and refuses a file with any other operator. This keeps the single binary without a C library, and it adds no dependency. The tests compare the engine with OpenCV: the raw numbers of both models, the frames and the marks of the finder to 0.2 pixels, and the numbers of a face with a likeness above 0.995.

**Where the models are.** The finder is a part of the binary. The recognition model is too large for the repository and for the binary. `make models` gets it, with a pinned checksum. The operator puts the file into the folder `models` of the data directory. The server loads only a file with this checksum. Without the file the server finds faces and does not compare them, and the page says so.

**How it works.**

- The server searches each picture once, in the background, with half of the processors. A new photo is searched at once. `--no-faces` turns the search off. A tree that follows Gramps has no search, because it takes no marks.
- The found faces are in two tables outside the tree. An export does not carry them, the journal does not record them, and they go away with the photo.
- A found face is no mark. An editor sees a dashed frame on each found face. One press marks a person that the server suggests; another button asks who it is; a third says that the face needs no mark.
- The server suggests a person when a marked face of that person, on any photo, is like the found face. The limit is a likeness of 0.38, a little above the 0.363 that the authors of the model give, because relatives look alike. A face names at most three people.
- Only tree editors and administrators get the found faces and the suggestions.
- No picture and no number leaves the server. The page gets the frames and the cards of the suggested people, never the numbers.

### What the file says

The server reads from the file of each picture the time on the clock of the camera and the place (EXIF of a JPEG). It keeps them in the table `media_info`, outside the tree, as it keeps the found faces: the data comes from the file, and the server can read it again. The reader is own code in `internal/imaging`; it takes numbers and short texts only and trusts no size in the file.

- The page of a photo shows the time, the place, and who added the photo and when. The journal of edits says who added the photo and when. The page does not show the camera, because the camera tells a relative nothing about the photo.
- The app sends no place to any service. A link opens OpenStreetMap for the streets, only when the reader presses it.
- An editor can write the date of the photo that the family knows, in the date form of the tree. Gramps stores it as the date of the media object. This date wins over the file: the file of an old photo holds the day of the scan.
- The form of a photo opens with the date that the page shows: the written date, or else the day from the file. The server stores a date only when the editor changed it.
- The gallery has a filter by years. The year is the year of the written date, or else the year from the file.

### Places

- **The name of a place.** The file gives two numbers. The server turns them into the name of the town or the village near the point, never into an address. It asks no service: `internal/geo` holds a list of the settlements of the world with 500 people or more, from GeoNames, as a file inside the program (3.8 MB). A large city wins over a small place inside it, and a point far from every settlement gets no name. The list has an international name and, for many places, a Russian name; the page writes the country in its own language with the browser.
- **A place by hand.** Most photos of a family tree are old and have no place in the file. An editor chooses the place of a photo in the edit mode: a search by the start of a name gives the places of the tree that have a point on the globe, then the settlements of the list. A family tree names villages that no list has. The choice is an attribute of the media object, `Heirfold place`, as plain text: `48.8566,2.3522|FR|Paris|Париж`. So it lives in the tree, the journal records it, an export carries it, and a person can read it in Gramps. It wins over the place from the file. `GET /api/v1/places/search` needs the role of an editor. The form shows a map under the chosen place, and a press on the map sets the point more exactly, to about ten metres. A press near the chosen place keeps its name; a press far from it asks the server for the settlement of the new point (`GET /api/v1/places/nearest`), so an editor can also find a place by eye, without a search. A point far from every settlement is a place without a name. The server keeps the list of settlements in memory only while somebody uses it.
- The gallery has a filter by place. A press on a place of the map chooses it. Places that stand on each other at a zoom show as one mark, and a press on it brings the map closer.

### The map

- **The map.** The page of a photo shows a small map with the point of the photo, and the gallery has a view with a map of all places. The app draws the map itself, from a file of 1.2 MB that it serves: the outlines of countries, lakes and rivers from Natural Earth, and the cities with 100 000 people or more from GeoNames. No tile and no request leaves the server, so no map service learns where the photos of a family were made, and the content security policy stays closed. The cost is detail: the map shows countries, rivers and settlements, not streets. Close to the ground the map asks the server for the towns and the villages of the part of the world that it shows (`GET /api/v1/places/around`, family members and above) and names them; the question holds only a box of the globe on a grid of a quarter of a degree, and the answer is public data. The small map of a photo opens at about 80 km across. The page sends the point of a photo rounded to about ten metres to family members and above.
- **The close map.** Close to the ground the map loads squares of two degrees with rivers, lakes, the borders of regions and the main roads from Natural Earth 1:10m. They are one file inside the program (4.6 MB), and the server gives one square at a time (`GET /geo/detail/{row}_{column}.json`, the same public data for everybody). This is the detail of a road atlas.
- **A map with streets is a choice of the operator.** No public data of a size that fits into a program has streets. An operator can name a tile service (`--map-tiles`). Then the map is the pictures of that service, to the zoom of single houses, and an editor can set the point of a photo at a house. The server gets each picture itself, keeps it on its disk, and gives it to family members (`GET /api/v1/map/tiles/{z}/{x}/{y}`), so the service never sees a relative and the content security policy stays closed. The price is real and the setting is off by default: the service sees the address of the server and which parts of the world the family looks at.
- `scripts/geo/build.py` makes the list of settlements and the two map files from the public data.

## Consequences

- The numbers of a face are personal data of a special kind. They exist only in the database of the server and in its backups. The operator who does not want them starts the server with `--no-faces`.
- The quality of the suggestions on old and small photos is not measured yet.
- A photo that an editor adds through the gallery stays hidden from guests until an editor opens it. This is on purpose.
- A removed photo leaves its file on the disk, as a removed portrait does. A later clean-up tool can remove files that no record uses.
