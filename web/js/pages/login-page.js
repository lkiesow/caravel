import { api, ApiError } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// Renders the combined login/register screen into `container` and resolves
// with the authenticated user once login/registration succeeds.
export function renderLoginPage(container) {
  return new Promise((resolve) => {
    let mode = "login"; // or "register"

    function render() {
      container.innerHTML = `
        <div class="auth-screen">
          <form class="auth-form" novalidate>
            <h1 data-i18n="${mode === "login" ? "auth.login.title" : "auth.register.title"}"></h1>
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

            <p class="auth-form__switch">
              <span data-i18n="${mode === "login" ? "auth.login.noAccount" : "auth.register.haveAccount"}"></span>
              <button type="button" class="link-button" data-action="switch-mode" data-i18n="${mode === "login" ? "auth.login.registerLink" : "auth.register.loginLink"}"></button>
            </p>
          </form>
        </div>
      `;
      translatePage(container);

      const form = container.querySelector("form");
      const errorEl = container.querySelector(".auth-form__error");

      container.querySelector('[data-action="switch-mode"]').addEventListener("click", () => {
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
  });
}
