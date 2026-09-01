// Light / dark / auto, stored per browser.
//
// "auto" is the app's historic behaviour - follow the OS - and it stays the
// default, but it is resolved *here* rather than in CSS: base.css keys its dark
// palette on `data-theme="dark"` alone, and this module is what puts that
// attribute on <html>. Two reasons that way round:
//
//   - one dark palette in the stylesheet instead of an OS copy plus an
//     explicit-choice copy that can drift apart;
//   - "auto" becomes a real, selectable state (the todo.md entry's point) with
//     the same code path as the other two, rather than the absence of a choice.
//
// The pre-paint copy of this rule lives inline in web/index.html - it has to
// run before the first paint, and this module is loaded from app.js. Keep the
// two in step; the storage key is the contract between them.
import { eventBus } from "./eventbus.js";

const STORAGE_KEY = "caravel.theme";
const THEMES = ["auto", "light", "dark"];
const DEFAULT_THEME = "auto";

const darkQuery = window.matchMedia("(prefers-color-scheme: dark)");

// The stored *preference*, which is what the settings control binds to - and
// not the same thing as the theme currently on screen ("auto" is never on
// screen). localStorage throws in a few real configurations (private windows
// with storage blocked, embedded webviews), and a theme is not worth breaking
// boot over, so every access here is guarded.
export function getTheme() {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return THEMES.includes(stored) ? stored : DEFAULT_THEME;
  } catch {
    return DEFAULT_THEME;
  }
}

// What the preference resolves to right now: "light" or "dark", never "auto".
export function resolveTheme(preference = getTheme()) {
  if (preference === "light" || preference === "dark") return preference;
  return darkQuery.matches ? "dark" : "light";
}

export function setTheme(preference) {
  if (!THEMES.includes(preference)) return;
  try {
    // "auto" is stored as the *absence* of a value rather than as the string,
    // so a browser that has never been told anything and one that was told
    // "follow the OS" are the same state. A lingering key would also make Auto
    // silently sticky if the default ever changed.
    if (preference === DEFAULT_THEME) localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, preference);
  } catch {
    // Unpersisted, but still applied for this page: a control that visibly
    // does nothing is worse than one that forgets.
  }
  applyTheme();
}

export function applyTheme() {
  const resolved = resolveTheme();
  const changed = document.documentElement.dataset.theme !== resolved;
  document.documentElement.dataset.theme = resolved;
  // CSS handles everything that keys off data-theme, but the map is drawn from
  // a style document rather than styled by the page, so it cannot follow an
  // attribute. It listens for this instead (see map-theme.js). Fired only on
  // an actual change, so the pre-paint script in index.html having already set
  // the attribute does not cause a spurious restyle at boot.
  if (changed) eventBus.dispatchEvent(new CustomEvent("theme-changed", { detail: { theme: resolved } }));
}

// Called once at boot. While the preference is "auto" the app has to follow the
// OS *live* - the OS switching at sunset with the tab already open is the
// common case, and CSS used to handle it for free.
export function initTheme() {
  applyTheme();
  darkQuery.addEventListener("change", () => {
    if (getTheme() === "auto") applyTheme();
  });
}
