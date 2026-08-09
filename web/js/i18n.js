import { eventBus } from "./eventbus.js";

const SUPPORTED_LOCALES = ["en", "de"];
const FALLBACK_LOCALE = "en";
const STORAGE_KEY = "caravel.locale";

let activeLocale = FALLBACK_LOCALE;
let messages = {};

function detectLocale() {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored && SUPPORTED_LOCALES.includes(stored)) return stored;

  for (const tag of navigator.languages || [navigator.language]) {
    const short = tag.slice(0, 2).toLowerCase();
    if (SUPPORTED_LOCALES.includes(short)) return short;
  }
  return FALLBACK_LOCALE;
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

export async function setLocale(locale) {
  if (!SUPPORTED_LOCALES.includes(locale) || locale === activeLocale) return;
  messages = await loadLocale(locale);
  activeLocale = locale;
  localStorage.setItem(STORAGE_KEY, locale);
  document.documentElement.lang = activeLocale;
  translatePage(document.body);
  eventBus.dispatchEvent(new CustomEvent("locale-changed", { detail: { locale } }));
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
  for (const [name, value] of Object.entries({ ...params, count })) {
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
