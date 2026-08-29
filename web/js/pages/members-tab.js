import { api, ApiError } from "../api.js";
import { guardClick, guardForm } from "../busy.js";
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
// onMembersChanged reports the non-owner member count back to the caller after
// every load, because `trip` is fetched once when the trip page opens and other
// tabs read `member_count` off it. Without this, adding the first member and then
// switching to Files showed the solo view — no visibility grouping, no upload
// choice — until the page was reloaded. The Files tab reads the count when it
// renders, which happens on tab switch, so updating the shared object in place is
// enough; nothing on screen right now depends on it.
export function renderMembersTab(content, trip, { onMembersChanged } = {}) {
  let members = [];

  async function load() {
    renderLoading(content);
    members = await api.get(`/trips/${trip.id}/members`);
    // The list includes the owner, who is not a member row (see migration
    // 0007), so the count the rest of the app means is one less.
    onMembersChanged?.(Math.max(0, members.length - 1));
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
            <!-- name="member", not "username", and no "user" in the id either.
                 Firefox classifies a field as a login username field from its
                 name and id, fills username-only forms from the saved login,
                 and ignores autocomplete="off" while doing it — so this field
                 came up pre-filled with the signed-in user's own handle, which
                 is never the answer to "who do you want to add?". The admin
                 screen keeps name="username" because that form really is
                 creating an account. -->
            <input type="text" name="member" id="member-add-field" list="member-suggestions"
                   autocomplete="off" autocapitalize="none" spellcheck="false" required />
            <datalist id="member-suggestions"></datalist>
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
        guardClick(btn, () => leave(m));
        actions.appendChild(btn);
      }
    }
  }

  function renderRowMenu(slot, m) {
    renderMenu(slot, {
      iconName: "ellipsis-vertical",
      chevron: false,
      triggerClass: "member-card__trigger",
      // An empty string, not omitted: renderMenu falls back to the *selected
      // item's* label when `label` is nullish, which made every row show its
      // role twice — once as .member-card__role and again on the button beside
      // it. "" pins the trigger to no text at all, which is what a per-row ⋮
      // is everywhere else in the app. The current role is still marked inside
      // the open menu, by aria-checked and the check mark.
      label: "",
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
    // Removing someone deletes their personal files on this trip, and the count
    // goes in the question rather than being discovered afterwards. Same
    // reasoning as the admin screen quoting a trip count before deleting an
    // account: a confirmation that hides what it destroys is not a
    // confirmation.
    const count = m.personal_file_count || 0;
    const message =
      count > 0
        ? t("members.removeConfirmWithFiles", { name: m.display_name, count }, count)
        : t("members.removeConfirm", { name: m.display_name });
    if (!(await confirmDialog({ message, confirmKey: "members.confirmRemove", danger: true }))) return;
    await api.delete(`/trips/${trip.id}/members/${m.user_id}`);
    await load();
  }

  async function leave(m) {
    // Leaving destroys your own personal files on the trip too, so say so.
    const count = m.personal_file_count || 0;
    const message =
      count > 0
        ? t("members.leaveConfirmWithFiles", { title: trip.title, count }, count)
        : t("members.leaveConfirm", { title: trip.title });
    // Still danger -- it is irreversible and it does destroy your personal
    // files here -- but not a bin: leaving takes you off the trip, it does not
    // delete the trip.
    if (!(await confirmDialog({ message, confirmKey: "members.confirmLeave", danger: true, iconName: "log-out" })))
      return;
    await api.delete(`/trips/${trip.id}/members/${m.user_id}`);
    // Access is gone the moment that returns, so staying on the trip would
    // just render a 404. The trips list is the only honest destination.
    navigate("/trips");
  }

  function bindAddForm() {
    const form = content.querySelector(".members-add");
    const error = content.querySelector(".members-add__error");
    bindSuggestions(form);

    guardForm(form, async () => {
      error.hidden = true;
      const username = form.elements.member.value.trim();
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

  // Suggestions come from GET /users/search, which searches every account on
  // the instance (see handleSearchUsers for why that scope was chosen).
  //
  // A native <datalist> rather than a custom popup: it gets keyboard handling,
  // screen-reader announcement and mobile behaviour from the browser, none of
  // which a hand-rolled combobox gets for free — and this is a convenience on
  // top of a field that already works when typed in full.
  function bindSuggestions(form) {
    const input = form.elements.member;
    const datalist = form.querySelector("#member-suggestions");
    let timer = null;
    let lastQuery = null;

    input.addEventListener("input", () => {
      const query = input.value.trim();
      // Debounced, because this fires per keystroke and each one is a query.
      clearTimeout(timer);
      if (query.length < 2) {
        datalist.innerHTML = "";
        lastQuery = null;
        return;
      }
      if (query === lastQuery) return;
      timer = setTimeout(async () => {
        lastQuery = query;
        let found;
        try {
          found = await api.get(`/users/search?q=${encodeURIComponent(query)}`);
        } catch {
          // A failed lookup leaves the last suggestions in place and stays
          // silent: the field is fully usable by typing a username out, so an
          // error banner here would report a problem the user does not have.
          return;
        }
        // Stale response guard — a slower earlier request must not overwrite a
        // newer one's results.
        if (input.value.trim() !== query) return;
        // People already on the trip are filtered out client-side: we have the
        // member list right here, and suggesting someone whose only outcome is
        // an "already on this trip" error is a worse hint than no hint.
        const onTrip = new Set(members.map((m) => m.username));
        datalist.innerHTML = "";
        for (const u of found) {
          if (onTrip.has(u.username)) continue;
          const option = document.createElement("option");
          // value is what lands in the field; the text is what the browser
          // shows beside it, so the list reads as people rather than handles.
          option.value = u.username;
          option.textContent = u.display_name;
          datalist.appendChild(option);
        }
      }, 200);
    });
  }

  load();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
