import { render } from "preact";
import "./styles/app.css";
import { App } from "./app";
import { locale, t } from "./i18n";
import { startRouter } from "./lib/router";
import { applyTheme, bootServer, loadDemo, theme } from "./state";

document.documentElement.lang = locale.value;
// The product has its own name in each language.
document.title = t("appName");
applyTheme(theme.value);
startRouter();

const root = document.getElementById("app");
if (root) {
  render(<App />, root);
}

if (__DEMO__) {
  void loadDemo();
} else {
  void bootServer();
}
