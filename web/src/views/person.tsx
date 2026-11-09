import type { ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { Card, EventView, MediaView, NoteView, PersonDetail } from "../api/types";
import { Icon } from "../components/icons";
import { PortraitDialog } from "../components/pictures";
import { Avatar, displayName, EmptyState, Spinner } from "../components/ui";
import { Thumbs } from "../components/viewer";
import { eventLabel, locale, relationLabel, t } from "../i18n";
import { orderEvents } from "../lib/events";
import { birthNote, formatDate, fullName, lifeYears, surnameLine } from "../lib/format";
import { nameTypeLabel } from "../lib/names";
import { link } from "../lib/router";
import { canEdit, canPortrait, me, revision, source, treeEdited } from "../state";

function Group({ title, children }: { title?: string; children: ComponentChildren }) {
  return (
    <section class="group">
      {title && <h2>{title}</h2>}
      <div class="group-body">{children}</div>
    </section>
  );
}

function PersonRow({ card, label }: { card: Card; label: string }) {
  const name = displayName(card);
  return (
    <a class="row link" href={link("people", card.id)}>
      <Avatar card={card} size="s" />
      <span class="row-text">
        <small>{label}</small>
        <span>
          {name.main} {surnameLine(card) || name.second}
        </span>
      </span>
      <span class="row-value">{lifeYears(card, locale.value)}</span>
      <Icon name="caret-right" />
    </a>
  );
}

function genderLabel(card: Card, male: string, female: string, other: string): string {
  return card.gender === "M" ? male : card.gender === "F" ? female : other;
}

function Notes({ notes }: { notes?: NoteView[] }) {
  if (!notes?.length) {
    return null;
  }
  return (
    <>
      {notes.map((note) => (
        <p key={note.id} class={note.hidden ? "note hidden-note" : "note"}>
          {note.hidden && <span class="note-mark">{t("noteHiddenMark")}</span>}
          {note.text}
        </p>
      ))}
    </>
  );
}

function EventRow({ event }: { event: EventView }) {
  const date = formatDate(event.date, locale.value);
  return (
    <div class="event">
      <div class="event-date">{date}</div>
      <div class="event-body">
        <p class="event-title">{eventLabel(event.type)}</p>
        {event.description && <p>{event.description}</p>}
        {event.place && <p class="event-place">{event.place.name}</p>}
        {event.cause && <p>{event.cause}</p>}
        <Notes notes={event.notes} />
        <Thumbs items={event.media ?? []} />
      </div>
    </div>
  );
}

export function PersonView({ id }: { id: string }) {
  const [detail, setDetail] = useState<PersonDetail | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "missing">("loading");
  const [choosing, setChoosing] = useState(false);

  // A change of the tree loads the person again, without a spinner.
  useEffect(() => {
    let active = true;
    const from = source.value;
    if (!from) {
      return;
    }
    if (detail?.id !== id) {
      setState("loading");
    }
    from
      .person(id)
      .then((value) => {
        if (active) {
          setDetail(value);
          setState("ready");
        }
      })
      .catch(() => {
        if (active) {
          setState("missing");
        }
      });
    return () => {
      active = false;
    };
  }, [id, source.value, revision.value]);

  if (state === "loading") {
    return (
      <div class="center-state">
        <Spinner />
      </div>
    );
  }
  if (state === "missing" || !detail) {
    return (
      <div class="center-state">
        <EmptyState icon="lock-simple" title={t("personHidden")}>
          <a class="button tinted" href={link("tree")}>
            {t("navTree")}
          </a>
        </EmptyState>
      </div>
    );
  }

  const name = displayName(detail);
  // The events of the person and of the couples, such as a wedding, in the order of the life.
  const life = orderEvents(
    [
      ...(detail.events ?? []),
      ...(detail.unions ?? []).flatMap((union) =>
        (union.events ?? []).map((event) => ({
          ...event,
          description: event.description || relationLabel(union.rel ?? ""),
        })),
      ),
    ],
    (event) => event,
  );
  const otherNames = (detail.names ?? []).slice(1);
  const hasFamily = Boolean(detail.parents?.length || detail.unions?.length);
  // The photos of the person come first, then the photos of the couples.
  const gallery: MediaView[] = [];
  for (const item of [
    ...(detail.media ?? []),
    ...(detail.unions ?? []).flatMap((union) => union.media ?? []),
  ]) {
    if (!gallery.some((other) => other.id === item.id)) {
      gallery.push(item);
    }
  }

  return (
    <article class="person">
      <header class="person-head">
        {canPortrait(detail.id) && !__DEMO__ ? (
          <button
            type="button"
            class="avatar-button"
            aria-label={t("portraitChoose")}
            title={t("portraitChoose")}
            onClick={() => setChoosing(true)}
          >
            <Avatar card={detail} size="l" />
            <span class="avatar-badge">
              <Icon name="image" />
            </span>
          </button>
        ) : (
          <Avatar card={detail} size="l" />
        )}
        <h1>
          {name.main} <span>{name.second}</span>
        </h1>
        {birthNote(detail, locale.value) && <p class="person-born">{birthNote(detail, locale.value)}</p>}
        <p class="person-years">
          {lifeYears(detail, locale.value)}
          {me.value?.user.person === detail.id && <span class="tag-self">{t("accessYou")}</span>}
        </p>
        <div class="person-actions">
          <a class="button filled" href={link("tree", detail.id)}>
            <Icon name="tree-structure" />
            {t("openTree")}
          </a>
          {canEdit(detail.id) && !__DEMO__ && (
            <a class="button" href={link("people", detail.id, "edit")}>
              {t("edit")}
            </a>
          )}
        </div>
      </header>

      {choosing && (
        <PortraitDialog
          detail={detail}
          apply={async (operation) => {
            const result = await operation();
            await treeEdited(result.revision);
            return result;
          }}
          onClose={() => setChoosing(false)}
        />
      )}

      {hasFamily && (
        <Group title={t("viewFamily")}>
          {detail.parents?.flatMap((parents) => [
            parents.father && (
              <PersonRow key={parents.father.id} card={parents.father} label={t("relFather")} />
            ),
            parents.mother && (
              <PersonRow key={parents.mother.id} card={parents.mother} label={t("relMother")} />
            ),
          ])}
          {detail.unions?.flatMap((union) => [
            union.spouse && (
              <PersonRow
                key={union.spouse.id}
                card={union.spouse}
                label={genderLabel(union.spouse, t("relHusband"), t("relWife"), t("relSpouse"))}
              />
            ),
            ...(union.children ?? []).map((child) => (
              <PersonRow
                key={child.id}
                card={child}
                label={genderLabel(child, t("relSon"), t("relDaughter"), t("relChild"))}
              />
            )),
          ])}
          {detail.parents?.flatMap((parents) =>
            (parents.siblings ?? []).map((sibling) => (
              <PersonRow
                key={sibling.id}
                card={sibling}
                label={genderLabel(sibling, t("relBrother"), t("relSister"), t("relSibling"))}
              />
            )),
          )}
        </Group>
      )}

      <Group title={t("lifeStory")}>
        {life.length > 0 ? (
          life.map((event) => <EventRow key={event.id} event={event} />)
        ) : (
          <p class="row-empty">{t("noEvents")}</p>
        )}
      </Group>

      {Boolean(detail.notes?.length) && (
        <Group title={t("stories")}>
          <div class="notes">
            <Notes notes={detail.notes} />
          </div>
        </Group>
      )}

      {otherNames.length > 0 && (
        <Group title={t("otherNames")}>
          {otherNames.map((other) => (
            <div class="row" key={`${other.type}-${other.given}-${other.surname}`}>
              <span class="row-text">
                <small>{nameTypeLabel(other.type ?? "")}</small>
                <span>{[other.given, other.patronymic, other.surname].filter(Boolean).join(" ")}</span>
              </span>
            </div>
          ))}
        </Group>
      )}

      {Boolean(detail.associations?.length) && (
        <Group title={t("associations")}>
          {detail.associations?.map((item) => (
            <PersonRow key={item.person.id} card={item.person} label={item.rel ?? ""} />
          ))}
        </Group>
      )}

      {Boolean(detail.attributes?.length || detail.tags?.length) && (
        <Group title={t("facts")}>
          {detail.attributes?.map((attribute) => (
            <div class="row" key={`${attribute.type}-${attribute.value}`}>
              <span class="row-text">
                <small>{attribute.type}</small>
                <span>{attribute.value}</span>
              </span>
            </div>
          ))}
          {Boolean(detail.tags?.length) && (
            <div class="row">
              <span class="row-text">
                <small>{t("tags")}</small>
                <span>{detail.tags?.map((tag) => tag.name).join(", ")}</span>
              </span>
            </div>
          )}
        </Group>
      )}

      {Boolean(detail.links?.length) && (
        <Group title={t("links")}>
          {detail.links?.map((item) => (
            <a class="row link" key={item.href} href={item.href} rel="noopener noreferrer" target="_blank">
              <span class="row-text">
                <span>{item.description || item.href}</span>
              </span>
              <Icon name="caret-right" />
            </a>
          ))}
        </Group>
      )}

      {gallery.length > 0 && (
        <Group title={t("mediaSection")}>
          <Thumbs items={gallery} />
        </Group>
      )}

      {Boolean(detail.citations?.length) && (
        <Group title={t("sources")}>
          {detail.citations?.map((citation) => (
            <div class="row citation" key={citation.id}>
              <span class="row-text">
                <span>{citation.title}</span>
                <small>
                  {[citation.author, citation.page ? t("page", { page: citation.page }) : ""]
                    .filter(Boolean)
                    .join(", ")}
                </small>
                <Thumbs items={citation.media ?? []} />
              </span>
            </div>
          ))}
        </Group>
      )}
      <p class="person-footnote">{fullName(detail, locale.value)}</p>
    </article>
  );
}
