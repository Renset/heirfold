# Third-party notices

Heirfold is licensed under AGPL-3.0-or-later. It includes or depends on the following work.

| Component | License | Use |
| --- | --- | --- |
| [Phosphor Icons](https://phosphoricons.com) | MIT | A few icon paths are copied into `web/src/components/icons.tsx`. The license text is in `web/src/components/phosphor-icons.LICENSE.txt`. |
| [Onest](https://github.com/simpals/onest) | SIL OFL-1.1 | Interface font in `web/src/fonts`. The license text ships with the app in `web/public/licenses/Onest-OFL.txt`. |
| [Astryx](https://github.com/meta/astryx) by Meta | MIT | Heirfold contains no Astryx code. Some colour values in `web/src/styles/app.css` follow Astryx tokens. |
| [Preact](https://preactjs.com) and `@preact/signals` | MIT | Frontend runtime |
| [PDF.js](https://mozilla.github.io/pdf.js/) (`pdfjs-dist`) by Mozilla | Apache-2.0 | Shows PDF files in the viewer. It loads as a separate file when a PDF opens. The license text ships with the app in `licenses/pdfjs-LICENSE.txt`. |
| JBIG2 and CCITT decoder of PDFium, OpenJPEG, built by the PDF.js project | BSD-3-Clause, BSD-2-Clause, Apache-2.0 | Two picture decoders that PDF.js loads for scanned pages. The files come from `pdfjs-dist/wasm` without a change. The license texts ship with the app in `licenses/pdfjs-LICENSE_*.txt`. |
| [YuNet](https://github.com/opencv/opencv_zoo/tree/main/models/face_detection_yunet) face detection model, from the OpenCV Zoo | MIT | Finds faces on photos. The file `internal/faces/models/face_detection_yunet_2023mar.onnx` is a part of the binary. The license text is next to it. |
| [SFace](https://github.com/opencv/opencv_zoo/tree/main/models/face_recognition_sface) face recognition model, from the OpenCV Zoo | Apache-2.0 | Compares faces. The file is not in the repository and not in the binary. `make models` gets it, and the operator puts it into the data directory. |
| [GeoNames](https://www.geonames.org) geographical database | CC BY 4.0 | The names of towns and villages for the place of a photo, and the cities on the map. `scripts/geo/build.py` makes `internal/geo/data/places.tsv.gz` and a part of `web/public/geo/world.json` from the files `cities500` and `alternateNamesV2`. The app names GeoNames under each map. |
| [Natural Earth](https://www.naturalearthdata.com) map data, 1:50m and 1:10m | Public domain | The outlines of countries, lakes and rivers on the map, in `web/public/geo/world.json`, and the rivers, lakes, borders of regions and main roads of the close map, in `internal/geo/data/detail.pack`. |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) | BSD-3-Clause | Database engine |

The Gramps XML reader and writer implement the public format definition. They contain no code from Gramps or Gramps Web.

The database reader in `internal/grampsdb` follows the storage format of Gramps 6.0. Its tables of type names (`types.go`) list the numbers and names that the format defines; they come from Gramps, which uses GPL-2.0-or-later. Gramps 6.0.8 produced the test data in `internal/grampsdb/grampstest` from a tree of invented people.
