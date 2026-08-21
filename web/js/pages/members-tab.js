import { api, ApiError } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { renderMenu } from "../components/menu.js";
import { confirmDialog } from "../components/dialog.js";
import { renderLoading } from "../components/loading.js";
import { canManageMembers } from "../trip-role.js";

// Who else is on this trip, and what they may do.
//
// Two shapes in one tab, decided by canManageMembers(trip):
//
//   owner    - an add form, and a per-row ⋮ menu holding the role radio group
//              plus a danger Remove
//   everyone - the same list, read-only, plus a "Leave trip" button for their
//              own membership
//
// The owner's own row is inert in both shapes. They have no trip_members row to
// change (see migration 0007) and the server refuses to remove them, so
// offering either control would be offering something that cannot work.
export function renderMembersTab(content, trip) {
  let members = [];

  async function load() {
    renderLoading(content);
    members = await api.get(`/trips/${trip.id}/members`);
    render();
  }

  function render() {
    const manage = canManageMembers(trip);

    content.innerHTML = `
      <div class="editor-card">
        <h2 data-i18n="members.heading"></h2>
        <p class="editor-card__hint" data-i18n="${manage ? "members.hintOwner" : "members.hintMember"}"></p>
        <ul class="members"></ul>
      </div>
      ${
        manage
          ? `<div class="editor-card">
        <h2 data-i18n="members.addHeading"></h2>
        <p class="editor-card__hint" data-i18n="members.addHint"></p>
        <form class="members-add">
          <label class="members-add__field">
            <span data-i18n="members.username"></span>
            <input type="text" name="username" autocomplete="off" autocapitalize="none" spellcheck="false" required />
          </label>
          <label class="members-add__field">
            <span data-i18n="members.role"></span>
            <select name="role">
              <option value="editor" data-i18n="members.role.editor"></option>
              <option value="viewer" data-i18n="members.role.viewer"></option>
            </select>
          </label>
          <button type="submit" class="btn btn-primary">${icon("user-plus")} <span data-i18n="members.add"></span></button>
        </form>
        <p class="members-add__error" role="alert" hidden></p>
      </div>`
          : ""
      }
    `;
    translatePage(content);

    renderRows(manage);
    if (manage) bindAddForm();
  }

  function renderRows(manage) {
    const list = content.querySelector(".members");
    list.innerHTML = "";

    for (const m of members) {
      const li = document.createElement("li");
      li.className = "member-card";
      li.dataset.userId = m.user_id;

      const initial = (m.display_name || m.username || "?").trim().charAt(0).toUpperCase();
      const isOwnerRow = m.role === "owner";
      li.innerHTML = `
        <span class="member-card__avatar" aria-hidden="true">${escapeHtml(initial)}</span>
        <span class="member-card__who">
          <span class="member-card__name">${escapeHtml(m.display_name)}${
            m.is_self ? ` <span class="member-card__you">${escapeHtml(t("members.you"))}</span>` : ""
          }</span>
          <span class="member-card__username">@${escapeHtml(m.username)}</span>
        </span>
        <span class="member-card__role">${escapeHtml(t(`members.role.${m.role}`))}</span>
        <span class="member-card__actions"></span>
      `;
      list.appendChild(li);

      const actions = li.querySelector(".member-card__actions");
      // The owner's row carries no control for anyone, including themselves.
      if (isOwnerRow) continue;

      if (manage) {
        renderRowMenu(actions, m);
      } else if (m.is_self) {
        // Leaving is the one write a non-owner may make here, and only on
        // their own membership — so it is a plain button rather than a menu
        // with a single item in it.
        const btn = document.createElement("button");
        btn.className = "btn btn-secondary btn-collapse";
        btn.dataset.action = "leave";
        btn.innerHTML = `${icon("log-out")} <span>${escapeHtml(t("members.leave"))}</span>`;
        btn.addEventListener("click", () => leave(m));
        actions.appendChild(btn);
      }
    }
  }

  function renderRowMenu(slot, m) {
    renderMenu(slot, {
      iconName: "ellipsis-vertical",
      chevron: false,
      // renderMenu takes an i18n *key* here and translates it declaratively,
      // so this cannot name the person. A static label is honest; the row it
      // sits in already says who it belongs to.
      ariaLabel: "members.actions",
      // A radio group for the role plus one danger action, which is the mix
      // menu.js supports directly — the same shape as a file row's ⋮.
      items: [
        { value: "editor", label: t("members.role.editor"), iconName: "pencil" },
        { value: "viewer", label: t("members.role.viewer"), iconName: "file-text" },
        { value: "remove", label: t("members.remove"), iconName: "trash-2", action: true, danger: true },
      ],
      activeValue: m.role,
      onSelect: async (value) => {
        if (value === "remove") return remove(m);
        if (value === m.role) return;
        await api.put(`/trips/${trip.id}/members/${m.user_id}`, { role: value });
        await load();
      },
    });
  }

  async function remove(m) {
    if (!(await confirmDialog({ message: t("members.removeConfirm", { name: m.display_name }), confirmKey: "members.confirmRemove", danger: true }))) return;
    await api.delete(`/trips/${trip.id}/members/${m.user_id}`);
    await load();
  }

  async function leave(m) {
    if (!(await confirmDialog({ message: t("members.leaveConfirm", { title: trip.title }), confirmKey: "members.confirmLeave", danger: true }))) return;
    await api.delete(`/trips/${trip.id}/members/${m.user_id}`);
    // Access is gone the moment that returns, so staying on the trip would
    // just render a 404. The trips list is the only honest destination.
    navigate("/trips");
  }

  function bindAddForm() {
    const form = content.querySelector(".members-add");
    const error = content.querySelector(".members-add__error");

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      error.hidden = true;
      const username = form.username.value.trim();
      if (!username) return;

      try {
        await api.post(`/trips/${trip.id}/members`, { username, role: form.role.value });
        form.reset();
        await load();
      } catch (err) {
        // The server's `code` is what distinguishes these — two of them share
        // a 409, and the message alone is not something to branch on.
        const code = err instanceof ApiError ? err.body?.code : null;
        const key =
          {
            user_not_found: "members.error.noSuchUser",
            already_member: "members.error.alreadyMember",
            already_owner: "members.error.alreadyOwner",
          }[code] || "members.error.addFailed";
        error.textContent = t(key, { username });
        error.hidden = false;
      }
    });
  }

  load();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
