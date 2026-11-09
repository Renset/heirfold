import type { ComponentChildren } from "preact";
import { useEffect, useMemo, useState } from "preact/hooks";
import { ApiError, edit, server } from "../api/client";
import type {
  Card,
  ChangeEntry,
  EditEvent,
  EditNote,
  EditPerson,
  EditResult,
  EventInput,
  NameInput,
  PersonDetail,
  PersonInput,
  RelationKind,
} from "../api/types";
import { DateInput, dateWords } from "../components/date-input";
import { Icon } from "../components/icons";
import { cardLine } from "../components/person-picker";
import { type Apply, PersonPictures } from "../components/pictures";
import { Avatar, Button, displayName, EmptyState, Field, Spinner, useAction } from "../components/ui";
import { eventLabel, locale, relationLabel, type TextKey, t } from "../i18n";
import { type DateDraft, dateFromDraft, dateSummary, draftFromDate } from "../lib/dates";
import { orderEvents } from "../lib/events";
import { lifeYears } from "../lib/format";
import { searchPeople } from "../lib/graph";
import { familySurnames, feminine, looksMarried, nameTypeLabel } from "../lib/names";
import { link } from "../lib/router";
import { canEdit, graph, me, navigate, ownBranch, revision, treeEdited } from "../state";

const personEvents = [
  "Birth",
  "Baptism",
  "Death",
  "Burial",
  "Residence",
  "Education",
  "Occupation",
  "Military Service",
  "Graduation",
  "Emigration",
  "Immigration",
  "Naturalization",
  "Census",
  "Religion",
  "Retirement",
  "Adopted",
  "Cremation",
];
const familyEvents = ["Marriage", "Engagement", "Divorce"];
const nameTypes = ["Birth Name", "Married Name", "Also Known As"];
const familyTypes = ["Married", "Unmarried", "Civil Union", "Unknown"];
const CUSTOM = "__custom";

function familyTypeLabel(type: string): string {
  return relationLabel(type) || t("familyTypeUnknown");
}

function Page({
  title,
  back,
  backLabel,
  children,
}: {
  title: string;
  back: string;
  backLabel: string;
  children: ComponentChildren;
}) {
  return (
    <div class="settings edit-page">
      <a class="back" href={back}>
        <Icon name="caret-left" />
        {backLabel}
      </a>
      <header class="page-head">
        <h1>{title}</h1>
      </header>
      {children}
    </div>
  );
}

function Group({ title, children }: { title?: string; children: ComponentChildren }) {
  return (
    <section class="group">
      {title && <h2>{title}</h2>}
      <div class="group-body">{children}</div>
    </section>
  );
}

function Check({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <label class="check">
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.currentTarget.checked)} />
      <span>{label}</span>
    </label>
  );
}

function GenderChoice({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const options: [string, string][] = [
    ["M", t("genderMale")],
    ["F", t("genderFemale")],
    ["U", t("genderUnknown")],
  ];
  return (
    <div class="field">
      <span class="field-label">{t("gender")}</span>
      <div class="segmented" role="group" aria-label={t("gender")}>
        {options.map(([key, label]) => (
          <button key={key} type="button" aria-pressed={value === key} onClick={() => onChange(key)}>
            {label}
          </button>
        ))}
      </div>
    </div>
  );
}

function blankName(type = "Birth Name"): NameInput {
  return {
    index: -1,
    type,
    given: "",
    surname: "",
    patronymic: "",
    suffix: "",
    title: "",
    nick: "",
    call: "",
  };
}

/** The three fields of a name, in the order that the language uses. */
function NameFields({ name, onChange }: { name: NameInput; onChange: (next: NameInput) => void }) {
  const input = (key: "surname" | "given" | "patronymic", label: string) => (
    <Field label={label} key={key}>
      <input
        value={name[key]}
        maxLength={120}
        autocomplete="off"
        onInput={(event) => onChange({ ...name, [key]: event.currentTarget.value })}
      />
    </Field>
  );
  const surname = input("surname", t("nameSurname"));
  const given = input("given", t("nameGiven"));
  const patronymic = input("patronymic", t("namePatronymic"));
  return (
    <div class="name-fields">
      {locale.value === "ru" ? [surname, given, patronymic] : [given, patronymic, surname]}
    </div>
  );
}

/** The form for the names, the gender and the privacy of a person. */
function IdentityForm({
  data,
  canHide,
  apply,
}: {
  data: EditPerson;
  /** Only an editor of the whole tree changes the privacy mark. */
  canHide: boolean;
  apply: Apply;
}) {
  const [names, setNames] = useState<NameInput[]>(data.names.length ? data.names : [blankName()]);
  const [gender, setGender] = useState(data.gender || "U");
  const [hidden, setHidden] = useState(data.private);
  const [saved, setSaved] = useState(false);
  const action = useAction();

  useEffect(() => {
    setNames(data.names.length ? data.names : [blankName()]);
    setGender(data.gender || "U");
    setHidden(data.private);
  }, [data.rev]);

  const setName = (position: number, next: NameInput): void => {
    setSaved(false);
    setNames(names.map((name, i) => (i === position ? next : name)));
  };
  const main = names[0] ?? blankName();
  // A woman whose only surname is the surname of her husband: maybe the tree
  // keeps the surname from the marriage as the surname at birth.
  const family = graph.value ? familySurnames(graph.value, data.id) : { husbands: [] };
  const typed = names.some(
    (name, i) => i > 0 && (name.type === "Birth Name" || name.type === "Married Name"),
  );
  const doubtful =
    gender === "F" &&
    !typed &&
    (main.type || "Birth Name") === "Birth Name" &&
    looksMarried(main.surname, family);
  /** A new name takes the given name and the patronymic of the main name, so a person types only the surname. */
  const nextName = (type: string): NameInput => ({
    ...blankName(type),
    given: main.given,
    patronymic: main.patronymic,
  });

  return (
    <form
      class="form padded"
      onSubmit={(event) => {
        event.preventDefault();
        setSaved(false);
        const input: PersonInput = { gender, private: hidden, names };
        void action.run(async () => {
          await apply(() => edit.updatePerson(data.id, data.rev, input));
          setSaved(true);
        });
      }}
    >
      <Field label={t("nameType")} help={t("nameTypeMainHelp")}>
        <select
          value={main.type || "Birth Name"}
          onChange={(event) => setName(0, { ...main, type: event.currentTarget.value })}
        >
          {[...new Set([...nameTypes, main.type])].filter(Boolean).map((type) => (
            <option key={type} value={type}>
              {nameTypeLabel(type)}
            </option>
          ))}
        </select>
      </Field>
      <NameFields name={main} onChange={(next) => setName(0, next)} />
      {doubtful && (
        <div class="notice">
          <p>{t("nameLooksMarried")}</p>
          <Button
            onClick={() => {
              setSaved(false);
              setNames([
                { ...main, type: "Married Name" },
                ...names.slice(1),
                { ...nextName("Birth Name"), surname: family.father ? feminine(family.father) : "" },
              ]);
            }}
          >
            {t("nameMakeMarried")}
          </Button>
        </div>
      )}
      <Field label={t("nameNick")}>
        <input
          value={main.nick}
          maxLength={120}
          onInput={(event) => setName(0, { ...main, nick: event.currentTarget.value })}
        />
      </Field>
      <GenderChoice
        value={gender}
        onChange={(value) => {
          setSaved(false);
          setGender(value);
        }}
      />
      {names.slice(1).map((name, offset) => {
        const position = offset + 1;
        return (
          <fieldset class="subform" key={`${name.index}-${position}`}>
            <legend>{t("nameOther")}</legend>
            <Field label={t("nameType")}>
              <select
                value={name.type}
                onChange={(event) => setName(position, { ...name, type: event.currentTarget.value })}
              >
                {[...new Set([...nameTypes, name.type])].filter(Boolean).map((type) => (
                  <option key={type} value={type}>
                    {nameTypeLabel(type)}
                  </option>
                ))}
              </select>
            </Field>
            <NameFields name={name} onChange={(next) => setName(position, next)} />
            <Button
              kind="plain"
              onClick={() => {
                setSaved(false);
                setNames(names.filter((_, i) => i !== position));
              }}
            >
              {t("nameRemove")}
            </Button>
          </fieldset>
        );
      })}
      <Button
        kind="plain"
        onClick={() => {
          setSaved(false);
          setNames([...names, nextName(main.type === "Married Name" ? "Birth Name" : "Married Name")]);
        }}
      >
        <Icon name="plus" />
        {t("nameAdd")}
      </Button>
      {canHide && (
        <>
          <Check
            label={t("privateRecordCheck")}
            checked={hidden}
            onChange={(value) => {
              setSaved(false);
              setHidden(value);
            }}
          />
          <p class="field-help">{t("privateRecordHelp")}</p>
        </>
      )}
      {action.error && (
        <p class="field-error" role="alert">
          {action.error}
        </p>
      )}
      {saved && <p class="success">{t("saved")}</p>}
      <Button kind="filled" type="submit" busy={action.busy}>
        {t("save")}
      </Button>
    </form>
  );
}

interface PlaceOption {
  id: string;
  name: string;
}

/** A couple that can get an event such as a wedding. */
interface UnionOption {
  id: string;
  label: string;
}

/**
 * The form of one event. Without `event`, it makes a new event. A new event
 * of a person can also be an event of a couple: the form then asks for the
 * couple and gives its ID to `onSave`.
 */
function EventForm({
  event,
  kinds,
  coupleKinds = [],
  unions = [],
  places,
  onSave,
  onRemove,
  onCancel,
}: {
  event?: EditEvent;
  kinds: string[];
  coupleKinds?: string[];
  unions?: UnionOption[];
  places: PlaceOption[];
  onSave: (input: EventInput, union?: string) => Promise<void>;
  onRemove?: () => Promise<void>;
  onCancel: () => void;
}) {
  const known = event ? kinds.includes(event.type) : true;
  const [type, setType] = useState(event ? (known ? event.type : CUSTOM) : (kinds[0] ?? CUSTOM));
  const [custom, setCustom] = useState(event && !known ? event.type : "");
  const [draft, setDraft] = useState<DateDraft>(draftFromDate(event?.date));
  const [place, setPlace] = useState(event?.place?.name ?? "");
  const [description, setDescription] = useState(event?.description ?? "");
  const [hidden, setHidden] = useState(event?.private ?? false);
  const [dateError, setDateError] = useState(false);
  const [union, setUnion] = useState(unions[0]?.id ?? "");
  const action = useAction();
  const listId = `places-${event?.id ?? "new"}`;
  const ofCouple = coupleKinds.includes(type);
  const noCouple = ofCouple && unions.length === 0;

  const submit = (submitEvent: Event): void => {
    submitEvent.preventDefault();
    const date = dateFromDraft(draft);
    setDateError(!date);
    if (!date) {
      return;
    }
    const text = place.trim();
    const match = places.find((option) => option.name.toLocaleLowerCase() === text.toLocaleLowerCase());
    const input: EventInput = {
      type: type === CUSTOM ? custom.trim() : type,
      date,
      place: match ? { id: match.id } : text ? { name: text } : null,
      description: description.trim(),
      private: hidden,
    };
    void action.run(() => onSave(input, ofCouple ? union : undefined));
  };

  return (
    <form class="form padded subform-plain" onSubmit={submit}>
      <Field label={t("eventType")}>
        <select value={type} onChange={(changeEvent) => setType(changeEvent.currentTarget.value)}>
          {kinds.map((kind) => (
            <option key={kind} value={kind}>
              {eventLabel(kind)}
            </option>
          ))}
          {coupleKinds.length > 0 && (
            <optgroup label={t("eventGroupCouple")}>
              {coupleKinds.map((kind) => (
                <option key={kind} value={kind}>
                  {eventLabel(kind)}
                </option>
              ))}
            </optgroup>
          )}
          <option value={CUSTOM}>{t("eventTypeOther")}</option>
        </select>
      </Field>
      {ofCouple && unions.length > 0 && (
        <Field label={t("eventCouple")}>
          <select value={union} onChange={(changeEvent) => setUnion(changeEvent.currentTarget.value)}>
            {unions.map((option) => (
              <option key={option.id} value={option.id}>
                {option.label}
              </option>
            ))}
          </select>
        </Field>
      )}
      {noCouple && <p class="field-help">{t("eventNeedsCouple")}</p>}
      {type === CUSTOM && (
        <Field label={t("eventTypeCustom")}>
          <input
            value={custom}
            maxLength={80}
            required
            onInput={(inputEvent) => setCustom(inputEvent.currentTarget.value)}
          />
        </Field>
      )}
      <DateInput draft={draft} onChange={setDraft} />
      {dateError && (
        <p class="field-error" role="alert">
          {t("errorDate")}
        </p>
      )}
      <Field label={t("eventPlace")} help={t("eventPlaceHelp")}>
        <input
          list={listId}
          value={place}
          maxLength={300}
          autocomplete="off"
          onInput={(inputEvent) => setPlace(inputEvent.currentTarget.value)}
        />
      </Field>
      <datalist id={listId}>
        {places.map((option) => (
          <option key={option.id} value={option.name} />
        ))}
      </datalist>
      <Field label={t("eventDescription")}>
        <input
          value={description}
          maxLength={500}
          onInput={(inputEvent) => setDescription(inputEvent.currentTarget.value)}
        />
      </Field>
      <Check label={t("privateEvent")} checked={hidden} onChange={setHidden} />
      {event && event.shared > 1 && <p class="field-help">{t("eventShared", { count: event.shared })}</p>}
      {action.error && (
        <p class="field-error" role="alert">
          {action.error}
        </p>
      )}
      <div class="actions">
        <Button kind="filled" type="submit" busy={action.busy} disabled={noCouple}>
          {t("save")}
        </Button>
        <Button kind="plain" onClick={onCancel}>
          {t("cancel")}
        </Button>
        {onRemove && (
          <Button kind="danger" busy={action.busy} onClick={() => void action.run(onRemove)}>
            {t("eventRemove")}
          </Button>
        )}
      </div>
    </form>
  );
}

/** One line of the list of events: an event of the person, or of a couple. */
interface EventItem {
  event: EditEvent;
  owner: "people" | "families";
  ownerId: string;
  /** The other person of the couple, for an event of a couple. */
  partner?: string;
}

/**
 * The events of a person, with a form to add and to change them. The events
 * of the couples of the person are in the same list, because a reader looks
 * for a wedding among the events of the life.
 */
function EventList({
  personId,
  items,
  unions,
  places,
  apply,
}: {
  personId: string;
  items: EventItem[];
  unions: UnionOption[];
  places: PlaceOption[];
  apply: Apply;
}) {
  const [open, setOpen] = useState<string | null>(null);
  const words = dateWords();
  return (
    <>
      {items.map(({ event, owner, ownerId, partner }) =>
        open === event.id ? (
          <EventForm
            key={event.id}
            event={event}
            kinds={owner === "families" ? familyEvents : personEvents}
            places={places}
            onCancel={() => setOpen(null)}
            onSave={async (input) => {
              await apply(() => edit.updateEvent(event.id, event.rev, input, personId));
              setOpen(null);
            }}
            onRemove={async () => {
              await apply(() => edit.removeEvent(owner, ownerId, event.id));
              setOpen(null);
            }}
          />
        ) : (
          <div class="row" key={event.id}>
            <span class="row-text">
              <span>{eventLabel(event.type)}</span>
              <small>
                {[dateSummary(event.date, words), event.place?.name, event.description, partner]
                  .filter(Boolean)
                  .join(" · ")}
              </small>
            </span>
            <Button kind="plain" onClick={() => setOpen(event.id)}>
              {t("change")}
            </Button>
          </div>
        ),
      )}
      {open === "new" ? (
        <EventForm
          kinds={personEvents}
          coupleKinds={familyEvents}
          unions={unions}
          places={places}
          onCancel={() => setOpen(null)}
          onSave={async (input, union) => {
            await apply(() =>
              union ? edit.addEvent("families", union, input) : edit.addEvent("people", personId, input),
            );
            setOpen(null);
          }}
        />
      ) : (
        <div class="row">
          <Button kind="plain" onClick={() => setOpen("new")}>
            <Icon name="plus" />
            {t("addEvent")}
          </Button>
        </div>
      )}
    </>
  );
}

interface Adding {
  as: RelationKind;
  family?: string;
  gender: string;
  title: string;
  /** Marks the place of the form in the list. */
  slot: string;
}

/** Adds a relative: a new person, or a person who is already in the tree. */
function RelativeForm({
  personId,
  adding,
  surname,
  allowExisting,
  apply,
  onDone,
}: {
  personId: string;
  adding: Adding;
  surname: string;
  /** A family member with the own branch adds new people only. */
  allowExisting: boolean;
  apply: Apply;
  onDone: () => void;
}) {
  const [mode, setMode] = useState<"new" | "existing">("new");
  const [name, setName] = useState<NameInput>({
    ...blankName(),
    surname: adding.as === "spouse" ? "" : surname,
  });
  const [gender, setGender] = useState(adding.gender);
  const [query, setQuery] = useState("");
  const action = useAction();
  const index = graph.value;
  const hits = useMemo(
    () => (index && query.trim() ? searchPeople(index, query, 8).filter((card) => card.id !== personId) : []),
    [index, query, personId],
  );
  const relation = { person: personId, as: adding.as, family: adding.family };

  return (
    <div class="padded subform-plain">
      <p class="subform-title">{adding.title}</p>
      {allowExisting && (
        <div class="segmented" role="group" aria-label={adding.title}>
          <button type="button" aria-pressed={mode === "new"} onClick={() => setMode("new")}>
            {t("relativeNew")}
          </button>
          <button type="button" aria-pressed={mode === "existing"} onClick={() => setMode("existing")}>
            {t("relativeExisting")}
          </button>
        </div>
      )}
      {mode === "new" ? (
        <form
          class="form"
          onSubmit={(event) => {
            event.preventDefault();
            void action.run(async () => {
              await apply(() => edit.createPerson({ gender, private: false, names: [name] }, relation));
              onDone();
            });
          }}
        >
          <NameFields name={name} onChange={setName} />
          <GenderChoice value={gender} onChange={setGender} />
          {action.error && (
            <p class="field-error" role="alert">
              {action.error}
            </p>
          )}
          <div class="actions">
            <Button kind="filled" type="submit" busy={action.busy}>
              {t("add")}
            </Button>
            <Button kind="plain" onClick={onDone}>
              {t("cancel")}
            </Button>
          </div>
        </form>
      ) : (
        <div class="form">
          <Field label={t("relativeFind")} error={action.error}>
            <input
              type="text"
              value={query}
              autocomplete="off"
              onInput={(event) => setQuery(event.currentTarget.value)}
            />
          </Field>
          {hits.map((card) => (
            <button
              type="button"
              class="row link"
              key={card.id}
              disabled={action.busy}
              onClick={() =>
                void action.run(async () => {
                  await apply(() => edit.link(personId, adding.as, card.id, adding.family));
                  onDone();
                })
              }
            >
              <Avatar card={card} size="s" />
              <span class="row-text">
                <span>{cardLine(card)}</span>
              </span>
              <span class="row-value">{lifeYears(card, locale.value)}</span>
            </button>
          ))}
          {query.trim() && hits.length === 0 && <p class="row-empty">{t("noResults")}</p>}
          <div class="actions">
            <Button kind="plain" onClick={onDone}>
              {t("cancel")}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function RelativeRow({
  card,
  label,
  onRemove,
}: {
  card: Card;
  label: string;
  /** Without it the row has no button: the user cannot remove this link. */
  onRemove?: () => Promise<void>;
}) {
  const action = useAction();
  const [confirm, setConfirm] = useState(false);
  return (
    <div class="row">
      <Avatar card={card} size="s" />
      <span class="row-text">
        <small>{label}</small>
        <a href={canEdit(card.id) ? link("people", card.id, "edit") : link("people", card.id)}>
          {cardLine(card)}
        </a>
        {action.error && (
          <small class="field-error" role="alert">
            {action.error}
          </small>
        )}
      </span>
      {onRemove &&
        (confirm ? (
          <Button kind="danger" busy={action.busy} onClick={() => void action.run(onRemove)}>
            {t("linkRemoveConfirm")}
          </Button>
        ) : (
          <Button kind="plain" onClick={() => setConfirm(true)}>
            {t("linkRemove")}
          </Button>
        ))}
    </div>
  );
}

function genderWord(card: Card, male: TextKey, female: TextKey, other: TextKey): string {
  return t(card.gender === "M" ? male : card.gender === "F" ? female : other);
}

/** Shows the relatives of a person and lets an editor add and remove links. */
/**
 * Shows the relatives of a person and lets an editor add and remove links.
 * `line` is the person of a family member with the descendants. Such a user
 * adds people to the families of that line and removes no link. For an editor
 * of the whole tree, `line` is null.
 */
function Relatives({
  detail,
  data,
  line,
  apply,
}: {
  detail: PersonDetail;
  data: EditPerson;
  line: Set<string> | null;
  apply: Apply;
}) {
  const [adding, setAdding] = useState<Adding | null>(null);
  const id = detail.id;
  const surname = data.names[0]?.surname ?? "";
  const parents = detail.parents ?? [];
  const unions = detail.unions ?? [];
  const full = line === null;
  /** A family is open for a family member when a parent is in the line. */
  const open = (...parentIds: (string | undefined)[]): boolean =>
    full || parentIds.some((parent) => parent !== undefined && line.has(parent));

  const addButton = (next: Adding) =>
    adding?.slot === next.slot ? (
      <RelativeForm
        key={next.slot}
        personId={id}
        adding={next}
        surname={surname}
        allowExisting={full}
        apply={apply}
        onDone={() => setAdding(null)}
      />
    ) : (
      <div class="row" key={next.slot}>
        <Button kind="plain" onClick={() => setAdding(next)}>
          <Icon name="plus" />
          {next.title}
        </Button>
      </div>
    );
  const unlink = (family: string, person: string) =>
    full
      ? async (): Promise<void> => {
          await apply(() => edit.unlink(family, person));
        }
      : undefined;
  const ownLine = open(id);

  return (
    <>
      {(full || parents.length > 0) && (
        <Group title={t("editParents")}>
          {parents.map((family) => {
            const canAdd = open(family.father?.id, family.mother?.id);
            return (
              <div key={family.familyId}>
                {family.father && (
                  <RelativeRow
                    card={family.father}
                    label={t("relFather")}
                    onRemove={unlink(family.familyId, family.father.id)}
                  />
                )}
                {family.mother && (
                  <RelativeRow
                    card={family.mother}
                    label={t("relMother")}
                    onRemove={unlink(family.familyId, family.mother.id)}
                  />
                )}
                {canAdd &&
                  !family.father &&
                  addButton({
                    as: "parent",
                    family: family.familyId,
                    gender: "M",
                    title: t("addFather"),
                    slot: `f-${family.familyId}`,
                  })}
                {canAdd &&
                  !family.mother &&
                  addButton({
                    as: "parent",
                    family: family.familyId,
                    gender: "F",
                    title: t("addMother"),
                    slot: `m-${family.familyId}`,
                  })}
                {(family.siblings ?? []).map((sibling) => (
                  <RelativeRow
                    key={sibling.id}
                    card={sibling}
                    label={genderWord(sibling, "relBrother", "relSister", "relSibling")}
                    onRemove={unlink(family.familyId, sibling.id)}
                  />
                ))}
                {canAdd &&
                  addButton({
                    as: "sibling",
                    family: family.familyId,
                    gender: "U",
                    title: t("addSibling"),
                    slot: `s-${family.familyId}`,
                  })}
              </div>
            );
          })}
          {full && parents.length === 0 && (
            <>
              {addButton({ as: "parent", gender: "M", title: t("addFather"), slot: "f" })}
              {addButton({ as: "parent", gender: "F", title: t("addMother"), slot: "m" })}
              {addButton({ as: "sibling", gender: "U", title: t("addSibling"), slot: "s" })}
            </>
          )}
        </Group>
      )}

      {unions.map((union) => {
        const family = data.families.find((item) => item.id === union.familyId);
        const canChange = open(id, union.spouse?.id);
        return (
          <Group
            key={union.familyId}
            title={union.spouse ? t("editUnionWith", { name: cardLine(union.spouse) }) : t("editUnion")}
          >
            {union.spouse ? (
              <RelativeRow
                card={union.spouse}
                label={genderWord(union.spouse, "relHusband", "relWife", "relSpouse")}
                onRemove={unlink(union.familyId, union.spouse.id)}
              />
            ) : (
              canChange &&
              addButton({
                as: "spouse",
                family: union.familyId,
                gender: data.gender === "M" ? "F" : "M",
                title: t("addSpouse"),
                slot: `p-${union.familyId}`,
              })
            )}
            <div class="row">
              <label class="row-text" for={`rel-${union.familyId}`}>
                <span>{t("familyType")}</span>
              </label>
              <select
                id={`rel-${union.familyId}`}
                class="inline-select"
                value={family?.rel ?? "Unknown"}
                disabled={!canChange}
                onChange={(event) => {
                  const rel = event.currentTarget.value;
                  void apply(() => edit.setFamilyType(union.familyId, rel)).catch(() => undefined);
                }}
              >
                {[...new Set([...familyTypes, family?.rel ?? "Unknown"])].map((type) => (
                  <option key={type} value={type}>
                    {familyTypeLabel(type)}
                  </option>
                ))}
              </select>
            </div>
            {(union.children ?? []).map((child) => (
              <RelativeRow
                key={child.id}
                card={child}
                label={genderWord(child, "relSon", "relDaughter", "relChild")}
                onRemove={unlink(union.familyId, child.id)}
              />
            ))}
            {canChange &&
              addButton({
                as: "child",
                family: union.familyId,
                gender: "U",
                title: t("addChild"),
                slot: `c-${union.familyId}`,
              })}
          </Group>
        );
      })}

      {ownLine && (
        <Group title={unions.length ? t("editAnotherUnion") : t("editUnion")}>
          {addButton({
            as: "spouse",
            gender: data.gender === "M" ? "F" : "M",
            title: t("addSpouse"),
            slot: "p",
          })}
          {unions.length === 0 && addButton({ as: "child", gender: "U", title: t("addChild"), slot: "c" })}
        </Group>
      )}
    </>
  );
}

function NoteForm({
  note,
  onSave,
  onRemove,
}: {
  note?: EditNote;
  /** `hidden` is set only when the person can hide a note: an editor. */
  onSave: (text: string, hidden?: boolean) => Promise<void>;
  onRemove?: () => Promise<void>;
}) {
  const [text, setText] = useState(note?.text ?? "");
  const [hidden, setHidden] = useState(note?.hidden ?? false);
  const action = useAction();
  // Only the people who can change the whole tree hide a note.
  const canHide = Boolean(me.value?.edit);
  useEffect(() => {
    setText(note?.text ?? "");
    setHidden(note?.hidden ?? false);
  }, [note?.rev]);
  return (
    <form
      class="form padded subform-plain"
      onSubmit={(event) => {
        event.preventDefault();
        void action.run(async () => {
          await onSave(text, canHide ? hidden : undefined);
          if (!note) {
            setText("");
            setHidden(false);
          }
        });
      }}
    >
      <label class="field">
        <span class="field-label">{note ? t("noteLabel") : t("noteNew")}</span>
        <textarea
          rows={4}
          maxLength={50000}
          value={text}
          onInput={(event) => setText(event.currentTarget.value)}
        />
      </label>
      {canHide && (
        <div>
          <label class="check">
            <input
              type="checkbox"
              checked={hidden}
              onChange={(event) => setHidden(event.currentTarget.checked)}
            />
            <span>{t("noteHidden")}</span>
          </label>
          <p class="field-help">{t("noteHiddenHelp")}</p>
        </div>
      )}
      {action.error && (
        <p class="field-error" role="alert">
          {action.error}
        </p>
      )}
      <div class="actions">
        <Button
          type="submit"
          busy={action.busy}
          disabled={!text.trim() || (text === note?.text && hidden === (note?.hidden ?? false))}
        >
          {note ? t("save") : t("add")}
        </Button>
        {onRemove && (
          <Button kind="plain" busy={action.busy} onClick={() => void action.run(onRemove)}>
            {t("noteRemove")}
          </Button>
        )}
      </div>
    </form>
  );
}

function actionLabel(entry: ChangeEntry): string {
  const keys: Record<string, TextKey> = {
    "person.add": "historyPersonAdd",
    "person.update": "historyPersonUpdate",
    "person.delete": "historyPersonDelete",
    "link.add": "historyLinkAdd",
    "link.remove": "historyLinkRemove",
    "family.update": "historyFamilyUpdate",
    "event.add": "historyEventAdd",
    "event.update": "historyEventUpdate",
    "event.remove": "historyEventRemove",
    "note.add": "historyNoteAdd",
    "note.update": "historyNoteUpdate",
    "note.remove": "historyNoteRemove",
    "home.set": "historyHomeSet",
    "media.add": "historyMediaAdd",
    "media.update": "historyMediaUpdate",
    "media.remove": "historyMediaRemove",
    "gallery.add": "historyGalleryAdd",
    "gallery.update": "historyGalleryUpdate",
    "gallery.remove": "historyGalleryRemove",
    "gallery.mark": "historyGalleryMark",
    "gallery.unmark": "historyGalleryUnmark",
    "portrait.clear": "historyPortraitClear",
  };
  if (entry.action === "undo") {
    return t("historyUndo", { id: entry.undoes ?? 0 });
  }
  const key = keys[entry.action];
  // A tool outside the app can write an entry with an action of its own.
  return key ? t(key) : t("historyOther");
}

/** The journal of edits, for one person or for the whole tree. */
function History({
  person,
  stamp,
  onUndone,
}: {
  person?: string;
  stamp: number;
  onUndone: (result: EditResult) => Promise<void>;
}) {
  const [entries, setEntries] = useState<ChangeEntry[] | null>(null);
  const [failed, setFailed] = useState<{ id: number; text: string } | null>(null);
  const [busy, setBusy] = useState(0);
  const index = graph.value;

  useEffect(() => {
    let active = true;
    edit
      .history(person)
      .then((value) => {
        if (active) {
          setEntries(value.entries);
        }
      })
      .catch(() => {
        if (active) {
          setEntries([]);
        }
      });
    return () => {
      active = false;
    };
  }, [person, stamp]);

  const undo = (entry: ChangeEntry): void => {
    setBusy(entry.id);
    setFailed(null);
    edit
      .undo(entry.id)
      .then(onUndone)
      .catch((error: unknown) => {
        const conflict = error instanceof ApiError && error.code === "changed";
        setFailed({ id: entry.id, text: conflict ? t("historyUndoBlocked") : t("errorGeneric") });
      })
      .finally(() => setBusy(0));
  };
  const time = (seconds: number): string =>
    new Intl.DateTimeFormat(locale.value, { dateStyle: "medium", timeStyle: "short" }).format(
      new Date(seconds * 1000),
    );

  if (entries === null) {
    return <Spinner />;
  }
  if (entries.length === 0) {
    return <p class="row-empty">{t("historyEmpty")}</p>;
  }
  return (
    <>
      {entries.map((entry) => {
        const subject = !person && entry.subject ? index?.byId.get(entry.subject) : undefined;
        return (
          <div class="row" key={entry.id}>
            <span class="row-text">
              <span class={entry.undone ? "struck" : undefined}>
                {t("historyNumber", { id: entry.id })} {actionLabel(entry)}
                {subject && (
                  <>
                    {": "}
                    <a href={link("people", subject.id)}>{cardLine(subject)}</a>
                  </>
                )}
              </span>
              <small>
                {[entry.actor, time(entry.at), entry.undone ? t("historyUndone") : ""]
                  .filter(Boolean)
                  .join(" · ")}
              </small>
              {failed?.id === entry.id && (
                <small class="field-error" role="alert">
                  {failed.text}
                </small>
              )}
            </span>
            {!entry.undone && entry.objects > 0 && (me.value?.edit || me.value?.editOwn) && (
              <Button kind="plain" busy={busy === entry.id} onClick={() => undo(entry)}>
                {t("historyUndoAction")}
              </Button>
            )}
          </div>
        );
      })}
    </>
  );
}

/** The page where an editor changes one person. */
export function EditPersonView({ id }: { id: string }) {
  const [data, setData] = useState<EditPerson | null>(null);
  const [detail, setDetail] = useState<PersonDetail | null>(null);
  const [places, setPlaces] = useState<PlaceOption[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "missing">("loading");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const danger = useAction();

  const load = async (): Promise<void> => {
    const [nextData, nextDetail, nextPlaces] = await Promise.all([
      edit.person(id),
      server.person(id),
      edit.places(),
    ]);
    setData(nextData);
    setDetail(nextDetail);
    setPlaces(nextPlaces.places);
    setState("ready");
  };

  // Another editor can change the person. The page then shows the new state.
  useEffect(() => {
    let active = true;
    if (data?.id !== id) {
      setState("loading");
    }
    load().catch(() => {
      if (active) {
        setState("missing");
      }
    });
    return () => {
      active = false;
    };
  }, [id, revision.value]);

  if (!canEdit(id)) {
    return (
      <div class="center-state">
        <EmptyState
          icon="lock-simple"
          title={t(
            me.value?.source === "gramps"
              ? "errorManagedByGramps"
              : me.value?.editOwn
                ? "errorOutsideBranch"
                : "errorForbidden",
          )}
        />
      </div>
    );
  }
  if (state === "loading") {
    return (
      <div class="center-state">
        <Spinner />
      </div>
    );
  }
  if (state === "missing" || !data || !detail) {
    return (
      <div class="center-state">
        <EmptyState icon="warning-circle" title={t("errorNotFound")}>
          <a class="button tinted" href={link("people")}>
            {t("navPeople")}
          </a>
        </EmptyState>
      </div>
    );
  }

  const apply: Apply = async (operation) => {
    const result = await operation();
    await treeEdited(result.revision);
    return result;
  };
  const name = displayName(detail);
  const full = Boolean(me.value?.edit);
  // A family member changes the couples where the person or the partner is in the own line.
  const line = full ? null : (ownBranch.value?.line ?? new Set<string>());
  const unions: UnionOption[] = (detail.unions ?? [])
    .filter(
      (union) => line === null || line.has(id) || (union.spouse !== undefined && line.has(union.spouse.id)),
    )
    .map((union) => ({
      id: union.familyId,
      label: union.spouse ? cardLine(union.spouse) : t("eventCoupleAlone"),
    }));
  // The events of the person and of the couples stand in one list, in the order of the life.
  const events: EventItem[] = orderEvents<EventItem>(
    [
      ...data.events.map((event): EventItem => ({ event, owner: "people", ownerId: id })),
      ...data.families
        .filter((family) => unions.some((union) => union.id === family.id))
        .flatMap((family) =>
          family.events.map(
            (event): EventItem => ({
              event,
              owner: "families",
              ownerId: family.id,
              partner: unions.find((union) => union.id === family.id)?.label,
            }),
          ),
        ),
    ],
    (item) => item.event,
  );

  return (
    <Page
      title={t("editTitle")}
      back={link("people", id)}
      backLabel={[name.main, name.second].filter(Boolean).join(" ")}
    >
      {!full && <p class="page-note">{t("editBranchNote")}</p>}
      <Group title={t("editIdentity")}>
        <IdentityForm data={data} canHide={full} apply={apply} />
      </Group>

      <Group title={t("editPhotos")}>
        <PersonPictures detail={detail} apply={apply} />
      </Group>

      <Group title={t("lifeStory")}>
        <EventList personId={id} items={events} unions={unions} places={places} apply={apply} />
      </Group>

      <Relatives detail={detail} data={data} line={line} apply={apply} />

      <Group title={t("stories")}>
        {data.notes.map((note) => (
          <NoteForm
            key={note.id}
            note={note}
            onSave={async (text, hidden) => {
              await apply(() => edit.updateNote(note.id, note.rev, text, id, hidden));
            }}
            onRemove={async () => {
              await apply(() => edit.removeNote(id, note.id));
            }}
          />
        ))}
        <NoteForm
          onSave={async (text, hidden) => {
            await apply(() => edit.addNote(id, text, hidden));
          }}
        />
      </Group>

      {full && (
        <Group title={t("editOther")}>
          {!data.home && (
            <div class="row">
              <span class="row-text">
                <span>{t("homeSet")}</span>
                <small>{t("homeSetHelp")}</small>
              </span>
              <Button
                busy={danger.busy}
                onClick={() => void danger.run(() => apply(() => edit.setHome(id)).then(() => undefined))}
              >
                {t("homeSetAction")}
              </Button>
            </div>
          )}
          <div class="row">
            <span class="row-text">
              <span>{t("personDelete")}</span>
              <small>{t("personDeleteHelp")}</small>
              {danger.error && (
                <small class="field-error" role="alert">
                  {danger.error}
                </small>
              )}
            </span>
            {confirmDelete ? (
              <Button
                kind="danger"
                busy={danger.busy}
                onClick={() =>
                  void danger.run(async () => {
                    const result = await edit.deletePerson(id);
                    navigate(link("people"));
                    await treeEdited(result.revision);
                  })
                }
              >
                {t("personDeleteConfirm")}
              </Button>
            ) : (
              <Button kind="plain" onClick={() => setConfirmDelete(true)}>
                {t("personDeleteAction")}
              </Button>
            )}
          </div>
        </Group>
      )}

      <Group title={t("historyTitle")}>
        <History person={id} stamp={revision.value} onUndone={(result) => treeEdited(result.revision)} />
      </Group>
    </Page>
  );
}

/** The page that adds a person with no link to the tree yet. */
export function NewPersonView() {
  const [name, setName] = useState<NameInput>(blankName());
  const [gender, setGender] = useState("U");
  const action = useAction();
  if (!me.value?.edit) {
    return (
      <div class="center-state">
        <EmptyState
          icon="lock-simple"
          title={t(me.value?.source === "gramps" ? "errorManagedByGramps" : "errorForbidden")}
        />
      </div>
    );
  }
  return (
    <Page title={t("personNew")} back={link("people")} backLabel={t("navPeople")}>
      <Group>
        <form
          class="form padded"
          onSubmit={(event) => {
            event.preventDefault();
            void action.run(async () => {
              const result = await edit.createPerson({ gender, private: false, names: [name] });
              await treeEdited(result.revision);
              navigate(link("people", result.created ?? "", "edit"));
            });
          }}
        >
          <p class="lead">{t("personNewLead")}</p>
          <NameFields name={name} onChange={setName} />
          <GenderChoice value={gender} onChange={setGender} />
          {action.error && (
            <p class="field-error" role="alert">
              {action.error}
            </p>
          )}
          <Button kind="filled" type="submit" busy={action.busy}>
            {t("add")}
          </Button>
        </form>
      </Group>
    </Page>
  );
}

/** The journal of all edits of the tree. */
export function HistoryView() {
  return (
    <Page title={t("historyTitle")} back={link("settings")} backLabel={t("settingsTitle")}>
      <Group>
        <p class="row-note">{t("historyLead")}</p>
        <History stamp={revision.value} onUndone={(result) => treeEdited(result.revision)} />
      </Group>
    </Page>
  );
}
