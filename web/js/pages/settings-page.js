import { translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { renderLanguageField } from "../components/language-field.js";
import { renderThemeField } from "../components/theme-field.js";

// The account settings screen, reached from the header's user menu.
//
// Three cards, in the order todo.md put them: Language, Appearance, Password.
// Everything here is per-*account* rather than per-trip, which is why it is a
// top-level route and not another tab next to the trip's own Settings - the
// two are different things that happen to share a word.
//
// Being a top-level route is also why it needs its own way out: it is reached
// from the header menu rather than from inside a trip, so there is no tab bar
// or trip header around it to navigate back with - the same situation
// not-found-page.js is in, and it uses the same `.back-link`.
//
// The controls arrive one milestone at a time (appearance, then language, then
// password in Milestone 5) so each is reviewable on its own; each card's slot
// is where its control lands.
export async function renderSettingsPage(container) {
  container.innerHTML = `
    <div class="page settings-page">
      <a href="/trips" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.backToTrips"></span></a>
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

  renderLanguageField(container.querySelector(".language-slot"));
  renderThemeField(container.querySelector(".appearance-slot"));
}
