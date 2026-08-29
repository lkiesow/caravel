import { createGuard } from "../busy.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { bindPopup } from "./popup.js";

// The locations tab's filters, behind one trigger.
//
// Why this exists rather than more options on menu.js: the toolbar is one
// deliberately non-wrapping row that has to fit 324px (see locations-tab.js),
// and it already held a search box, two filter buttons and "New location".
// Every filter added past that made it wider, and there are two more coming.
//
// Why it drills down rather than flying out. The obvious shape for a menu of
// menus is a submenu opening to the side, and at 324px there is no side to open
// into - a second panel would have nowhere to go, and hover-opened flyouts are
// a poor fit for a finger in any case. So there is one panel at a time: the
// root lists the groups, activating one replaces the panel with that group's
// options plus a back row, and choosing an option returns to the root.
//
// Each root row doubles as the current-value display - "All categories", "Any
// distance" - so the state of every filter is readable without opening
// anything, which is what the two separate trigger buttons used to give for
// free. The trigger takes menu.js's accent treatment when *any* group is off
// its neutral value, reusing the same CSS.
//
//   renderFilterMenu(container, { ariaLabel, title, groups })
//
// A group is:
//   { key, name, neutralLabel, neutralValue, activeValue, items: [{ value, label }],
//     onSelect, renderPanel, currentLabel }
//
// `name` and `neutralLabel` are two different strings and both are needed.
// `name` is what the filter *is* ("Distance") and titles the back row;
// `neutralLabel` is what its root row reads while nothing is chosen ("Any
// distance"). Collapsing them makes one of the two read badly: a back row
// saying "Any distance" above an option also saying "Any distance" is the same
// sentence twice, and a root row saying only "Distance" throws away the
// current-value display that is the reason these rows are worth having.
//
// `items` is the ordinary case: a list of single-select options, rendered as
// menuitemradio rows exactly as menu.js does. `renderPanel(panel, { close })`
// is the escape hatch for a group whose options are not a list - the date
// filter needs presets plus a pair of date inputs - and `currentLabel()` lets
// such a group say what its row should read when its state is not one of a
// fixed set of items.
//
// Returns { setActive(groupKey, value), refresh() }. setActive is for a group
// that cannot honor what was picked: the distance filter needs the device's
// position and must fall back to "any distance" if it is refused, the same case
// menu.js's setActive exists for. refresh() re-reads the groups, for when the
// available options change under the menu - the tag list grows as locations are
// tagged.
export function renderFilterMenu(container, { ariaLabel, title, groups }) {
  container.innerHTML = `
    <div class="menu menu--filter">
      <button type="button" class="menu__trigger btn btn-secondary btn-collapse" data-action="toggle"
              aria-haspopup="menu" aria-expanded="false"${ariaLabel ? ` data-i18n-aria-label="${ariaLabel}"` : ""}>
        ${icon("funnel")}
        <span class="menu__label"></span>
        ${icon("chevron-down", { className: "menu__chevron" })}
      </button>
      <div class="menu__dropdown menu--filter__panel" role="menu" hidden></div>
    </div>
  `;
  translatePage(container);

  const trigger = container.querySelector('[data-action="toggle"]');
  const panel = container.querySelector(".menu__dropdown");
  const labelEl = container.querySelector(".menu__label");
  labelEl.textContent = t(title);

  // Every opening starts at the root. Reopening inside a submenu somebody left
  // drilled into would be a menu remembering a navigation nobody asked it to
  // remember - and doing this on the way in rather than on the way out means
  // "open" and "which panel" cannot drift apart.
  const popup = bindPopup(container, trigger, panel, { onOpen: () => renderRoot() });

  // Matches menu.js: one guard per menu, so two rows cannot race each other.
  const guard = createGuard({ elements: () => [trigger, ...panel.querySelectorAll("button")] });

  function activeGroups() {
    return groups.filter((g) => g.available !== false);
  }

  // What a group's root row reads.
  //
  // At its neutral value the row shows the group's own label - "All
  // categories", "Any distance" - and NOT the neutral item's label, which is
  // the bare "All" that reads fine inside the category panel and says nothing
  // in a list of four filters. Once something is chosen the row shows that
  // choice ("Stay", "Within 5 km"), which names its filter well enough on its
  // own. A group with a `currentLabel` computes its own, for the date filter,
  // whose state is a range rather than one of a fixed set of items.
  function groupLabel(group) {
    if (group.currentLabel) return group.currentLabel();
    if (isNeutral(group)) return group.neutralLabel;
    return group.items?.find((i) => i.value === group.activeValue)?.label ?? group.neutralLabel;
  }

  function isNeutral(group) {
    if (group.isNeutral) return group.isNeutral();
    return group.activeValue === group.neutralValue;
  }

  function syncTrigger() {
    trigger.classList.toggle(
      "menu__trigger--active",
      activeGroups().some((g) => !isNeutral(g))
    );
  }

  function renderRoot() {
    panel.replaceChildren();
    for (const group of activeGroups()) {
      const row = document.createElement("button");
      row.type = "button";
      row.className = "menu--filter__row";
      row.setAttribute("role", "menuitem");
      // Two levels means the row both *is* the current value and opens a
      // panel, so it needs to say the second part out loud - the chevron is
      // decorative and a screen reader would otherwise hear only the value.
      row.setAttribute("aria-haspopup", "true");
      row.dataset.group = group.key;
      if (!isNeutral(group)) row.classList.add("menu--filter__row--active");

      const label = document.createElement("span");
      label.className = "menu--filter__row-label";
      label.textContent = groupLabel(group);
      row.append(label);
      row.insertAdjacentHTML("beforeend", icon("chevron-down", { className: "menu--filter__row-chevron" }));

      row.addEventListener("click", (e) => {
        e.stopPropagation();
        renderGroup(group);
      });
      panel.appendChild(row);
    }
    syncTrigger();
  }

  function renderGroup(group) {
    panel.replaceChildren();

    const back = document.createElement("button");
    back.type = "button";
    back.className = "menu--filter__back";
    back.setAttribute("role", "menuitem");
    back.insertAdjacentHTML("afterbegin", icon("arrow-left", { className: "menu--filter__back-icon" }));
    const backLabel = document.createElement("span");
    // Names the group it is leaving rather than saying only "Back", so the
    // panel says which filter it is even when the options themselves are
    // ambiguous ("All", "Any").
    backLabel.textContent = group.name;
    back.append(backLabel);
    back.addEventListener("click", (e) => {
      e.stopPropagation();
      renderRoot();
      panel.querySelector(`[data-group="${group.key}"]`)?.focus();
    });
    panel.appendChild(back);

    if (group.renderPanel) {
      const body = document.createElement("div");
      body.className = "menu--filter__body";
      panel.appendChild(body);
      group.renderPanel(body, {
        done: () => {
          renderRoot();
          popup.close();
        },
      });
    } else {
      for (const item of group.items) {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.setAttribute("role", "menuitemradio");
        btn.setAttribute("aria-checked", String(item.value === group.activeValue));
        btn.dataset.value = item.value;
        btn.insertAdjacentHTML("afterbegin", icon("check", { className: "menu__check" }));
        const label = document.createElement("span");
        label.textContent = item.label;
        btn.append(label);
        btn.addEventListener("click", (e) => {
          e.stopPropagation();
          popup.close();
          if (item.value === group.activeValue) return;
          // Optimistic, and before the call, exactly as menu.js is: the
          // selection is what was just chosen, and a caller that cannot honor
          // it says so through setActive().
          group.activeValue = item.value;
          renderRoot();
          return guard.run(() => group.onSelect?.(item.value));
        });
        panel.appendChild(btn);
      }
    }

    // Focus lands inside the panel that just replaced the one under the
    // pointer, so the keyboard is where the eye is.
    panel.querySelector("button:not(.menu--filter__back)")?.focus();
  }

  renderRoot();

  return {
    setActive(groupKey, value) {
      const group = groups.find((g) => g.key === groupKey);
      if (!group || group.activeValue === value) return;
      group.activeValue = value;
      if (!popup.isOpen()) renderRoot();
      else syncTrigger();
    },
    refresh() {
      if (!popup.isOpen()) renderRoot();
      else syncTrigger();
    },
  };
}
