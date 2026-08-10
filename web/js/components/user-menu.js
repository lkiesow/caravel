import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// Renders an initials-avatar user menu into `container`, at the far right
// of the header. Clicking the avatar toggles a small dropdown - just
// "Log out" for now, structured as a list so more items (settings, etc.)
// can be added later without restructuring.
export function renderUserMenu(container, user, { onLogout }) {
  const initial = (user.display_name || "?").trim().charAt(0).toUpperCase();

  container.innerHTML = `
    <div class="user-menu">
      <button type="button" class="user-menu__trigger" data-action="toggle" aria-haspopup="menu" aria-expanded="false" data-i18n-aria-label="auth.userMenu">
        <span class="user-menu__avatar">${escapeHtml(initial)}</span>
        <span class="user-menu__name">${escapeHtml(user.display_name)}</span>
        ${icon("chevron-down")}
      </button>
      <ul class="user-menu__dropdown" role="menu" hidden>
        <li role="none">
          <button type="button" role="menuitem" data-action="logout">${icon("log-out")} <span data-i18n="auth.logout"></span></button>
        </li>
      </ul>
    </div>
  `;
  translatePage(container);

  const trigger = container.querySelector('[data-action="toggle"]');
  const dropdown = container.querySelector(".user-menu__dropdown");

  function close() {
    dropdown.hidden = true;
    trigger.setAttribute("aria-expanded", "false");
    document.removeEventListener("click", onOutsideClick);
    document.removeEventListener("keydown", onKeydown);
  }

  function open() {
    dropdown.hidden = false;
    trigger.setAttribute("aria-expanded", "true");
    document.addEventListener("click", onOutsideClick);
    document.addEventListener("keydown", onKeydown);
  }

  function onOutsideClick(e) {
    if (!container.contains(e.target)) close();
  }

  function onKeydown(e) {
    if (e.key === "Escape") close();
  }

  trigger.addEventListener("click", (e) => {
    e.stopPropagation();
    if (dropdown.hidden) open();
    else close();
  });

  container.querySelector('[data-action="logout"]').addEventListener("click", () => {
    close();
    onLogout?.();
  });
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
