import { Icon, type IconName } from "./components/icons";
import { ReauthDialog } from "./components/reauth";
import { SearchBox } from "./components/search";
import { Button, EmptyState, Spinner } from "./components/ui";
import { locale, t } from "./i18n";
import { link, route } from "./lib/router";
import { demoRole, graph, loadDemo, me, reloadTree, session, treeState } from "./state";
import { LoginView, SetupNeededView, SetupView } from "./views/auth";
import { EditPersonView, HistoryView, NewPersonView } from "./views/edit";
import { GalleryView, PhotoView } from "./views/gallery";
import { AccountsView, AuditView, ImportView, SettingsView } from "./views/manage";
import { PeopleView } from "./views/people";
import { PersonView } from "./views/person";
import { TreeView } from "./views/tree";

interface NavItem {
  href: string;
  label: string;
  icon: IconName;
  active: boolean;
}

function navItems(): NavItem[] {
  const section = route.value.path[0] ?? "tree";
  const items: NavItem[] = [
    { href: link("tree"), label: t("navTree"), icon: "tree-structure", active: section === "tree" },
    { href: link("people"), label: t("navPeople"), icon: "users-three", active: section === "people" },
  ];
  // The gallery holds photos of living relatives, so a guest has no gallery.
  if (me.value?.gallery && !__DEMO__) {
    items.push({
      href: link("gallery"),
      label: t("navGallery"),
      icon: "image",
      active: section === "gallery",
    });
  }
  items.push({
    href: link("settings"),
    label: t("navSettings"),
    icon: "gear-six",
    active: section === "settings",
  });
  return items;
}

function Sidebar() {
  return (
    <nav class="sidebar" aria-label={t("appName")}>
      <a class="brand" href={link("tree")}>
        <img src="icons/icon.svg" alt="" width="28" height="28" />
        <span>{t("appName")}</span>
      </a>
      <ul>
        {navItems().map((item) => (
          <li key={item.href}>
            <a
              href={item.href}
              class={item.active ? "nav-item active" : "nav-item"}
              aria-current={item.active ? "page" : undefined}
            >
              <Icon name={item.icon} />
              <span>{item.label}</span>
            </a>
          </li>
        ))}
      </ul>
      {me.value && <p class="signed-in">{t("signedInAs", { name: me.value.user.displayName })}</p>}
    </nav>
  );
}

function TabBar() {
  return (
    <nav class="tabbar" aria-label={t("appName")}>
      {navItems().map((item) => (
        <a
          key={item.href}
          href={item.href}
          class={item.active ? "tab active" : "tab"}
          aria-current={item.active ? "page" : undefined}
        >
          <Icon name={item.icon} />
          <span>{item.label}</span>
        </a>
      ))}
    </nav>
  );
}

function DemoBar() {
  const change = (role: "guest" | "member"): void => {
    demoRole.value = role;
    void loadDemo();
  };
  return (
    <aside class="demo-bar">
      <p>{t("demoNotice")}</p>
      <div class="demo-switch" role="group" aria-label={t("demoViewAs")}>
        <span class="demo-label">{t("demoViewAs")}</span>
        <div class="segmented compact">
          <button
            type="button"
            aria-pressed={demoRole.value === "guest"}
            title={t("demoGuestHint")}
            onClick={() => change("guest")}
          >
            {t("demoGuest")}
          </button>
          <button
            type="button"
            aria-pressed={demoRole.value === "member"}
            title={t("demoMemberHint")}
            onClick={() => change("member")}
          >
            {t("demoMember")}
          </button>
        </div>
      </div>
    </aside>
  );
}

function NotFound() {
  return (
    <div class="center-state">
      <EmptyState icon="warning-circle" title={t("notFoundPage")}>
        <a class="button" href={link("tree")}>
          {t("navTree")}
        </a>
      </EmptyState>
    </div>
  );
}

function Main() {
  const [section, id, action] = route.value.path;
  if (treeState.value === "loading" && !graph.value) {
    return (
      <div class="center-state">
        <Spinner />
      </div>
    );
  }
  if (treeState.value === "error" || !graph.value) {
    return (
      <div class="center-state">
        <EmptyState icon="warning-circle" title={t("errorNetwork")}>
          <Button onClick={() => void reloadTree()}>{t("retry")}</Button>
        </EmptyState>
      </div>
    );
  }
  switch (section) {
    case undefined:
    case "tree":
      return <TreeView personId={id} />;
    case "people":
      if (id === "new" && !__DEMO__) {
        return <NewPersonView />;
      }
      if (id && action === "edit" && !__DEMO__) {
        return <EditPersonView id={id} />;
      }
      return id ? <PersonView id={id} /> : <PeopleView />;
    case "gallery":
      if (__DEMO__ || !me.value?.gallery) {
        return <NotFound />;
      }
      return id ? <PhotoView id={id} editing={action === "edit"} /> : <GalleryView />;
    case "settings":
      if (id === "import") {
        return <ImportView />;
      }
      if (id === "accounts") {
        return <AccountsView />;
      }
      if (id === "history" && !__DEMO__) {
        return <HistoryView />;
      }
      if (id === "audit") {
        return <AuditView />;
      }
      return <SettingsView />;
    default:
      return <NotFound />;
  }
}

function Shell() {
  return (
    <div class="shell">
      <Sidebar />
      <div class="content">
        {__DEMO__ && <DemoBar />}
        <header class="topbar">
          <SearchBox />
        </header>
        <main class="main">
          <Main />
        </main>
      </div>
      <TabBar />
      {!__DEMO__ && <ReauthDialog />}
    </div>
  );
}

export function App() {
  // Reading the locale here re-renders the app when the language changes.
  void locale.value;
  if (__DEMO__) {
    return <Shell />;
  }
  const [section, token] = route.value.path;
  if (section === "setup" && token) {
    return <SetupView token={token} />;
  }
  if (session.value === "unknown") {
    return (
      <div class="center-state full">
        <Spinner />
      </div>
    );
  }
  if (session.value === "needs-setup") {
    return <SetupNeededView />;
  }
  if (session.value === "signed-out") {
    return <LoginView />;
  }
  return <Shell />;
}
