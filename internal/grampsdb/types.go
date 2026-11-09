// Code generated from the type tables of the Gramps 6.0 format. DO NOT EDIT.

package grampsdb

// typeTable maps the numbers that Gramps stores to the names that Gramps XML
// writes. A value equal to custom carries its own text.
type typeTable struct {
	custom  int
	initial int
	names   map[int]string
}

var attributeTypes = typeTable{custom: 0, initial: 3, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
	1:  "Caste",
	2:  "Description",
	3:  "Identification Number",
	4:  "National Origin",
	5:  "Number of Children",
	6:  "Social Security Number",
	7:  "Nickname",
	8:  "Cause",
	9:  "Agency",
	10: "Age",
	11: "Father Age",
	12: "Mother Age",
	13: "Witness",
	14: "Time",
	15: "Occupation",
}}

var childRefTypes = typeTable{custom: 7, initial: 1, names: map[int]string{
	0: "None",
	1: "Birth",
	2: "Adopted",
	3: "Stepchild",
	4: "Sponsored",
	5: "Foster",
	6: "Unknown",
	7: "Custom",
}}

var eventRoleTypes = typeTable{custom: 0, initial: 1, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
	1:  "Primary",
	2:  "Clergy",
	3:  "Celebrant",
	4:  "Aide",
	5:  "Bride",
	6:  "Groom",
	7:  "Witness",
	8:  "Family",
	9:  "Informant",
	10: "Godparent",
	11: "Father",
	12: "Mother",
	13: "Parent",
	14: "Child",
	15: "Multiple birth",
	16: "Friend",
	17: "Neighbor",
	18: "Officiator",
}}

var eventTypes = typeTable{custom: 0, initial: 12, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
	1:  "Marriage",
	2:  "Marriage Settlement",
	3:  "Marriage License",
	4:  "Marriage Contract",
	5:  "Marriage Banns",
	6:  "Engagement",
	7:  "Divorce",
	8:  "Divorce Filing",
	9:  "Annulment",
	10: "Alternate Marriage",
	11: "Adopted",
	12: "Birth",
	13: "Death",
	14: "Adult Christening",
	15: "Baptism",
	16: "Bar Mitzvah",
	17: "Bas Mitzvah",
	18: "Blessing",
	19: "Burial",
	20: "Cause Of Death",
	21: "Census",
	22: "Christening",
	23: "Confirmation",
	24: "Cremation",
	25: "Degree",
	26: "Education",
	27: "Elected",
	28: "Emigration",
	29: "First Communion",
	30: "Immigration",
	31: "Graduation",
	32: "Medical Information",
	33: "Military Service",
	34: "Naturalization",
	35: "Nobility Title",
	36: "Number of Marriages",
	37: "Occupation",
	38: "Ordination",
	39: "Probate",
	40: "Property",
	41: "Religion",
	42: "Residence",
	43: "Retirement",
	44: "Will",
	45: "Stillbirth",
}}

var familyRelTypes = typeTable{custom: 4, initial: 0, names: map[int]string{
	0: "Married",
	1: "Unmarried",
	2: "Civil Union",
	3: "Unknown",
	4: "Custom",
}}

var nameOriginTypes = typeTable{custom: 0, initial: 1, names: map[int]string{
	-1: "Unknown ",
	0:  "Custom",
	1:  "",
	2:  "Inherited",
	3:  "Given",
	4:  "Taken",
	5:  "Patronymic",
	6:  "Matronymic",
	7:  "Feudal",
	8:  "Pseudonym",
	9:  "Patrilineal",
	10: "Matrilineal",
	11: "Occupation",
	12: "Location",
}}

var nameTypes = typeTable{custom: 0, initial: 2, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
	1:  "Also Known As",
	2:  "Birth Name",
	3:  "Married Name",
}}

var noteTypes = typeTable{custom: 0, initial: 1, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
	1:  "General",
	2:  "Research",
	3:  "Transcript",
	4:  "Person Note",
	5:  "Attribute Note",
	6:  "Address Note",
	7:  "Association Note",
	8:  "LDS Note",
	9:  "Family Note",
	10: "Event Note",
	11: "Event Reference Note",
	12: "Source Note",
	13: "Source Reference Note",
	14: "Place Note",
	15: "Repository Note",
	16: "Repository Reference Note",
	17: "Media Note",
	18: "Media Reference Note",
	19: "Child Reference Note",
	20: "Name Note",
	21: "Source text",
	22: "Citation",
	23: "Report",
	24: "Html code",
	25: "To Do",
	26: "Link",
	27: "Analysis",
}}

var placeTypes = typeTable{custom: 0, initial: -1, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
	1:  "Country",
	2:  "State",
	3:  "County",
	4:  "City",
	5:  "Parish",
	6:  "Locality",
	7:  "Street",
	8:  "Province",
	9:  "Region",
	10: "Department",
	11: "Neighborhood",
	12: "District",
	13: "Borough",
	14: "Municipality",
	15: "Town",
	16: "Village",
	17: "Hamlet",
	18: "Farm",
	19: "Building",
	20: "Number",
}}

var repositoryTypes = typeTable{custom: 0, initial: 1, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
	1:  "Library",
	2:  "Cemetery",
	3:  "Church",
	4:  "Archive",
	5:  "Album",
	6:  "Web site",
	7:  "Bookstore",
	8:  "Collection",
	9:  "Safe",
}}

var sourceMediaTypes = typeTable{custom: 0, initial: 2, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
	1:  "Audio",
	2:  "Book",
	3:  "Card",
	4:  "Electronic",
	5:  "Fiche",
	6:  "Film",
	7:  "Magazine",
	8:  "Manuscript",
	9:  "Map",
	10: "Newspaper",
	11: "Photo",
	12: "Tombstone",
	13: "Video",
}}

var srcAttributeTypes = typeTable{custom: 0, initial: -1, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
}}

var styleTypes = typeTable{custom: -1, initial: -1, names: map[int]string{
	0:  "bold",
	1:  "italic",
	2:  "underline",
	3:  "fontface",
	4:  "fontsize",
	5:  "fontcolor",
	6:  "highlight",
	7:  "superscript",
	8:  "link",
	9:  "strikethrough",
	10: "subscript",
}}

var urlTypes = typeTable{custom: 0, initial: -1, names: map[int]string{
	-1: "Unknown",
	0:  "Custom",
	1:  "E-mail",
	2:  "Web Home",
	3:  "Web Search",
	4:  "FTP",
}}
