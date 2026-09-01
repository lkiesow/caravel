import { translatePage } from "../i18n.js";
import { getMapTheme, setMapTheme } from "../map-theme.js";

// The map's own light/dark, beside the app's on the settings screen.
//
// Four choices where the interface has three, and the extra one is the default:
// "follow the app" is what the map has always effectively done, so it stays the
// behaviour nobody has to choose. The other three exist because the map is not
// interface -- it is the thing you look at rather than read, and wanting a
// bright map inside a dark app (or the reverse) is a preference rather than a
// mistake.
//
// "Day / night" is the one that needs its hint: it follows the sun where the
// reader is, which is not the same as the device's dark mode. Somebody
// travelling has an operating system that switched at home and a sky that did
// not.
//
// Radios, matching the theme control it sits under -- a fixed, small set. Four
// wrap to two rows at 324px, which is fine and is asserted in map.spec.js.
const MAP_THEMES = [
  { value: "app", labelKey: "settings.mapTheme.app" },
  { value: "auto", labelKey: "settings.mapTheme.auto" },
  { value: "light", labelKey: "settings.mapTheme.light" },
  { value: "dark", labelKey: "settings.mapTheme.dark" },
];

export function renderMapThemeField(container) {
  const active = getMapTheme();

  container.innerHTML = `
    <div class="setting-choices" role="radiogroup" data-i18n-aria-label="settings.mapTheme">
      ${MAP_THEMES.map(
        ({ value, labelKey }) => `
        <label class="setting-choice">
          <input type="radio" name="map-theme" value="${value}"${value === active ? " checked" : ""} />
          <span data-i18n="${labelKey}"></span>
        </label>
      `
      ).join("")}
    </div>
  `;
  translatePage(container);

  // Delegated, and on `change` rather than `click`, for the reason
  // theme-field.js gives: arrow keys move through a radio group and fire
  // change without ever clicking.
  container.addEventListener("change", (e) => {
    if (e.target.name === "map-theme") setMapTheme(e.target.value);
  });
}
