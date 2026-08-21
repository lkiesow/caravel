import {
  AUTO_LOCALE,
  LOCALE_NAMES,
  SUPPORTED_LOCALES,
  detectBrowserLocale,
  getLocalePreference,
  setLocale,
  t,
} from "../i18n.js";
import { renderMenu } from "./menu.js";

// The Language control on the account settings screen, and the first caller of
// setLocale() - the mechanism has existed since Stage 01 with nothing wired to
// it, so German was only reachable by changing the browser's own language.
//
// A dropdown rather than the radio row the Appearance card uses, because the
// two sets are different shapes: appearance is three fixed states, while this
// list grows every time a locale file is added. The items are *generated* from
// SUPPORTED_LOCALES for the same reason - adding a language must not mean
// editing this screen.
export function renderLanguageField(container) {
  const items = [
    {
      // Auto names what it currently resolves to. "Automatic" on its own gives
      // no feedback about what the browser actually asked for, which is the
      // one thing this row knows and the user doesn't.
      value: AUTO_LOCALE,
      label: t("settings.language.auto", { language: LOCALE_NAMES[detectBrowserLocale()] }),
    },
    ...SUPPORTED_LOCALES.map((locale) => ({ value: locale, label: LOCALE_NAMES[locale] })),
  ];

  renderMenu(container, {
    items,
    activeValue: getLocalePreference(),
    ariaLabel: "settings.language",
    className: "menu--setting",
    // Not .btn-collapse (the default): that hides the trigger's label under
    // 640px, which is fine for an icon button and useless here - this trigger
    // has no icon, so collapsing it would leave a bare chevron.
    triggerClass: "btn btn-secondary",
    onSelect: (value) => setLocale(value),
  });
}
