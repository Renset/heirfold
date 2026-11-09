package fixture

// All values in this file are invented for the demo.

type givenName struct {
	name        string
	patronymicM string
	patronymicF string
}

type surnameForms struct {
	male   string
	female string
}

type placeDef struct {
	name string
	kind string
	lat  float64
	long float64
}

type sourceDef struct {
	title   string
	author  string
	pubinfo string
}

type tagDef struct {
	name  string
	color string
}

type language struct {
	code          string
	researcher    string
	region        string
	male          []givenName
	female        []string
	surnames      []surnameForms
	places        []placeDef
	repositories  []string
	sources       []sourceDef
	tags          []tagDef
	occupationsM  []string
	occupationsF  []string
	storiesM      []string
	storiesF      []string
	coupleStories []string
	privateNote   string
	hobbyType     string
	hobbies       []string
	nicknames     []string
	godparent     string
	pageFormat    string
}

func languageFor(code string) *language {
	if code == "ru" {
		return &russian
	}
	return &english
}

var russian = language{
	code:       "ru",
	researcher: "Хранитель выдуманного архива",
	region:     "Заречный край",
	male: []givenName{
		{"Аристарх", "Аристархович", "Аристарховна"}, {"Глеб", "Глебович", "Глебовна"},
		{"Всеволод", "Всеволодович", "Всеволодовна"}, {"Тимофей", "Тимофеевич", "Тимофеевна"},
		{"Родион", "Родионович", "Родионовна"}, {"Платон", "Платонович", "Платоновна"},
		{"Савелий", "Савельевич", "Савельевна"}, {"Арсений", "Арсеньевич", "Арсеньевна"},
		{"Матвей", "Матвеевич", "Матвеевна"}, {"Прохор", "Прохорович", "Прохоровна"},
		{"Демьян", "Демьянович", "Демьяновна"}, {"Ефим", "Ефимович", "Ефимовна"},
		{"Захар", "Захарович", "Захаровна"}, {"Игнат", "Игнатьевич", "Игнатьевна"},
		{"Кирилл", "Кириллович", "Кирилловна"}, {"Леонтий", "Леонтьевич", "Леонтьевна"},
		{"Никанор", "Никанорович", "Никаноровна"}, {"Остап", "Остапович", "Остаповна"},
		{"Феофан", "Феофанович", "Феофановна"}, {"Елисей", "Елисеевич", "Елисеевна"},
	},
	female: []string{
		"Агафья", "Варвара", "Глафира", "Евдокия", "Зинаида", "Ксения", "Лукерья", "Марфа",
		"Мелания", "Пелагея", "Прасковья", "Серафима", "Таисия", "Ульяна", "Анфиса", "Дарья",
		"Вера", "Нина", "Полина", "Алёна", "Милена", "Ярослава",
	},
	surnames: []surnameForms{
		{"Вересов", "Вересова"}, {"Ольхин", "Ольхина"}, {"Тальников", "Тальникова"},
		{"Рябинников", "Рябинникова"}, {"Черноклёнов", "Черноклёнова"}, {"Зимогоров", "Зимогорова"},
		{"Подлеснов", "Подлеснова"}, {"Синеводов", "Синеводова"}, {"Белоярцев", "Белоярцева"},
		{"Кедрин", "Кедрина"}, {"Перелётов", "Перелётова"}, {"Листвянцев", "Листвянцева"},
		{"Ручейников", "Ручейникова"}, {"Закатов", "Закатова"},
	},
	places: []placeDef{
		{"Вербоград", "City", 57.3120, 41.1740},
		{"Тихозёрск", "Town", 57.8815, 42.4530},
		{"Малые Ключи", "Village", 56.9941, 40.5522},
		{"Рябиновый Погост", "Village", 57.1502, 42.8117},
		{"Синеводск", "City", 58.2043, 43.2964},
		{"Ольховая Слобода", "Village", 56.7425, 41.9308},
		{"Перевоз-на-Тихой", "Village", 57.5519, 40.2276},
	},
	repositories: []string{"Архив Заречного края", "Семейный архив"},
	sources: []sourceDef{
		{"Метрическая книга прихода Святой Варвары", "Причт храма, Вербоград", "Рукопись"},
		{"Семейный альбом", "Составлен родственниками", "Домашнее собрание"},
		{"Адресная книга Заречного края", "Краевая управа", "Вербоград, 1911"},
		{"Письма из Синеводска", "Разные авторы", "Домашнее собрание"},
	},
	tags: []tagDef{{"Проверено", "#2f6f5e"}, {"Нужен источник", "#a65d2e"}},
	occupationsM: []string{
		"Плотник", "Учитель", "Врач", "Мельник", "Кузнец", "Инженер", "Часовщик", "Картограф",
		"Лесничий", "Лоцман", "Агроном", "Программист",
	},
	occupationsF: []string{
		"Учительница", "Врач", "Швея", "Садовод", "Библиотекарь", "Фельдшер", "Переводчица",
		"Агроном", "Инженер", "Картограф", "Часовщица", "Программист",
	},
	storiesM: []string{
		"Построил дом у реки в месте под названием {place}. Яблони из его сада растут там до сих пор.",
		"По семейному преданию, прошёл пешком от места {place} до моря, чтобы увидеть маяк.",
		"Вёл дневник погоды сорок лет подряд. Тетради хранятся у внуков.",
		"Чинил часы всем соседям и никогда не брал денег с вдов.",
		"Научил детей читать по старой лоции, потому что других книг в доме не было.",
		"Вырезал из липы шахматы. Одного коня потеряли, и его заменяет напёрсток.",
	},
	storiesF: []string{
		"Построила дом у реки в месте под названием {place}. Яблони из её сада растут там до сих пор.",
		"По семейному преданию, прошла пешком от места {place} до моря, чтобы увидеть маяк.",
		"Вела дневник погоды сорок лет подряд. Тетради хранятся у внуков.",
		"Лечила соседей травами и записывала рецепты в поваренную книгу.",
		"Научила детей читать по старой лоции, потому что других книг в доме не было.",
		"Сшила одеяло из лоскутов всех семейных платьев. Его до сих пор передают старшей дочери.",
	},
	coupleStories: []string{
		"Познакомились на ярмарке: не поделили последнюю банку мёда.",
		"Обменивались письмами три года, прежде чем встретились.",
		"Каждую весну вместе сажали по одному дереву у дома.",
	},
	privateNote: "Личная заметка: семья просит не публиковать эту историю.",
	hobbyType:   "Увлечение",
	hobbies:     []string{"Пчеловодство", "Резьба по дереву", "Хоровое пение", "Гербарии", "Шахматы"},
	nicknames:   []string{"Ёж", "Звонарь", "Синица", "Лоцман"},
	godparent:   "Крёстный родитель",
	pageFormat:  "Лист %d",
}

var english = language{
	code:       "en",
	researcher: "Keeper of an invented archive",
	region:     "Everdale",
	male: []givenName{
		{name: "Ambrose"}, {name: "Silas"}, {name: "Jasper"}, {name: "Theodore"}, {name: "Alistair"},
		{name: "Gideon"}, {name: "Rupert"}, {name: "Edmund"}, {name: "Cornelius"}, {name: "Barnaby"},
		{name: "Tobias"}, {name: "Ezra"}, {name: "Felix"}, {name: "Hugo"}, {name: "Leopold"},
		{name: "Milo"}, {name: "Oswin"}, {name: "Percival"}, {name: "Wendell"}, {name: "Lucian"},
	},
	female: []string{
		"Adelaide", "Beatrix", "Clementine", "Dorothea", "Eleanor", "Florence", "Harriet", "Imogen",
		"Josephine", "Lavinia", "Matilda", "Nell", "Ottilie", "Philippa", "Rosalind", "Sylvia",
		"Tabitha", "Winifred", "Ada", "Cora", "Iris", "Maud", "Hazel",
	},
	surnames: []surnameForms{
		{"Thistlewood", "Thistlewood"}, {"Brackenridge", "Brackenridge"}, {"Merrivale", "Merrivale"},
		{"Hollowell", "Hollowell"}, {"Fernsby", "Fernsby"}, {"Wrenfield", "Wrenfield"},
		{"Larkspur", "Larkspur"}, {"Alderwick", "Alderwick"}, {"Quillfeather", "Quillfeather"},
		{"Ashgrove", "Ashgrove"}, {"Tansley", "Tansley"}, {"Marchbank", "Marchbank"},
		{"Penhallow", "Penhallow"}, {"Greywether", "Greywether"},
	},
	places: []placeDef{
		{"Harrowmere", "City", 54.3120, -2.1740},
		{"Port Sorrel", "Town", 54.8815, -3.4530},
		{"Alderbrook", "Village", 53.9941, -1.5522},
		{"Wexcombe", "Village", 54.1502, -2.8117},
		{"Fenwick-on-Lyle", "Town", 55.2043, -3.2964},
		{"Thornley Vale", "Village", 53.7425, -1.9308},
		{"Cobbleton", "Village", 54.5519, -1.2276},
	},
	repositories: []string{"Everdale County Archive", "Family archive"},
	sources: []sourceDef{
		{"Parish register of St Aldhelm", "Harrowmere parish clerk", "Manuscript"},
		{"The family album", "Compiled by relatives", "Private collection"},
		{"Everdale county directory", "County office", "Harrowmere, 1911"},
		{"Letters from Port Sorrel", "Various authors", "Private collection"},
	},
	tags:         []tagDef{{"Verified", "#2f6f5e"}, {"Needs a source", "#a65d2e"}},
	occupationsM: []string{"Carpenter", "Teacher", "Doctor", "Miller", "Blacksmith", "Engineer", "Watchmaker", "Cartographer", "Forester", "River pilot", "Agronomist", "Programmer"},
	occupationsF: []string{"Teacher", "Doctor", "Seamstress", "Orchard keeper", "Librarian", "Nurse", "Translator", "Agronomist", "Engineer", "Cartographer", "Watchmaker", "Programmer"},
	storiesM: []string{
		"Built a house by the river near {place}. Apples from that orchard still grow there.",
		"By family legend, walked from {place} to the sea to see a lighthouse.",
		"Kept a weather diary for forty years. The grandchildren still have the notebooks.",
		"Repaired clocks for all the neighbours and never charged a widow.",
		"Taught the children to read from an old pilot book, the only book in the house.",
		"Carved a chess set from lime wood. One knight is lost, and a thimble stands in for it.",
	},
	storiesF: []string{
		"Built a house by the river near {place}. Apples from that orchard still grow there.",
		"By family legend, walked from {place} to the sea to see a lighthouse.",
		"Kept a weather diary for forty years. The grandchildren still have the notebooks.",
		"Treated the neighbours with herbs and wrote every remedy in a cookery book.",
		"Taught the children to read from an old pilot book, the only book in the house.",
		"Sewed a quilt from every family dress. It still passes to the eldest daughter.",
	},
	coupleStories: []string{
		"They met at a fair, over the last jar of honey.",
		"They exchanged letters for three years before they met.",
		"Every spring they planted one tree by the house.",
	},
	privateNote: "Private note: the family asks not to publish this story.",
	hobbyType:   "Hobby",
	hobbies:     []string{"Beekeeping", "Woodcarving", "Choir singing", "Pressed flowers", "Chess"},
	nicknames:   []string{"Hedgehog", "Bellringer", "Wren", "Pilot"},
	godparent:   "Godparent",
	pageFormat:  "Folio %d",
}
