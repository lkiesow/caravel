import { api } from "../api.js";
import { guardForm } from "../busy.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// The Password card's control: current password, new password, and a
// confirmation of the new one.
//
// Three fields rather than two because there is no way back from a typo here -
// the change logs every other device out, so a mistyped new password locks you
// out of them with no way to discover what you actually typed. The confirmation
// is checked client-side only; the server has no business knowing you typed it
// twice.
//
// Success and failure both report inline, in the card. A change of password is
// exactly the operation where "did that work?" must be answerable without
// guessing, and the only other signal - a new session cookie - is invisible.
export function renderPasswordField(container) {
  container.innerHTML = `
    <form class="password-form" novalidate>
      <p class="password-form__error" role="alert" hidden></p>
      <p class="password-form__success" role="status" hidden data-i18n="settings.password.changed"></p>
      <label>
        <span data-i18n="settings.password.current"></span>
        <input type="password" name="current" autocomplete="current-password" required />
      </label>
      <label>
        <span data-i18n="settings.password.new"></span>
        <input type="password" name="next" autocomplete="new-password" required />
      </label>
      <label>
        <span data-i18n="settings.password.confirm"></span>
        <input type="password" name="confirm" autocomplete="new-password" required />
      </label>
      <div class="password-form__actions">
        <button type="submit" class="btn btn-primary">${icon("check")} <span data-i18n="settings.password.submit"></span></button>
      </div>
    </form>
  `;
  translatePage(container);

  const form = container.querySelector("form");
  const errorEl = container.querySelector(".password-form__error");
  const successEl = container.querySelector(".password-form__success");

  function fail(message) {
    errorEl.textContent = message;
    errorEl.hidden = false;
    successEl.hidden = true;
  }

  // The disable-while-in-flight this form always had, now the shared one -
  // guardForm finds the submit button itself and drops a second press.
  guardForm(form, async () => {
    errorEl.hidden = true;
    successEl.hidden = true;

    const current = form.current.value;
    const next = form.next.value;

    if (next !== form.confirm.value) {
      fail(t("settings.password.mismatch"));
      return;
    }
    // Mirrors the server's floor (httpapi.handleChangePassword) so the common
    // mistake is caught without a round trip; the server still enforces it.
    if (next.length < 8) {
      fail(t("settings.password.tooShort"));
      return;
    }

    try {
      await api.post("/auth/password", { current_password: current, new_password: next });
      form.reset();
      successEl.hidden = false;
    } catch (err) {
      // 401 here means "the current password is wrong", not "you are logged
      // out" - the request itself was authenticated by the session cookie.
      fail(err?.status === 401 ? t("settings.password.wrongCurrent") : t("common.error"));
      console.error(err);
    }
  });
}
