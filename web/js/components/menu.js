import { translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// A small single-select dropdown menu: a button that shows the currently
// selected option, and a popup list to change it.
//
// This is the generalized version of the popup behavior that previously
// only existed inline in user-menu.js - `hidden`-attribute visibility,
// aria-expanded kept in sync with it, and outside-click/Escape listeners
// added on open and removed again on close (so a closed menu leaves
// nothing attached to `document`). As of Stage 12 Milestone 1 user-menu.js
// is a caller rather than a second copy, so this is the only popup
// implementation in the tree.
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
// An item marked `action: true` is not a selection: "Delete" is something the
// menu *does*, not a state it is now in, so `role="menuitemradio"` and
// `aria-checked` would both be lies. Action items render as plain
// `role="menuitem"`, never take the checked styling, and fire onSelect on every
// click - the "don't re-fire the current value" guard below is exactly wrong
// for them, since pressing Delete twice has to mean twice. `danger: true`
// tints one red, for the destructive one in the list. Radio and action items
// can share a menu; only the action ones opt out.
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
//   "More" has to keep saying "More" even while Files is the open
//   section, and the *item* rows carry the which-one-is-current signal.
// - `chevron: false` drops the caret. In the tab bar the trigger is one cell
//   of five, sized like its neighbours, and the three-dot icon already says
//   "there is more here".
// - `triggerClass` replaces the default button classes, so the trigger can
//   be styled as a tab rather than as a secondary button.
// - `className` lands on the `.menu` root for variant styling (positioning
//   the dropdown, and how the current row is marked).
//
// `triggerPrefixHtml` is trusted markup placed at the head of the trigger,
// before the label. It exists for the header's user menu, whose trigger leads
// with an initials avatar rather than with an icon - `iconName` can only name
// something in the sprite. Trusted means what it says: the caller escapes
// anything user-supplied (see user-menu.js) before handing it over.
//
// Whenever the popup is open the trigger also carries
// `menu__trigger--open`, so an engaged menu reads as engaged. That applies
// to every caller, not just the tab bar - a menu you've just opened looking
// inert was never deliberate.
export function renderMenu(
  container,
  { iconName, items, activeValue, neutralValue, ariaLabel, onSelect, label, chevron = true, triggerClass = "btn btn-secondary btn-collapse", className = "", triggerPrefixHtml = "" }
) {
  let active = activeValue;

  container.innerHTML = `
    <div class="menu${className ? ` ${className}` : ""}">
      <button type="button" class="menu__trigger${triggerClass ? ` ${triggerClass}` : ""}" data-action="toggle" aria-haspopup="menu" aria-expanded="false"${ariaLabel ? ` data-i18n-aria-label="${ariaLabel}"` : ""}>
        ${iconName ? icon(iconName) : ""}
        ${triggerPrefixHtml}
        <span class="menu__label"></span>
        ${chevron ? icon("chevron-down", { className: "menu__chevron" }) : ""}
      </button>
      <ul class="menu__dropdown" role="menu" hidden>
        ${items
          .map(
            (item) => `
          <li role="none">
            <button type="button" ${item.action ? `role="menuitem" class="menu__action${item.danger ? " menu__action--danger" : ""}"` : `role="menuitemradio" aria-checked="${item.value === active}"`} data-value="${escapeAttr(item.value)}">
              ${
                item.iconName
                  ? icon(item.iconName, { className: "menu__item-icon" })
                  : // An action item with no icon gets no leading slot at all,
                    // rather than an invisible checkmark reserving space for a
                    // selection it can never carry. Mixing icon-less action
                    // items with radio ones in one menu would misalign them,
                    // which is a reason to give action items icons, not a
                    // reason for the component to fake one.
                    item.action
                    ? ""
                    : icon("check", { className: "menu__check" })
              }
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
    // Only the radio items carry a checked state; an action item has none to
    // sync, and stamping aria-checked on it would invent one.
    dropdown.querySelectorAll('[role="menuitemradio"]').forEach((btn) => {
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

  dropdown.querySelectorAll("[data-value]").forEach((btn, i) => {
    btn.addEventListener("click", () => {
      const value = btn.getAttribute("data-value");
      close();
      if (items[i].action) {
        onSelect?.(value);
        return;
      }
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
