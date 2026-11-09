# Design

Heirfold is a calm, modern and neutral app. Its colours and shapes follow Astryx by Meta. Heirfold does not use the Astryx library: [ADR 0002](docs/adr/0002-frontend.md) gives the reason. Change this file only with a change of the design.

## Principles

- The tree is the first screen. It centres on one person.
- A relative walks the tree by tapping a person. The tapped portrait travels to the centre.
- Plain words. No genealogy jargon on screens for relatives.
- No decoration without a job.

## Type

The app uses Onest, a variable font with Cyrillic and Latin letters. The font files ship inside the binary, in four subsets. A browser loads only the subsets that the page needs. The system font stack is the fallback. All sizes are in `rem`, so the text setting of a device scales the layout.

| Role | Size | Weight | Other |
| --- | --- | --- | --- |
| Large title | 2.125rem | 700 | Tracking -0.022em, line height 1.1 |
| Hero name | 1.625rem (1.375rem on phones) | 700 | Tracking -0.018em |
| Body | 1.0625rem | 400 | Line height 1.45 |
| Row label | 0.8125rem | 400 | Secondary colour |
| Section title | 0.875rem | 600 | Secondary colour, no capitals |

## Colour

Tokens live in `web/src/styles/app.css`. The neutrals are pure greys. There is one accent. Dark mode follows the system unless the user chooses.

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--bg` | `#f1f1f1` | `#161616` | Page |
| `--surface` | `#ffffff` | `#232323` | Grouped lists, cards, fields, buttons |
| `--chrome` | `#ffffff` | `#1c1c1c` | Top bar, tab bar, sidebar, view tabs |
| `--text` | `#141414` | `#f4f4f4` | Main text |
| `--text-2` | `#474747` | `#b5b5b5` | Secondary text |
| `--line-strong` | black at 26% | white at 28% | Outlines of fields and buttons |
| `--accent` | `#225bff` | `#8aa9ff` | Links, active tab, main action, main person |
| `--fan-a` | `#225bff` | `#6f95ff` | Father's side in the fan chart |
| `--fan-b` | `#e0663c` | `#ee8a64` | Mother's side in the fan chart |

## Components

- **Portrait.** A round monogram on a flat grey fill. Sizes are 2.5, 4 and 7rem (5.25rem for the hero on phones). The chosen photo of a person replaces the monogram, cut to the region that Gramps stores. On the page of a person, the people who can choose the portrait get a small round button on the portrait. It opens a dialog with the pictures of the person: one row for each picture, with the text buttons to make it the portrait, to take the portrait off, to crop and to remove.
- **Fan chart.** Names bend along the arc of their ring while one segment is at least 80px long. In narrower outer rings the names run along the radius. In the lower half the arc text runs the other way, so it stays upright.
- **Photo and scan tiles.** Square tiles with a hairline border. A file that is not a picture shows its type and its title.
- **Viewer.** A photo or a scan opens on the whole screen, fitted to it. The reader drags, pinches or uses the buttons to zoom. Escape closes it. The original is a download. A PDF shows as a column of white pages. A Word file shows as a light sheet in both themes, with the colors of the file.
- **Photo gallery.** A grid of square tiles, the newest photo first. Above the grid stands one row of filters: a segmented switch between the tiles and the map, a toggle for the photos without marks, a list of years, a list of places, and a button that opens the list of people. The list stands under the row, each person with the number of shown photos, and a chosen person becomes a chip in the row. The number of shown photos stands at the end of the row. A tile shows the number of marked people on a dark plate and a lock while guests cannot see the photo. The page of a photo opens for looking: the picture, the actions of a reader, the people, and a block of facts with the name of each fact on the left. The white frame of a marked face and the name under it show under the pointer, for the row of that person, or by a button. The way into the edit mode is a text button at the end of the row of actions. In the edit mode the frames always show, and a press on a face opens the dialog of a mark: the square frame of the portrait dialog and the question who it is. A face that the server found and that has no mark has a dashed frame. Below the photo each such face is a row: the face, outlined buttons with the people that the server suggests, and the text buttons for another person and for no mark.
- **List of choices.** Every `select` has the same arrow: two small triangles in the color of the text, 0.8rem from the right edge, with room between the text and the arrow. The arrow of the system is off, because it stands at the very edge.
- **Names.** A card shows the given name and the patronymic on the first line and the surname on the second. A woman shows the surname from her marriage; the surname at birth stands in brackets on a card and in a list, «Иванова (Сидорова)», and as «урожд. Сидорова» in the second text color under the name on a page. A long surname line on a chart card gets narrower letters before it is cut (ADR 0011).
- **Map.** The app draws the map itself on a canvas: land on a cooler ground for the sea, hairline borders, rivers and lakes in a soft blue. Close to the ground it adds main roads in a warm grey, the borders of regions as a dashed hairline, and the names of towns and villages. With a tile service the canvas shows the pictures of that service, and the marks of the app lie over them. The names of countries are small capitals in the third text color; the names of cities stand next to a dot. A place with photos is an accent pill with the number of photos and its name on a small plate under it. The large map moves under a finger or the mouse and has three stacked tool buttons: closer, farther, all places. The small map on the page of a photo is a still picture of the region, about 80 km across, with the towns and the villages around the point. In the edit form the map under the chosen place takes a press that sets the point; its pointer is a cross. A line under each map names the sources of the data.
- **Whole tree.** One view shows a person and everybody who is linked to that person, with all the links. It opens whole. The main person has the accent. A note above the chart counts the people who have no link to this tree.
- **Levels of detail.** A card in a chart shows less when the reader zooms out, and the text that stays is larger, so a name stays readable. From 0.7 of the full size down to 0.25 the card shows the given name and the family name in large letters. Down to 0.14 it shows the given name only. Below that it is a grey mark without text. A long name gets narrower letters and stays inside the card.
- **Chart hint.** A small note explains how to move a chart, with one text for touch and one for a mouse. It goes away at the first move and does not come back.
- **Setting row.** A setting with a few fixed values uses the segmented control. A setting whose list can grow, such as the language, uses a select at the end of the row.
- **Block note.** An explanation of a block stands under the block in small secondary text, not inside it.
- **Action order.** A screen has one filled button. Other actions are outlined buttons or text buttons. A text button that takes something away is red.
- **Main person.** A white card with a hairline border and a soft shadow. Connectors stop at the edge of the card.
- **Grouped list.** A white block with a hairline border. Hairlines divide the rows across the full width.
- **Button.** A white button with a strong outline. The main action on a screen is filled with the accent.
- **Field.** A white field with a strong outline.
- **View tabs.** Text tabs with a 2px accent line under the open tab. The row centres on a desktop. On a phone it scrolls and keeps the open tab in sight.
- **Segmented control.** A grey track. The active item is white with an outline and has no shadow. It chooses a setting. It does not switch views.
- **Sheet.** A solid panel with a border. It rises above the tab bar and closes by drag, by the close button or with Escape.
- **Bars.** The top bar and the tab bar are solid. Tabs replace the sidebar below 60rem.
- **Sign-in.** One field for the e-mail address, then one field for the code. The code field takes a paste from the letter and submits when it has 8 digits.
- **Code dialog.** A dialog in the middle of the screen asks for a new code before an action that changes who has access. It is the only modal dialog.
- **Edit page.** A person has a separate edit page, so the reading page stays calm. The page is a column of grouped lists. A row opens its form in place, with a grey fill. Text buttons carry the actions; only the add and upload actions have an icon.
- **Date fields.** A select for the kind of date, then day, month and year. The month is a select with names, so the order of day and month cannot be confused.
- **Destructive action.** The first press changes the plain button into a filled red button with the consequence in words. The second press does the action. The history can undo it.

## Motion

- The one authored moment is the portrait that travels to the centre. It uses the View Transitions API.
- Controls respond on press with a slight scale.
- Chart pan and zoom track the pointer 1:1. On release the velocity passes to a critically damped spring, and any animation can be interrupted.
- `prefers-reduced-motion` turns the travel into an instant change.

## Layout

- Touch targets are at least 2.75rem where the control is a primary action.
- Connectors between relatives are measured from the rendered portraits, so they stay correct at any text size.
- The chart views fill the space between the bars and never scroll the page. They open at a readable size around the main person (1.15 on phones, 1 on desktops). Only the fit button shows the whole chart. The whole-tree view is the exception: it opens whole.
- Phones skip the grandparents row in the family view. A relative walks up by tapping a parent.

## Limits from the security model

- No inline styles or scripts. Dynamic values go through SVG attributes or the CSSOM.
- No fonts, images or scripts from another origin.
- HTML strings cannot reach the DOM.
