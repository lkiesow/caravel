import { api, ApiError } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { renderMenu } from "../components/menu.js";
import { confirmDialog, promptDialog } from "../components/dialog.js";
import { renderLoading } from "../components/loading.js";
import { getCurrentUser } from "../session.js";
import { navigate } from "../router.js";
import { renderNotFoundPage } from "./not-found-page.js";

// Account administration: the accounts on this instance, and whether anyone can
// register a new one.
//
// This is the only screen in the app that is about *other people's* accounts,
// and deliberately not about their data: nothing here opens a trip. An admin who
// needs to see a trip has to be added to it like anyone else.
//
// The route is guarded twice. The user menu shows no entry for a non-admin, and
// this page checks again on load — a hidden menu item is not a permission, and
// the URL is typeable. The real boundary is requireAdmin on the server; both
// client checks exist so a non-admin gets an honest screen rather than a wall of
// 403s.
export async function renderAdminPage(container) {
  const me = getCurrentUser();
  if (!me?.is_admin) {
    renderNotFoundPage(container, { href: "/trips", labelKey: "common.home" });
    return;
  }

  let users = [];
  let openSignup = false;

  async function load() {
    renderLoading(container);
    [users, openSignup] = await Promise.all([
      api.get("/admin/users"),
      api.get("/auth/config").then((c) => c.open_signup),
    ]);
    render();
  }

  function render() {
    container.innerHTML = `
      <div class="page admin-page">
        <a href="/trips" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.backToTrips"></span></a>
        <div class="page__header">
          <h1 data-i18n="admin.title"></h1>
        </div>

        <div class="editor-card">
          <h2 data-i18n="admin.users"></h2>
          <!-- Directly under the heading, before the hint: a refusal or a
               confirmation belongs to this card, and the row that caused it can
               be anywhere down the list — including below the fold. Reporting it
               at the top of the card the action was taken in is the only
               placement that is right for every row. (It used to land in the
               *next* card's error line, which is where the Add-an-account form
               reports, and reading a last-admin refusal under "Add an account"
               is nonsense.) -->
          <p class="admin-users__error" role="alert" hidden></p>
          <p class="admin-users__status" role="status" hidden></p>
          <p class="editor-card__hint" data-i18n="admin.usersHint"></p>
          <ul class="admin-users"></ul>
        </div>

        <div class="editor-card">
          <h2 data-i18n="admin.newUser"></h2>
          <p class="admin-new-user__error" role="alert" hidden></p>
          <p class="editor-card__hint" data-i18n="admin.newUserHint"></p>
          <form class="admin-new-user">
            <label class="admin-new-user__field">
              <span data-i18n="admin.username"></span>
              <input type="text" name="username" autocomplete="off" autocapitalize="none" spellcheck="false" required />
            </label>
            <label class="admin-new-user__field">
              <span data-i18n="admin.displayName"></span>
              <input type="text" name="displayName" autocomplete="off" />
            </label>
            <label class="admin-new-user__field">
              <span data-i18n="admin.password"></span>
              <input type="password" name="password" autocomplete="new-password" required minlength="8" />
            </label>
            <label class="admin-new-user__checkbox">
              <input type="checkbox" name="isAdmin" />
              <span data-i18n="admin.makeAdmin"></span>
            </label>
            <button type="submit" class="btn btn-primary">${icon("user-plus")} <span data-i18n="admin.create"></span></button>
          </form>
        </div>

        <div class="editor-card">
          <h2 data-i18n="admin.registration"></h2>
          <p class="editor-card__hint" data-i18n="admin.registrationHint"></p>
          <label class="admin-signup-toggle">
            <input type="checkbox" name="openSignup" ${openSignup ? "checked" : ""} />
            <span data-i18n="admin.openSignup"></span>
          </label>
          <p class="admin-signup-toggle__status" role="status" hidden></p>
        </div>
      </div>
    `;
    translatePage(container);
    renderRows();
    bindNewUserForm();
    bindSignupToggle();
  }

  function renderRows() {
    const list = container.querySelector(".admin-users");
    list.innerHTML = "";

    for (const u of users) {
      const li = document.createElement("li");
      li.className = "admin-user";
      li.dataset.userId = u.id;
      const initial = (u.display_name || u.username || "?").trim().charAt(0).toUpperCase();
      li.innerHTML = `
        <span class="admin-user__avatar" aria-hidden="true">${escapeHtml(initial)}</span>
        <span class="admin-user__who">
          <span class="admin-user__name">${escapeHtml(u.display_name)}${
            u.is_self ? ` <span class="admin-user__you">${escapeHtml(t("members.you"))}</span>` : ""
          }</span>
          <span class="admin-user__username">@${escapeHtml(u.username)}</span>
        </span>
        <span class="admin-user__meta">
          ${u.is_admin ? `<span class="admin-user__badge">${escapeHtml(t("admin.badge"))}</span>` : ""}
          <span class="admin-user__trips">${escapeHtml(t("admin.tripCount", { count: u.trip_count }, u.trip_count))}</span>
        </span>
        <span class="admin-user__actions"></span>
      `;
      list.appendChild(li);
      renderRowMenu(li.querySelector(".admin-user__actions"), u);
    }
  }

  function renderRowMenu(slot, u) {
    renderMenu(slot, {
      iconName: "ellipsis-vertical",
      chevron: false,
      triggerClass: "admin-user__trigger",
      label: "",
      ariaLabel: "admin.actions",
      // All actions, no radio group: unlike a trip role, "is an admin" is a
      // toggle rather than a choice between named states, so it reads better as
      // one item whose label says what it will do.
      items: [
        {
          value: "toggle-admin",
          label: t(u.is_admin ? "admin.revokeAdmin" : "admin.grantAdmin"),
          iconName: "shield-user",
          action: true,
        },
        { value: "reset-password", label: t("admin.resetPassword"), iconName: "key-round", action: true },
        { value: "delete", label: t("admin.deleteUser"), iconName: "trash-2", action: true, danger: true },
      ],
      onSelect: (value) => {
        if (value === "toggle-admin") return toggleAdmin(u);
        if (value === "reset-password") return resetPassword(u);
        if (value === "delete") return deleteUser(u);
      },
    });
  }

  async function toggleAdmin(u) {
    try {
      await api.patch(`/admin/users/${u.id}`, { is_admin: !u.is_admin });
    } catch (err) {
      reportRowError(err);
      return;
    }
    // Demoting yourself removes your own access to this screen, so there is
    // nothing here to re-render into.
    if (u.is_self && u.is_admin) {
      navigate("/trips");
      return;
    }
    await load();
  }

  async function resetPassword(u) {
    const password = await promptDialog({
      message: t("admin.resetPasswordPrompt", { name: u.display_name }),
      confirmKey: "common.save",
    });
    if (password === null) return;
    if (password.length < 8) {
      showAccountsMessage(".admin-users__error", t("admin.passwordTooShort"));
      return;
    }
    try {
      await api.post(`/admin/users/${u.id}/password`, { password });
    } catch (err) {
      reportRowError(err);
      return;
    }
    reportRowMessage(t("admin.passwordReset", { name: u.display_name }));
  }

  async function deleteUser(u) {
    // The trip count is in the question because deleting an account destroys
    // the trips it owns, and that is not something to discover afterwards.
    const message =
      u.trip_count > 0
        ? t("admin.deleteConfirmWithTrips", { name: u.display_name, count: u.trip_count }, u.trip_count)
        : t("admin.deleteConfirm", { name: u.display_name });
    if (!(await confirmDialog({ message, confirmKey: "common.delete", danger: true }))) return;
    try {
      await api.delete(`/admin/users/${u.id}`);
    } catch (err) {
      reportRowError(err);
      return;
    }
    if (u.is_self) {
      // You have just deleted your own account; the session is gone with it.
      window.location.href = "/";
      return;
    }
    await load();
  }

  function bindNewUserForm() {
    const form = container.querySelector(".admin-new-user");
    const error = container.querySelector(".admin-new-user__error");

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      error.hidden = true;
      const username = form.username.value.trim();
      if (!username || form.password.value.length < 8) {
        error.textContent = t("admin.createInvalid");
        error.hidden = false;
        return;
      }
      try {
        await api.post("/admin/users", {
          username,
          display_name: form.displayName.value.trim() || undefined,
          password: form.password.value,
          is_admin: form.isAdmin.checked,
        });
      } catch (err) {
        const code = err instanceof ApiError ? err.body?.code : null;
        error.textContent = code === "username_taken" ? t("admin.usernameTaken", { username }) : t("admin.createFailed");
        error.hidden = false;
        return;
      }
      form.reset();
      await load();
    });
  }

  function bindSignupToggle() {
    const box = container.querySelector('[name="openSignup"]');
    const status = container.querySelector(".admin-signup-toggle__status");

    box.addEventListener("change", async () => {
      status.hidden = true;
      try {
        await api.put("/admin/settings/open-signup", { open_signup: box.checked });
      } catch {
        // Put the checkbox back rather than leaving it showing a state the
        // server does not hold.
        box.checked = !box.checked;
        status.textContent = t("admin.settingFailed");
        status.hidden = false;
        return;
      }
      openSignup = box.checked;
      status.textContent = t(openSignup ? "admin.signupOpened" : "admin.signupClosed");
      status.hidden = false;
    });
  }

  // Row outcomes report in the Accounts card, split into an alert and a status
  // region the way password-field.js does — a success rendered in something
  // styled as an error callout was the other half of the same mistake.
  //
  // Elements are looked up per call rather than captured: render() rebuilds the
  // card, so a reference taken at bind time would be detached.
  function reportRowError(err) {
    const code = err instanceof ApiError ? err.body?.code : null;
    const message =
      {
        last_admin: t("admin.lastAdmin"),
        no_local_password: t("admin.noLocalPassword"),
      }[code] || t("admin.actionFailed");
    showAccountsMessage(".admin-users__error", message);
  }

  function reportRowMessage(message) {
    showAccountsMessage(".admin-users__status", message);
  }

  function showAccountsMessage(selector, message) {
    // Only one at a time: an old success sitting above a fresh failure would
    // read as both having just happened.
    for (const sel of [".admin-users__error", ".admin-users__status"]) {
      const el = container.querySelector(sel);
      if (el) el.hidden = true;
    }
    const el = container.querySelector(selector);
    if (!el) return;
    el.textContent = message;
    el.hidden = false;
    el.scrollIntoView({ block: "nearest" });
  }

  await load();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
