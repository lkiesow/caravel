import { api, ApiError } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// Renders the combined login/register screen into `container` and resolves
// with the authenticated user once login/registration succeeds.
//
// Whether registration is offered at all is the instance's decision, read from
// GET /auth/config (unauthenticated, since this page runs before anyone has a
// session). Before Stage 14 Milestone 5 the register form was always shown and
// a closed instance answered 403, which the page could only report as the
// generic "invalid username or password" — a dead end that looked like a typo.
export function renderLoginPage(container) {
  return new Promise((resolve) => {
    let mode = "login"; // or "register"
    // Assume closed until told otherwise. The switch link appears a moment
    // later on an open instance, which is better than offering registration
    // and withdrawing it.
    let openSignup = false;

    function render() {
      // Carried across the re-render rather than lost. Two things trigger one:
      // the /auth/config probe landing, and switching between login and
      // register — and in both cases the username the user has already typed
      // is still the username they meant.
      const kept = {};
      for (const name of ["username", "password", "displayName"]) {
        const el = container.querySelector(`[name="${name}"]`);
        if (el) kept[name] = el.value;
      }

      container.innerHTML = `
        <div class="auth-screen">
          <!-- The hero is the app's front door and its only marketing surface:
               an instance owner sends somebody a link and this is what loads.
               The mark repeats large behind the copy as a watermark, which is
               decorative - the lockup above the heading already names the app. -->
          <div class="auth-hero">
            <span class="auth-hero__watermark brand-mark" aria-hidden="true"></span>
            <div class="auth-hero__intro">
              <span class="app-brand app-brand--hero">
                <span class="brand-mark" aria-hidden="true"></span>
                <span class="app-brand__wordmark">${t("app.name")}</span>
              </span>
              <h1 class="auth-hero__title" data-i18n="auth.hero.title"></h1>
              <p class="auth-hero__subtitle" data-i18n="auth.hero.subtitle"></p>
            </div>
            <form class="auth-form" novalidate>
              <!-- h2, not h1: the hero's headline is the page's one h1, and the
                   form's own title says which of the two things this form does. -->
              <h2 data-i18n="${mode === "login" ? "auth.login.title" : "auth.register.title"}"></h2>
              <p class="auth-form__error" role="alert" hidden></p>

              <label>
                <span data-i18n="auth.login.username"></span>
                <input type="text" name="username" autocomplete="username" required />
              </label>

              ${
                mode === "register"
                  ? `<label>
                       <span data-i18n="auth.register.displayName"></span>
                       <input type="text" name="displayName" autocomplete="name" />
                     </label>`
                  : ""
              }

              <label>
                <span data-i18n="auth.login.password"></span>
                <input type="password" name="password" autocomplete="${mode === "login" ? "current-password" : "new-password"}" required minlength="8" />
              </label>

              <button type="submit" class="btn btn-primary">${icon(mode === "login" ? "log-in" : "check")} <span data-i18n="${mode === "login" ? "auth.login.submit" : "auth.register.submit"}"></span></button>

              ${
                // On a closed instance there is nothing to switch *to*, so the
                // whole line goes rather than leaving a dangling "Don't have an
                // account?" with no answer.
                openSignup || mode === "register"
                  ? `<p class="auth-form__switch">
                <span data-i18n="${mode === "login" ? "auth.login.noAccount" : "auth.register.haveAccount"}"></span>
                <button type="button" class="btn btn-secondary auth-form__switch-btn" data-action="switch-mode" data-i18n="${mode === "login" ? "auth.login.registerLink" : "auth.register.loginLink"}"></button>
              </p>`
                  : ""
              }
            </form>
          </div>
        </div>
      `;
      translatePage(container);

      for (const [name, value] of Object.entries(kept)) {
        const el = container.querySelector(`[name="${name}"]`);
        if (el) el.value = value;
      }

      const form = container.querySelector("form");
      const errorEl = container.querySelector(".auth-form__error");

      container.querySelector('[data-action="switch-mode"]')?.addEventListener("click", () => {
        mode = mode === "login" ? "register" : "login";
        render();
      });

      form.addEventListener("submit", async (e) => {
        e.preventDefault();
        errorEl.hidden = true;
        const data = new FormData(form);

        try {
          const user =
            mode === "login"
              ? await api.post("/auth/login", {
                  username: data.get("username"),
                  password: data.get("password"),
                })
              : await api.post("/auth/register", {
                  username: data.get("username"),
                  password: data.get("password"),
                  display_name: data.get("displayName") || undefined,
                });
          resolve(user);
        } catch (err) {
          if (err instanceof ApiError && err.status === 409) {
            errorEl.textContent = t("auth.register.usernameTaken");
          } else {
            errorEl.textContent = t("auth.login.error");
          }
          errorEl.hidden = false;
        }
      });
    }

    render();

    // After the first paint, not before it: the login form is usable either
    // way, and blocking the whole screen on a capability probe would make a
    // slow instance look broken.
    api
      .get("/auth/config")
      .then((config) => {
        if (config.open_signup === openSignup) return;
        openSignup = config.open_signup;
        render();
      })
      .catch(() => {
        // Leave it closed. Logging in still works, which is what this page is
        // for; a register link that 403s would be worse than none.
      });
  });
}
