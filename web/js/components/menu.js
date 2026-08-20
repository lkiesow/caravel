import { translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// A small single-select dropdown menu: a button that shows the currently
// selected option, and a popup list to change it.
//
// This is the generalized version of the popup behavior that previously
// only existed inline in user-menu.js - `hidden`-attribute visibility,
// aria-expanded kept in sync with it, and outside-click/Escape listeners
// added on open and removed again on close (so a closed menu leaves
// nothing attached to `document`). user-menu.js still has its own copy;
// folding it onto this component is a todo.md item.
//
// By default the trigger label is owned by this component: it re-renders to
// the selected option's label on every selection, which is why `items`
// carries resolved label strings rather than i18n keys - callers translate
// first (labels here come from several different key namespaces).
//
// items: [{ value, label, iconName }]. onSelect is called with the value only
// when it actually changes, so callers don't have to guard against
// re-selecting the current option.
//
// An item's optional `iconName` takes the leading slot that otherwise holds
// the check mark. Where the items are things that also exist elsewhere in the
// UI with an icon - the tab bar's overflow sections - dropping the icon on the
// way into the menu makes the same destination look like a different one, so
// the icon wins the slot and the current item is marked by styling instead
// (`aria-checked` still carries it for assistive tech either way).
//
// `neutralValue` names the option that means "no choice made" (e.g. the
// locations filter's "all"). While anything else is selected, the trigger
// takes the accent color, so a collapsed icon-only trigger - whose label is
// visually hidden under 640px - still shows that a filter is narrowing the
// list. Omit it and the trigger never highlights.
//
// The remaining options exist for the trip tab bar's "More" menu (Stage 09
// Milestone 6 follow-up), which is the same popup with a different shape:
//
// - `label` pins the trigger's text, instead of it tracking the selection.
//   "More" has to keep saying "More" even while Documents is the open
//   section, and the *item* rows carry the which-one-is-current signal.
// - `chevron: false` drops the caret. In the tab bar the trigger is one cell
//   of five, sized like its neighbours, and the three-dot icon already says
//   "there is more here".
// - `triggerClass` replaces the default button classes, so the trigger can
//   be styled as a tab rather than as a secondary button.
// - `className` lands on the `.menu` root for variant styling (positioning
//   the dropdown, and how the current row is marked).
//
// Whenever the popup is open the trigger also carries
// `menu__trigger--open`, so an engaged menu reads as engaged. That applies
// to every caller, not just the tab bar - a menu you've just opened looking
// inert was never deliberate.
export function renderMenu(
  container,
  { iconName, items, activeValue, neutralValue, ariaLabel, onSelect, label, chevron = true, triggerClass = "btn btn-secondary btn-collapse", className = "" }
) {
  let active = activeValue;

  container.innerHTML = `
    <div class="menu${className ? ` ${className}` : ""}">
      <button type="button" class="menu__trigger${triggerClass ? ` ${triggerClass}` : ""}" data-action="toggle" aria-haspopup="menu" aria-expanded="false"${ariaLabel ? ` data-i18n-aria-label="${ariaLabel}"` : ""}>
        ${iconName ? icon(iconName) : ""}
        <span class="menu__label"></span>
        ${chevron ? icon("chevron-down", { className: "menu__chevron" }) : ""}
      </button>
      <ul class="menu__dropdown" role="menu" hidden>
        ${items
          .map(
            (item) => `
          <li role="none">
            <button type="button" role="menuitemradio" data-value="${escapeAttr(item.value)}" aria-checked="${item.value === active}">
              ${item.iconName ? icon(item.iconName, { className: "menu__item-icon" }) : icon("check", { className: "menu__check" })}
              <span></span>
            </button>
          </li>
        `
          )
          .join("")}
      </ul>
    </div>
  `;
  translatePage(container);

  const trigger = container.querySelector('[data-action="toggle"]');
  const dropdown = container.querySelector(".menu__dropdown");
  const labelEl = container.querySelector(".menu__label");

  // Labels are set as textContent rather than interpolated into the
  // template above so no escaping is needed for user-facing copy.
  items.forEach((item, i) => {
    dropdown.querySelectorAll("[data-value] span")[i].textContent = item.label;
  });
  syncLabel();

  function syncLabel() {
    labelEl.textContent = label ?? items.find((item) => item.value === active)?.label ?? "";
    trigger.classList.toggle("menu__trigger--active", neutralValue !== undefined && active !== neutralValue);
    dropdown.querySelectorAll("[data-value]").forEach((btn) => {
      btn.setAttribute("aria-checked", String(btn.getAttribute("data-value") === active));
    });
  }

  function close() {
    dropdown.hidden = true;
    trigger.classList.remove("menu__trigger--open");
    trigger.setAttribute("aria-expanded", "false");
    document.removeEventListener("click", onOutsideClick);
    document.removeEventListener("keydown", onKeydown);
  }

  function open() {
    dropdown.hidden = false;
    trigger.classList.add("menu__trigger--open");
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

  dropdown.querySelectorAll("[data-value]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const value = btn.getAttribute("data-value");
      close();
      if (value === active) return;
      active = value;
      syncLabel();
      onSelect?.(value);
    });
  });
}

function escapeAttr(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
