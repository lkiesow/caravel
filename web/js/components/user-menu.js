import { t } from "../i18n.js";
import { navigate } from "../router.js";
import { renderMenu } from "./menu.js";

// Renders an initials-avatar user menu into `container`, at the far right of
// the header. Clicking the avatar opens a small dropdown: account settings and
// Log out.
//
// This is a thin wrapper over components/menu.js: until Stage 12 Milestone 1
// it carried its own copy of the popup behaviour (hidden-attribute
// visibility, aria-expanded, outside-click and Escape listeners) plus a
// duplicate .user-menu__dropdown stylesheet, which is exactly the
// half-migrated state todo.md had been warning about since Stage 06. What is
// left here is what is actually specific to this menu: the avatar in the
// trigger, and that logging out is an *action* rather than a selection -
// menu.js's action-item mode (Stage 11 Milestone 3) is what made the
// migration possible.
export function renderUserMenu(container, user, { onLogout }) {
  const initial = (user.display_name || "?").trim().charAt(0).toUpperCase();

  renderMenu(container, {
    className: "menu--user",
    triggerClass: "user-menu__trigger",
    // menu.js takes this as trusted markup, so the initial is escaped here.
    triggerPrefixHtml: `<span class="user-menu__avatar">${escapeHtml(initial)}</span>`,
    // The trigger shows who you are, not which item you last picked, so the
    // label is pinned rather than tracking a selection.
    label: user.display_name,
    ariaLabel: "auth.userMenu",
    // Both are actions, not a selection: neither is a state the menu is now
    // in. Settings leads because it is the one you might use twice.
    items: [
      { value: "settings", label: t("settings.title"), iconName: "settings", action: true },
      // Only for an admin, and only as a convenience: the page re-checks and
      // the server checks again on every /api/admin route. A menu item is not
      // a permission.
      ...(user.is_admin ? [{ value: "admin", label: t("admin.title"), iconName: "shield-user", action: true }] : []),
      { value: "logout", label: t("auth.logout"), iconName: "log-out", action: true },
    ],
    onSelect: (value) => {
      if (value === "settings") navigate("/settings");
      if (value === "admin") navigate("/admin");
      // Returned, not just called: menu.js keeps its guard held for as long as
      // this resolves, so the logout POST is what the ⋮ stays busy for.
      if (value === "logout") return onLogout?.();
    },
  });
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
