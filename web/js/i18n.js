import { eventBus } from "./eventbus.js";

export const SUPPORTED_LOCALES = ["en", "de"];

// Each language named in its own language, so the settings control is readable
// to someone who can't read the language the app is currently in. Deliberately
// not in the locale files: "Deutsch" is not a string to translate. Adding a
// language is a new entry here plus web/locales/xx.json plus the code in
// SUPPORTED_LOCALES - nothing in the settings screen changes.
export const LOCALE_NAMES = { en: "English", de: "Deutsch" };

const FALLBACK_LOCALE = "en";
const STORAGE_KEY = "caravel.locale";

// The preference value meaning "follow the browser" - the app's default, and a
// real selectable choice rather than the absence of one (see theme.js, which
// stores its own "auto" the same way: as no stored value at all).
export const AUTO_LOCALE = "auto";

let activeLocale = FALLBACK_LOCALE;
let messages = {};

// The stored preference: a locale code, or AUTO_LOCALE when nothing is stored.
// This is what the settings control binds to, and it is *not* getLocale() -
// "auto" is a preference, never an active locale.
//
// localStorage throws in a few real configurations (storage blocked in a
// private window, some embedded webviews). initI18n is the first thing boot
// does, so an unguarded read here took the whole app down with it.
export function getLocalePreference() {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored && SUPPORTED_LOCALES.includes(stored)) return stored;
  } catch {
    // Fall through: no stored preference we can see means "follow the browser".
  }
  return AUTO_LOCALE;
}

// What "auto" resolves to right now, from the browser's own language list.
// Exported because the settings control names it ("Automatic (English)"):
// a row that resolves to something invisible gives no feedback about what the
// browser actually asked for.
export function detectBrowserLocale() {
  for (const tag of navigator.languages || [navigator.language]) {
    const short = tag.slice(0, 2).toLowerCase();
    if (SUPPORTED_LOCALES.includes(short)) return short;
  }
  return FALLBACK_LOCALE;
}

function detectLocale() {
  const preference = getLocalePreference();
  return preference === AUTO_LOCALE ? detectBrowserLocale() : preference;
}

function storePreference(preference) {
  try {
    // "auto" is stored as the absence of a value, so "never chose" and "chose
    // to follow the browser" are one state. A leftover key would make Auto
    // silently sticky.
    if (preference === AUTO_LOCALE) localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, preference);
  } catch {
    // Unpersisted, but still applied for this page load: a control that
    // visibly does nothing is worse than one that forgets.
  }
}

async function loadLocale(locale) {
  const res = await fetch(`/locales/${locale}.json`);
  if (!res.ok) throw new Error(`failed to load locale ${locale}`);
  return res.json();
}

export async function initI18n() {
  activeLocale = detectLocale();
  messages = await loadLocale(activeLocale);
  document.documentElement.lang = activeLocale;
  translatePage(document.body);
}

// Takes a locale code or AUTO_LOCALE. Note what is *not* guarded any more:
// this used to bail out when the requested locale was already active, which
// made "back to Auto" unreachable in the common case - an English browser with
// "English" explicitly chosen resolves to the same active locale, so the
// preference would never have been cleared.
export async function setLocale(preference) {
  if (preference !== AUTO_LOCALE && !SUPPORTED_LOCALES.includes(preference)) return;
  storePreference(preference);

  const next = preference === AUTO_LOCALE ? detectBrowserLocale() : preference;
  if (next !== activeLocale) {
    messages = await loadLocale(next);
    activeLocale = next;
    document.documentElement.lang = activeLocale;
    translatePage(document.body);
  }

  // Dispatched even when the active locale didn't move: the *preference* did,
  // and the control showing it has to catch up (Auto and English are two rows
  // on an English browser, not one).
  eventBus.dispatchEvent(
    new CustomEvent("locale-changed", { detail: { locale: activeLocale, preference } })
  );
}

export function getLocale() {
  return activeLocale;
}

// t(key, params, count) — {name}-style interpolation; when `count` is given,
// picks "key_plural" for count !== 1 if that key exists, else falls back to
// "key" for both forms (sufficient for this app's copy volume; see plan 4.4).
export function t(key, params = {}, count) {
  let lookupKey = key;
  if (typeof count === "number" && count !== 1 && messages[`${key}_plural`]) {
    lookupKey = `${key}_plural`;
  }
  let text = messages[lookupKey] ?? messages[key] ?? key;
  // The third argument overrides a `count` in params -- but only when it was
  // actually given. Spreading `{ ...params, count }` unconditionally sets
  // count to undefined when the argument is absent, which clobbers a count
  // supplied in params and leaves a literal "{count}" in the rendered string.
  // Every caller happened to pass both until assist.trace.tokens, which wants
  // the placeholder without the plural.
  const values = { ...params };
  if (typeof count === "number") values.count = count;
  for (const [name, value] of Object.entries(values)) {
    if (value !== undefined) text = text.replaceAll(`{${name}}`, value);
  }
  return text;
}

// Walks data-i18n[-*] attributes in `root` and fills in translated text, so
// static markup can declare its own translation keys declaratively.
export function translatePage(root) {
  root.querySelectorAll("[data-i18n]").forEach((el) => {
    el.textContent = t(el.getAttribute("data-i18n"));
  });
  root.querySelectorAll("[data-i18n-placeholder]").forEach((el) => {
    el.setAttribute("placeholder", t(el.getAttribute("data-i18n-placeholder")));
  });
  root.querySelectorAll("[data-i18n-aria-label]").forEach((el) => {
    el.setAttribute("aria-label", t(el.getAttribute("data-i18n-aria-label")));
  });
}
