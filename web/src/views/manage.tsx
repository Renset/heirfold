import type { ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import { server } from "../api/client";
import type {
  AccessUser,
  AuditEntry,
  Counts,
  ImportPreview,
  Role,
  SessionInfo,
  SyncState,
} from "../api/types";
import { Icon, type IconName } from "../components/icons";
import { cardLine, PersonPicker } from "../components/person-picker";
import { withFreshCode } from "../components/reauth";
import { Button, EmptyState, Field, Spinner, useAction } from "../components/ui";
import { locale, plural, setLocale, t } from "../i18n";
import { link } from "../lib/router";
import { applyTheme, demoRole, graph, loadDemo, me, reloadTree, signOut, type Theme, theme } from "../state";

export function roleLabel(role: Role): string {
  switch (role) {
    case "guest":
      return t("roleGuest");
    case "member":
      return t("roleMember");
    case "editor":
      return t("roleEditor");
    case "admin":
      return t("roleAdmin");
    default:
      return t("roleDisabled");
  }
}

function roleHelp(role: Role): string {
  switch (role) {
    case "guest":
      return t("roleGuestHelp");
    case "member":
      return t("roleMemberHelp");
    case "editor":
      return t("roleEditorHelp");
    case "admin":
      return t("roleAdminHelp");
    default:
      return t("roleDisabledHelp");
  }
}

function Page({ title, back, children }: { title: string; back?: boolean; children: ComponentChildren }) {
  return (
    <div class="settings">
      {back && (
        <a class="back" href={link("settings")}>
          <Icon name="caret-left" />
          {t("settingsTitle")}
        </a>
      )}
      <header class="page-head">
        <h1>{title}</h1>
      </header>
      {children}
    </div>
  );
}

function Group({
  title,
  note,
  children,
}: {
  title?: string;
  /** A short explanation below the block. */
  note?: string;
  children: ComponentChildren;
}) {
  return (
    <section class="group">
      {title && <h2>{title}</h2>}
      <div class="group-body">{children}</div>
      {note && <p class="group-note">{note}</p>}
    </section>
  );
}

function LinkRow({ href, icon, label }: { href: string; icon: IconName; label: string }) {
  return (
    <a class="row link" href={href}>
      <span class="row-icon">
        <Icon name={icon} />
      </span>
      <span class="row-text">
        <span>{label}</span>
      </span>
      <Icon name="caret-right" />
    </a>
  );
}

function Choice<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  return (
    <div class="row choice">
      <span class="row-text">
        <span>{label}</span>
      </span>
      <div class="segmented compact" role="group" aria-label={label}>
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            aria-pressed={option.value === value}
            onClick={() => onChange(option.value)}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  );
}

/** A setting with many possible values: a list that opens. */
function SelectRow<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  return (
    <label class="row choice">
      <span class="row-text">
        <span>{label}</span>
      </span>
      <select value={value} onChange={(event) => onChange(event.currentTarget.value as T)}>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );
}

function formatDay(seconds: number): string {
  return new Intl.DateTimeFormat(locale.value, { day: "numeric", month: "long", year: "numeric" }).format(
    new Date(seconds * 1000),
  );
}

/** Lets the person change the name that relatives see. */
function NameForm() {
  const account = me.value;
  const [name, setName] = useState(account?.user.displayName ?? "");
  const [done, setDone] = useState(false);
  const action = useAction();
  if (!account) {
    return null;
  }
  const changed = name.trim() !== "" && name.trim() !== account.user.displayName;
  return (
    <form
      class="padded"
      onSubmit={(event) => {
        event.preventDefault();
        setDone(false);
        void action.run(async () => {
          me.value = await server.updateMe({ displayName: name });
          setDone(true);
        });
      }}
    >
      <Field label={t("yourName")} help={done ? undefined : t("nameHelp")} error={action.error}>
        <span class="field-line">
          <input
            autocomplete="name"
            maxLength={80}
            value={name}
            onInput={(event) => {
              setName(event.currentTarget.value);
              setDone(false);
            }}
            required
          />
          <Button type="submit" kind={changed ? "filled" : "tinted"} busy={action.busy} disabled={!changed}>
            {t("save")}
          </Button>
        </span>
        {done && !action.error && <span class="field-help success-text">{t("saved")}</span>}
      </Field>
    </form>
  );
}

/** Lets the person say who they are in the tree. */
function OwnPerson() {
  const account = me.value;
  const action = useAction();
  if (!account) {
    return null;
  }
  const person = account.user.person ?? "";
  // With the right for the own branch the link decides what the user can
  // change, so an administrator sets it.
  const locked = Boolean(account.user.ownBranch);
  if (locked && !person) {
    return null;
  }
  return (
    <>
      <div class="padded own-person">
        <PersonPicker
          label={t("ownPerson")}
          help={locked ? t("ownPersonLocked") : t("ownPersonHelp")}
          value={person}
          busy={action.busy}
          locked={locked}
          onChange={(id) =>
            void action.run(async () => {
              me.value = await server.updateMe({ person: id });
            })
          }
        />
        {action.error && (
          <p class="field-error" role="alert">
            {action.error}
          </p>
        )}
      </div>
      {person && graph.value?.byId.has(person) && (
        <LinkRow href={link("people", person)} icon="tree-structure" label={t("ownPersonOpen")} />
      )}
    </>
  );
}

/** Lists the browsers where the person is signed in. */
function Devices() {
  const [sessions, setSessions] = useState<SessionInfo[] | null>(null);
  const action = useAction();
  const refresh = async (): Promise<void> => {
    setSessions((await server.sessions()).sessions);
  };
  useEffect(() => {
    void action.run(refresh);
  }, []);
  const others = sessions?.filter((item) => !item.current).length ?? 0;
  return (
    <Group title={t("devicesSection")} note={t("devicesHelp")}>
      {sessions === null && !action.error && <Spinner />}
      {action.error && (
        <p class="field-error padded" role="alert">
          {action.error}
        </p>
      )}
      {sessions?.map((item) => (
        <div class="row" key={item.id}>
          <span class="row-text">
            <span>{item.label || t("deviceUnknown")}</span>
            <small>
              {item.current ? t("deviceThis") : t("deviceLastSeen", { date: formatDay(item.lastSeenAt) })}
            </small>
          </span>
          {!item.current && (
            <Button
              kind="quiet-danger"
              onClick={() => void action.run(() => server.endSession(item.id).then(refresh))}
            >
              {t("deviceEnd")}
            </Button>
          )}
        </div>
      ))}
      {others > 1 && (
        <div class="row">
          <Button onClick={() => void action.run(() => server.endOtherSessions().then(refresh))}>
            {t("devicesEndOthers")}
          </Button>
        </div>
      )}
    </Group>
  );
}

function syncErrorText(code: string): string {
  switch (code) {
    case "busy":
      return t("syncErrorBusy");
    case "not_found":
      return t("syncErrorNotFound");
    case "unsupported":
      return t("syncErrorUnsupported");
    case "too_large":
      return t("syncErrorTooLarge");
    default:
      return t("syncErrorFailed");
  }
}

/** Shows the state of the sync with Gramps to a person who manages the tree. */
function SyncPanel() {
  const [state, setState] = useState<SyncState | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let active = true;
    const load = (): void => {
      server
        .sync()
        .then((value) => {
          if (active) {
            setState(value);
          }
        })
        .catch(() => undefined);
    };
    load();
    const timer = setInterval(load, 5000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, []);

  const status = state?.status;
  if (state?.source !== "gramps" || !status) {
    return null;
  }
  const taken = status.syncedAt
    ? new Intl.DateTimeFormat(locale.value, { dateStyle: "medium", timeStyle: "medium" }).format(
        new Date(status.syncedAt * 1000),
      )
    : t("syncNever");
  return (
    <Group title={t("syncSection")} note={t("syncHelp")}>
      <div class="row">
        <span class="row-text">
          <small>{t("syncTree")}</small>
          <span>{status.name || "Gramps"}</span>
        </span>
      </div>
      <div class="row">
        <span class="row-text">
          <small>{t("syncLast")}</small>
          <span>{taken}</span>
        </span>
      </div>
      {status.counts && (
        <div class="row">
          <span class="row-text">
            <small>{t("syncContent")}</small>
            <span>
              {t("syncCounts", {
                people: status.counts.people,
                families: status.counts.families,
                media: status.counts.media,
              })}
            </span>
          </span>
        </div>
      )}
      {status.error && (
        <p class="row-note field-error" role="alert">
          {syncErrorText(status.error)}
        </p>
      )}
      <div class="row">
        <Button
          busy={busy}
          onClick={() => {
            setBusy(true);
            server
              .syncNow()
              .then(setState)
              .catch(() => undefined)
              .finally(() => setBusy(false));
          }}
        >
          {t("syncNow")}
        </Button>
      </div>
    </Group>
  );
}

export function SettingsView() {
  const account = me.value;
  return (
    <Page title={t("settingsTitle")}>
      {__DEMO__ && (
        <Group
          title={t("viewAsSection")}
          note={demoRole.value === "guest" ? t("demoGuestHint") : t("demoMemberHint")}
        >
          <Choice
            label={t("demoViewAs")}
            value={demoRole.value}
            options={[
              { value: "guest", label: t("demoGuest") },
              { value: "member", label: t("demoMember") },
            ]}
            onChange={(value) => {
              demoRole.value = value;
              void loadDemo();
            }}
          />
        </Group>
      )}
      <Group title={t("appearance")}>
        <SelectRow
          label={t("language")}
          value={locale.value}
          options={[
            { value: "ru", label: "Русский" },
            { value: "en", label: "English" },
          ]}
          onChange={(value) => {
            setLocale(value);
            if (__DEMO__) {
              void loadDemo();
            } else {
              void server.updateMe({ lang: value }).catch(() => undefined);
            }
          }}
        />
        <Choice<Theme>
          label={t("theme")}
          value={theme.value}
          options={[
            { value: "system", label: t("themeSystem") },
            { value: "light", label: t("themeLight") },
            { value: "dark", label: t("themeDark") },
          ]}
          onChange={applyTheme}
        />
      </Group>
      {account?.invite && !__DEMO__ && (
        <Group title={t("treeSourceSection")}>
          <p class="row-note">
            {account.source === "gramps" ? t("treeSourceGramps") : t("treeSourceHeirfold")}
          </p>
        </Group>
      )}
      {account?.manageTree && !__DEMO__ && <SyncPanel />}
      {account?.invite && (
        <Group title={t("manageSection")}>
          <LinkRow href={link("settings", "accounts")} icon="users-three" label={t("adminPeople")} />
          {!__DEMO__ && (
            <LinkRow href={link("settings", "history")} icon="book-open-text" label={t("historyTitle")} />
          )}
          {account.importTree && (
            <LinkRow href={link("settings", "import")} icon="upload-simple" label={t("adminImport")} />
          )}
          {account.manageUsers && (
            <LinkRow href={link("settings", "audit")} icon="book-open-text" label={t("adminAudit")} />
          )}
        </Group>
      )}
      {account?.export && !account.importTree && !__DEMO__ && (
        <Group title={t("exportTitle")}>
          <div class="padded">
            <p class="lead">{t("exportLead")}</p>
            <a class="button tinted" href="/api/v1/export.gramps" download>
              <Icon name="download-simple" />
              {t("exportAction")}
            </a>
          </div>
        </Group>
      )}
      {account && (
        <Group title={t("accountSection")}>
          <div class="row">
            <span class="row-text">
              <span>{account.user.email}</span>
            </span>
            <span class="access-role">{roleLabel(account.user.role)}</span>
          </div>
          <NameForm />
          {!__DEMO__ && <OwnPerson />}
        </Group>
      )}
      {account && !__DEMO__ && <Devices />}
      {account && (
        <Group>
          <button type="button" class="row link danger" onClick={() => void signOut()}>
            <span class="row-icon">
              <Icon name="sign-out" />
            </span>
            <span class="row-text">
              <span>{t("signOut")}</span>
            </span>
          </button>
        </Group>
      )}
    </Page>
  );
}

function CountGrid({ counts }: { counts: Counts }) {
  const items: [string, number][] = [
    [t("countPeople"), counts.people],
    [t("countFamilies"), counts.families],
    [t("countEvents"), counts.events],
    [t("countPlaces"), counts.places],
    [t("countSources"), counts.sources],
    [t("countNotes"), counts.notes],
  ];
  return (
    <dl class="counts">
      {items.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function ImportView() {
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [result, setResult] = useState<string | null>(null);
  const upload = useAction();
  const commit = useAction();

  const choose = (event: Event): void => {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file) {
      return;
    }
    setPreview(null);
    setResult(null);
    void upload.run(async () => {
      setPreview(await server.uploadImport(file));
    });
  };

  const confirm = (): void => {
    if (!preview) {
      return;
    }
    void commit.run(async () => {
      const outcome = await withFreshCode(() => server.commitImport(preview.id, Boolean(preview.replaces)));
      setResult(t("importDone", { people: preview.counts.people, living: outcome.living }));
      setPreview(null);
      await reloadTree();
    });
  };

  const cancel = (): void => {
    if (preview) {
      void server.cancelImport(preview.id);
    }
    setPreview(null);
  };

  return (
    <Page title={t("adminImport")} back>
      <Group title={t("importTitle")}>
        <div class="padded">
          <p class="lead">{t("importLead")}</p>
          <label class="button filled file-button">
            <Icon name="upload-simple" />
            {upload.busy ? t("importChecking") : t("importChoose")}
            <input
              type="file"
              accept=".gramps,.xml,.gz,application/gzip,application/xml"
              onChange={choose}
              disabled={upload.busy}
            />
          </label>
          {upload.error && (
            <p class="field-error" role="alert">
              {upload.error}
            </p>
          )}
          {result && <p class="success">{result}</p>}
        </div>
      </Group>
      {preview && (
        <Group title={t("importPreview")}>
          <div class="padded">
            <CountGrid counts={preview.counts} />
            {preview.warningsTotal > 0 && (
              <p class="warning">{plural("importWarnings", preview.warningsTotal)}</p>
            )}
            {preview.replaces && (
              <p class="warning">{t("importReplaces", { people: preview.replaces.people })}</p>
            )}
            {commit.error && (
              <p class="field-error" role="alert">
                {commit.error}
              </p>
            )}
            <div class="actions">
              <Button kind={preview.replaces ? "danger" : "filled"} busy={commit.busy} onClick={confirm}>
                {preview.replaces ? t("importReplaceConfirm") : t("importConfirm")}
              </Button>
              <Button kind="plain" onClick={cancel}>
                {t("cancel")}
              </Button>
            </div>
          </div>
        </Group>
      )}
      <Group title={t("exportTitle")}>
        <div class="padded">
          <p class="lead">{t("exportLead")}</p>
          <a class="button tinted" href="/api/v1/export.gramps" download>
            <Icon name="download-simple" />
            {t("exportAction")}
          </a>
        </div>
      </Group>
    </Page>
  );
}

const allRoles: Role[] = ["guest", "member", "editor", "admin"];

/** One person in the list of people with access. An administrator can change the line. */
function AccessRow({ user, refresh }: { user: AccessUser; refresh: () => Promise<void> }) {
  const account = me.value;
  const action = useAction();
  const [confirm, setConfirm] = useState(false);
  const [sentAgain, setSentAgain] = useState(false);
  const [linking, setLinking] = useState(false);
  const admin = Boolean(account?.manageUsers);
  const person = user.person ? graph.value?.byId.get(user.person) : undefined;
  const canWithdraw = !user.self && (admin || (user.mine && user.pending));
  const state = user.pending
    ? t("accessPending", { date: formatDay(user.createdAt) })
    : t("accessLastLogin", { date: formatDay(user.lastLoginAt ?? user.createdAt) });
  return (
    <div class="row access-row">
      <span class="row-text">
        <span>
          {user.displayName}
          {user.self && <small class="tag-self">{t("accessYou")}</small>}
        </span>
        <small>{[user.email, admin || user.pending ? state : ""].filter(Boolean).join(" · ")}</small>
        {person && (
          <small>
            {t("accessPerson")} <a href={link("people", person.id)}>{cardLine(person)}</a>
          </small>
        )}
        {action.error && (
          <small class="field-error" role="alert">
            {action.error}
          </small>
        )}
        {sentAgain && <small class="success">{t("inviteSentAgain")}</small>}
      </span>
      {admin && !user.self ? (
        <select
          aria-label={`${t("inviteRole")}: ${user.displayName}`}
          value={user.role}
          onChange={(event) => {
            const next = event.currentTarget.value as Role;
            void action.run(() =>
              withFreshCode(() => server.updateUser(user.id, { role: next })).then(refresh),
            );
          }}
        >
          {allRoles.concat("disabled").map((value) => (
            <option key={value} value={value}>
              {roleLabel(value)}
            </option>
          ))}
        </select>
      ) : (
        <span class="access-role">{roleLabel(user.role)}</span>
      )}
      {(admin || (user.pending && user.mine) || canWithdraw) && (
        <div class="access-actions">
          {admin && !linking && (
            <Button kind="plain" onClick={() => setLinking(true)}>
              {user.person ? t("accessPersonChange") : t("accessPersonSet")}
            </Button>
          )}
          {user.pending && (admin || user.mine) && (
            <Button
              kind="plain"
              busy={action.busy}
              onClick={() =>
                void action.run(async () => {
                  await server.inviteAgain(user.id);
                  setSentAgain(true);
                })
              }
            >
              {t("inviteAgain")}
            </Button>
          )}
          {canWithdraw &&
            (confirm ? (
              <Button
                kind="danger"
                busy={action.busy}
                onClick={() =>
                  void action.run(() => withFreshCode(() => server.deleteUser(user.id)).then(refresh))
                }
              >
                {t("accessRemoveConfirm")}
              </Button>
            ) : (
              <Button kind="quiet-danger" onClick={() => setConfirm(true)}>
                {user.pending ? t("inviteRevoke") : t("accessRemove")}
              </Button>
            ))}
        </div>
      )}
      {admin && user.role === "member" && (
        <div class="access-link">
          <label class="check">
            <input
              type="checkbox"
              checked={Boolean(user.ownBranch)}
              disabled={action.busy || (!user.person && !user.ownBranch)}
              onChange={(event) => {
                const next = event.currentTarget.checked;
                void action.run(() =>
                  withFreshCode(() => server.updateUser(user.id, { ownBranch: next })).then(refresh),
                );
              }}
            />
            <span>{t("accessOwnBranch")}</span>
          </label>
          <small class="field-help">
            {user.person || user.ownBranch ? t("accessOwnBranchHelp") : t("accessOwnBranchNeedsPerson")}
          </small>
        </div>
      )}
      {linking && (
        <div class="access-link">
          <PersonPicker
            label={t("invitePerson")}
            value={user.person ?? ""}
            busy={action.busy}
            onChange={(id) =>
              void action.run(async () => {
                await server.updateUser(user.id, { person: id });
                await refresh();
                if (user.self) {
                  me.value = await server.me();
                }
                if (id) {
                  setLinking(false);
                }
              })
            }
          />
          <Button kind="plain" onClick={() => setLinking(false)}>
            {t("close")}
          </Button>
        </div>
      )}
    </div>
  );
}

export function AccountsView() {
  const account = me.value;
  const [users, setUsers] = useState<AccessUser[] | null>(null);
  const [role, setRole] = useState<Role>("member");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [person, setPerson] = useState("");
  const [ownBranch, setOwnBranch] = useState(false);
  const [letterLang, setLetterLang] = useState<string>(locale.value);
  const [sent, setSent] = useState<string | null>(null);
  // The list is the main content of the page. The form opens on request.
  const [inviting, setInviting] = useState(false);
  const action = useAction();
  const load = useAction();
  // An editor invites guests and family members. An administrator gives every role.
  const grantable: Role[] = account?.manageUsers ? allRoles : ["guest", "member"];

  const refresh = async (): Promise<void> => {
    setUsers((await server.users()).users);
  };

  useEffect(() => {
    void load.run(refresh);
  }, []);

  const invite = (event: Event): void => {
    event.preventDefault();
    setSent(null);
    const address = email.trim();
    void action.run(async () => {
      const branch = ownBranch && role === "member" && Boolean(person);
      await withFreshCode(() => server.invite(address, name, role, letterLang, person, branch));
      setSent(address);
      setName("");
      setEmail("");
      setPerson("");
      setOwnBranch(false);
      setInviting(false);
      await refresh();
    });
  };

  const pending = users?.filter((user) => user.pending) ?? [];
  const active = users?.filter((user) => !user.pending) ?? [];

  return (
    <Page title={t("usersTitle")} back>
      <p class="page-lead">{t("inviteLead")}</p>
      {!inviting && (
        <div class="page-actions">
          <Button
            kind="filled"
            onClick={() => {
              setSent(null);
              setInviting(true);
            }}
          >
            {t("inviteNew")}
          </Button>
          {sent && <p class="success-text">{t("inviteSent", { email: sent })}</p>}
        </div>
      )}
      {inviting && (
        <Group title={t("inviteNew")}>
          <form class="form padded" onSubmit={invite}>
            <PersonPicker
              label={t("invitePerson")}
              help={t("invitePersonHelp")}
              value={person}
              onChange={(id, card) => {
                setPerson(id);
                // The name of the person in the tree is a good first name of the account.
                if (card && !name.trim()) {
                  setName([card.given, card.surname].filter(Boolean).join(" ").slice(0, 80));
                }
              }}
            />
            <Field label={t("inviteEmail")}>
              <input
                type="email"
                autocomplete="off"
                autocapitalize="none"
                value={email}
                onInput={(event) => setEmail(event.currentTarget.value)}
                required
              />
            </Field>
            <Field label={t("inviteName")}>
              <input
                maxLength={80}
                value={name}
                required
                onInput={(event) => setName(event.currentTarget.value)}
              />
            </Field>
            <Field label={t("inviteRole")} help={roleHelp(role)} error={action.error}>
              <select value={role} onChange={(event) => setRole(event.currentTarget.value as Role)}>
                {grantable.map((value) => (
                  <option key={value} value={value}>
                    {roleLabel(value)}
                  </option>
                ))}
              </select>
            </Field>
            {account?.manageUsers && role === "member" && person && (
              <>
                <label class="check">
                  <input
                    type="checkbox"
                    checked={ownBranch}
                    onChange={(event) => setOwnBranch(event.currentTarget.checked)}
                  />
                  <span>{t("accessOwnBranch")}</span>
                </label>
                <p class="field-help">{t("accessOwnBranchHelp")}</p>
              </>
            )}
            <Field label={t("inviteLang")} help={t("inviteLangHelp")}>
              <select value={letterLang} onChange={(event) => setLetterLang(event.currentTarget.value)}>
                <option value="ru">Русский</option>
                <option value="en">English</option>
              </select>
            </Field>
            <div class="actions">
              <Button kind="filled" type="submit" busy={action.busy}>
                {t("inviteCreate")}
              </Button>
              <Button kind="plain" onClick={() => setInviting(false)}>
                {t("cancel")}
              </Button>
            </div>
          </form>
        </Group>
      )}

      {users === null && !load.error && <Spinner />}
      {load.error && (
        <p class="field-error padded" role="alert">
          {load.error}
        </p>
      )}
      {pending.length > 0 && (
        <Group title={t("invitesPending")}>
          {pending.map((user) => (
            <AccessRow key={user.id} user={user} refresh={refresh} />
          ))}
        </Group>
      )}
      {active.length > 0 && (
        <Group title={t("usersList")}>
          {active.map((user) => (
            <AccessRow key={user.id} user={user} refresh={refresh} />
          ))}
        </Group>
      )}
    </Page>
  );
}

export function AuditView() {
  const [entries, setEntries] = useState<AuditEntry[] | null>(null);
  const action = useAction();

  useEffect(() => {
    void action.run(async () => {
      setEntries((await server.audit()).entries);
    });
  }, []);

  const formatTime = (seconds: number): string =>
    new Intl.DateTimeFormat(locale.value, { dateStyle: "medium", timeStyle: "short" }).format(
      new Date(seconds * 1000),
    );

  return (
    <Page title={t("adminAudit")} back>
      {entries === null && !action.error && <Spinner />}
      {action.error && <EmptyState icon="warning-circle" title={action.error} />}
      {entries?.length === 0 && <EmptyState icon="book-open-text" title={t("auditEmpty")} />}
      {entries && entries.length > 0 && (
        <div class="group-body">
          {entries.map((entry) => (
            <div class="row" key={entry.id}>
              <span class="row-text">
                <span>
                  {entry.action} <small>{entry.target}</small>
                </span>
                <small>{[entry.actor, formatTime(entry.at), entry.ip].filter(Boolean).join(", ")}</small>
              </span>
            </div>
          ))}
        </div>
      )}
    </Page>
  );
}
