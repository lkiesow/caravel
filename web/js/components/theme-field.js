import { translatePage } from "../i18n.js";
import { getTheme, setTheme } from "../theme.js";

// The Appearance control on the account settings screen: three radios, not a
// two-state toggle. "Auto" (follow the device) is the app's default and has to
// be selectable, so it is a choice like the other two rather than what you get
// by clearing them.
//
// Radios rather than a dropdown because the set is genuinely fixed at three
// and all three fit on one line; the language control beside it is the
// opposite case - an open-ended list - and uses the menu component.
const THEMES = [
  { value: "auto", labelKey: "settings.theme.auto" },
  { value: "light", labelKey: "settings.theme.light" },
  { value: "dark", labelKey: "settings.theme.dark" },
];

export function renderThemeField(container) {
  const active = getTheme();

  container.innerHTML = `
    <div class="setting-choices" role="radiogroup" data-i18n-aria-label="settings.appearance">
      ${THEMES.map(
        ({ value, labelKey }) => `
        <label class="setting-choice">
          <input type="radio" name="theme" value="${value}"${value === active ? " checked" : ""} />
          <span data-i18n="${labelKey}"></span>
        </label>
      `
      ).join("")}
    </div>
  `;
  translatePage(container);

  // Delegated, and listening for `change` rather than `click`: keyboard users
  // move through a radio group with the arrow keys, which fires change without
  // ever clicking.
  container.addEventListener("change", (e) => {
    if (e.target.name === "theme") setTheme(e.target.value);
  });
}
