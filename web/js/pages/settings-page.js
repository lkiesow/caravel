import { translatePage } from "../i18n.js";
import { renderThemeField } from "../components/theme-field.js";

// The account settings screen, reached from the header's user menu.
//
// Three cards, in the order todo.md put them: Language, Appearance, Password.
// Everything here is per-*account* rather than per-trip, which is why it is a
// top-level route and not another tab next to the trip's own Settings - the
// two are different things that happen to share a word.
//
// The controls arrive one milestone at a time (appearance in Milestone 3, then
// language, then password) so each is reviewable on its own; each card's slot
// is where its control lands.
export async function renderSettingsPage(container) {
  container.innerHTML = `
    <div class="page settings-page">
      <div class="page__header">
        <h1 data-i18n="settings.title"></h1>
      </div>
      <div class="editor-card">
        <h2 data-i18n="settings.language"></h2>
        <p class="editor-card__hint" data-i18n="settings.languageHint"></p>
        <div class="language-slot"></div>
      </div>
      <div class="editor-card">
        <h2 data-i18n="settings.appearance"></h2>
        <p class="editor-card__hint" data-i18n="settings.appearanceHint"></p>
        <div class="appearance-slot"></div>
      </div>
      <div class="editor-card">
        <h2 data-i18n="settings.password"></h2>
        <p class="editor-card__hint" data-i18n="settings.passwordHint"></p>
        <div class="password-slot"></div>
      </div>
    </div>
  `;
  translatePage(container);

  renderThemeField(container.querySelector(".appearance-slot"));
}
