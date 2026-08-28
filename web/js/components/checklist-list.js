import { api } from "../api.js";
import { guard, guardForm } from "../busy.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { confirmDialog, promptDialog } from "./dialog.js";
import { renderMenu } from "./menu.js";
import { renderLoading } from "./loading.js";

// Renders a trip's checklists: each an "editor-card" with a title, its
// checkable items, an "add item" input, and a per-list ⋮ menu. Plus an "Add
// checklist" form below. `tripId` scopes the list/create calls; item-level calls
// are scoped by their own checklist id (see internal/httpapi/checklists.go's
// route shape).
//
// `readOnly` renders the same lists with nothing that writes: no create forms,
// no menus, and disabled checkboxes — ticking is a write like any other. Same
// option name and shape as file-list.js's.
//
// Visibility (Stage 14 Milestone 8): pass `shared: true` when somebody else is
// on the trip. Lists then group into three labelled sections and each card
// offers the moves between them. Three states, not the files' two, because a
// list can be *ticked*:
//
//   shared    everyone on the trip sees it and ticks it (the default)
//   trip      everyone sees it, only its author ticks it
//   personal  only its author sees it at all
//
// Following the files rework: the grouping carries the state, so no card shows a
// visibility badge and no menu holds a visibility *selection* — each card offers
// the two moves available to it as plain actions. A menu that holds a selection
// cannot also have a silent trigger, and a per-row ⋮ should say nothing.
//
// Which writes a given card allows comes from the server, not from re-deriving
// the rule here: `can_tick` covers ticking, adding, rewording, renaming and
// deleting, and `is_mine` covers changing the visibility. A trip-visible list
// belonging to somebody else is readable and nothing more.
export async function renderChecklistList(container, tripId, { readOnly = false, shared = false } = {}) {
  renderLoading(container);
  let checklists = await api.get(`/trips/${tripId}/checklists`);
  // The visibility a new list gets. Remembered across the re-render that
  // follows creating one, the same way the file drop zone remembers its choice.
  let newVisibility = "shared";

  const SECTIONS = [
    { key: "shared", titleKey: "checklists.section.shared" },
    { key: "trip", titleKey: "checklists.section.trip" },
    { key: "personal", titleKey: "checklists.section.personal" },
  ];

  // The move a card offers, per current state: the other two, named by what
  // taking them does rather than by the state they land in.
  const MOVES = {
    shared: [
      { value: "trip", labelKey: "checklists.makeTrip", iconName: "eye" },
      { value: "personal", labelKey: "checklists.makePersonal", iconName: "lock" },
    ],
    trip: [
      { value: "shared", labelKey: "checklists.makeShared", iconName: "users" },
      { value: "personal", labelKey: "checklists.makePersonal", iconName: "lock" },
    ],
    personal: [
      { value: "shared", labelKey: "checklists.makeShared", iconName: "users" },
      { value: "trip", labelKey: "checklists.makeTrip", iconName: "eye" },
    ],
  };

  function render() {
    container.innerHTML = `
      <div class="checklist-sections"></div>
      <p class="checklists-empty" data-i18n="checklists.empty" hidden></p>
      ${
        readOnly
          ? ""
          : `<div class="checklist-new">
        <form class="checklist-new-form">
          <input type="text" name="title" data-i18n-placeholder="checklists.titlePlaceholder" required />
          <button type="submit" class="btn btn-primary btn-collapse">${icon("plus")} <span data-i18n="checklists.add"></span></button>
        </form>
        ${
          shared
            ? `<div class="checklist-new__visibility">
          <span id="checklist-visibility-label" data-i18n="checklists.newVisibility"></span>
          <div class="setting-choices" role="radiogroup" aria-labelledby="checklist-visibility-label">
            ${SECTIONS.map(
              (sec) => `<label class="setting-choice">
              <input type="radio" name="newVisibility" value="${sec.key}" />
              <span data-i18n="${sec.titleKey}"></span>
            </label>`
            ).join("")}
          </div>
        </div>`
            : ""
        }
      </div>`
      }
    `;
    translatePage(container);

    const sections = container.querySelector(".checklist-sections");
    container.querySelector(".checklists-empty").hidden = checklists.length > 0;

    // Grouped only when the trip is shared; otherwise one unlabelled run, since
    // on a solo trip every list is the same kind of thing.
    const groups = shared
      ? SECTIONS.map((sec) => ({ ...sec, entries: checklists.filter((c) => (c.visibility || "shared") === sec.key) })).filter(
          (g) => g.entries.length > 0
        )
      : [{ key: null, titleKey: null, entries: checklists }];

    for (const group of groups) {
      const wrapper = document.createElement("div");
      wrapper.className = "checklist-section";
      wrapper.dataset.visibility = group.key || "";
      if (group.titleKey) {
        const title = document.createElement("p");
        title.className = "checklist-section__title";
        title.textContent = t(group.titleKey);
        wrapper.appendChild(title);
      }
      for (const checklist of group.entries) renderCard(wrapper, checklist);
      sections.appendChild(wrapper);
    }

    if (readOnly) return;
    bindNewForm();
  }

  function renderCard(parent, checklist) {
    // Both come from the server: can_tick is every write except the visibility,
    // is_mine is the visibility itself.
    const canWrite = !readOnly && checklist.can_tick;
    const canMove = !readOnly && shared && checklist.is_mine;
    // Duplicating asks neither: the copy is a new list on the trip, so being an
    // editor is the whole requirement. Notably true for somebody else's
    // trip-visible list, where canWrite and canMove are both false and this is
    // the only thing the ⋮ holds - which is also the list most worth copying.
    const canDuplicate = !readOnly;

    const card = document.createElement("div");
    card.className = "editor-card checklist-card";
    card.dataset.checklistId = checklist.id;
    card.innerHTML = `
      <div class="checklist-card__header">
        <h2>${escapeHtml(checklist.title)}</h2>
        <span class="checklist-card__actions"></span>
      </div>
      <ul class="checklist-items"></ul>
      ${
        canWrite
          ? `<form class="checklist-item-form">
        <input type="text" name="text" data-i18n-placeholder="checklists.itemPlaceholder" required />
        <button type="submit" class="btn btn-secondary btn-collapse">${icon("plus")} <span data-i18n="checklists.addItem"></span></button>
      </form>`
          : ""
      }
    `;
    translatePage(card);

    const itemsList = card.querySelector(".checklist-items");
    for (const item of checklist.items) renderItem(itemsList, checklist, item, canWrite);

    if (canWrite || canMove || canDuplicate) {
      renderCardMenu(card.querySelector(".checklist-card__actions"), checklist, canWrite, canMove, canDuplicate);
    }

    const itemForm = card.querySelector(".checklist-item-form");
    if (itemForm) {
      guardForm(itemForm, async (e) => {
        const input = e.target.elements.text;
        const text = input.value.trim();
        if (!text) return;
        const item = await api.post(`/checklists/${checklist.id}/items`, { text });
        checklist.items.push(item);
        render();
        // Re-render rebuilds the whole card (and drops focus with it) - refocus
        // this checklist's item input so several items can be added in a row
        // without reaching for the mouse each time.
        container.querySelector(`[data-checklist-id="${checklist.id}"] .checklist-item-form input`)?.focus();
      });
    }

    parent.appendChild(card);
  }

  function renderCardMenu(slot, checklist, canWrite, canMove, canDuplicate) {
    const current = checklist.visibility || "shared";
    renderMenu(slot, {
      iconName: "ellipsis-vertical",
      chevron: false,
      triggerClass: "checklist-card__trigger",
      // Empty, not omitted: renderMenu echoes the selected item when given an
      // activeValue, and everything here is an action precisely so the trigger
      // can stay silent.
      label: "",
      ariaLabel: "checklists.listActions",
      items: [
        ...(canMove ? MOVES[current].map((m) => ({ value: m.value, label: t(m.labelKey), iconName: m.iconName, action: true })) : []),
        ...(canDuplicate ? [{ value: "duplicate", label: t("checklists.duplicate"), iconName: "copy", action: true }] : []),
        ...(canWrite
          ? [
              { value: "rename", label: t("checklists.rename"), iconName: "pencil", action: true },
              { value: "delete", label: t("common.delete"), iconName: "trash-2", action: true, danger: true },
            ]
          : []),
      ],
      onSelect: async (action) => {
        if (action === "rename") {
          const title = await promptDialog({
            message: t("checklists.renamePrompt"),
            value: checklist.title,
            confirmKey: "common.save",
          });
          if (title === null || !title.trim()) return;
          const updated = await api.patch(`/checklists/${checklist.id}`, { title });
          checklists = checklists.map((c) => (c.id === updated.id ? updated : c));
          render();
          return;
        }
        if (action === "duplicate") {
          // The title is built here rather than server-side: "(copy)" is
          // translated copy, and internal/httpapi emits no user-facing strings.
          const copy = await api.post(`/checklists/${checklist.id}/duplicate`, {
            title: t("checklists.duplicateTitle", { title: checklist.title }),
          });
          checklists.push(copy);
          render();
          return;
        }
        if (action === "delete") {
          if (!(await confirmDialog({ messageKey: "checklists.deleteConfirm" }))) return;
          await api.delete(`/checklists/${checklist.id}`);
          checklists = checklists.filter((c) => c.id !== checklist.id);
          render();
          return;
        }
        // Otherwise it is a move, and the value is the visibility to move to.
        const updated = await api.put(`/checklists/${checklist.id}/visibility`, { visibility: action });
        checklists = checklists.map((c) => (c.id === updated.id ? updated : c));
        render();
      },
    });
  }

  function renderItem(list, checklist, item, canWrite) {
    const li = document.createElement("li");
    li.className = "checklist-item";
    li.dataset.itemId = item.id;
    li.innerHTML = `
      <label>
        <input type="checkbox" ${item.checked ? "checked" : ""} ${canWrite ? "" : "disabled"} />
        <span class="${item.checked ? "checklist-item__text--done" : ""}">${escapeHtml(item.text)}</span>
      </label>
      <span class="checklist-item__actions"></span>
      <p class="checklist-item__error" role="alert" hidden></p>
    `;

    if (canWrite) {
      // Guarded on the box itself: a tick that is still being saved cannot be
      // un-ticked into a second, racing PATCH.
      const box = li.querySelector('input[type="checkbox"]');
      const failure = li.querySelector(".checklist-item__error");
      box.addEventListener(
        "change",
        guard(
          async (e) => {
            failure.hidden = true;
            let updated;
            try {
              updated = await api.patch(`/checklists/${checklist.id}/items/${item.id}`, { checked: e.target.checked });
            } catch (err) {
              // Put the box back rather than leaving it showing a state the
              // server does not hold, and say so. Without this the tick simply
              // stayed where the click put it -- so an item you believed was
              // packed was not, and nothing anywhere said otherwise. The
              // admin page's open-signup toggle is the same shape.
              //
              // item.checked and the strikethrough are deliberately untouched:
              // they still describe what the server holds, which is what the
              // box is being returned to.
              console.error("could not save the tick", err);
              e.target.checked = item.checked;
              failure.textContent = t("checklists.tickFailed");
              failure.hidden = false;
              return;
            }
            item.checked = updated.checked;
            li.querySelector("span").className = item.checked ? "checklist-item__text--done" : "";
          },
          { elements: box }
        )
      );

      // A ⋮ rather than the bare remove icon this row used to carry: an item can
      // be reworded now as well as removed, and two icons competing beside a
      // line of text is the pile-up the file row already solved this way. The
      // text stays inside the label, so tapping it still ticks — which is the
      // thing you do a hundred times more often than editing.
      renderMenu(li.querySelector(".checklist-item__actions"), {
        iconName: "ellipsis-vertical",
        chevron: false,
        triggerClass: "checklist-item__trigger",
        label: "",
        ariaLabel: "checklists.itemActions",
        items: [
          { value: "edit", label: t("checklists.editItem"), iconName: "pencil", action: true },
          { value: "remove", label: t("common.remove"), iconName: "x", action: true, danger: true },
        ],
        onSelect: async (action) => {
          if (action === "edit") {
            const text = await promptDialog({
              message: t("checklists.editItemPrompt"),
              value: item.text,
              confirmKey: "common.save",
            });
            if (text === null || !text.trim()) return;
            const updated = await api.put(`/checklists/${checklist.id}/items/${item.id}/text`, { text });
            item.text = updated.text;
            render();
            return;
          }
          await api.delete(`/checklists/${checklist.id}/items/${item.id}`);
          checklist.items = checklist.items.filter((i) => i.id !== item.id);
          render();
        },
      });
    }
    list.appendChild(li);
  }

  function bindNewForm() {
    if (shared) {
      const current = container.querySelector(`[name="newVisibility"][value="${newVisibility}"]`);
      if (current) current.checked = true;
      container.querySelectorAll('[name="newVisibility"]').forEach((input) => {
        input.addEventListener("change", () => {
          if (input.checked) newVisibility = input.value;
        });
      });
    }

    const newForm = container.querySelector(".checklist-new-form");
    if (newForm) {
      guardForm(newForm, async (e) => {
        const input = e.target.elements.title;
        const title = input.value.trim();
        if (!title) return;
        const checklist = await api.post(`/trips/${tripId}/checklists`, { title, visibility: newVisibility });
        checklists.push(checklist);
        render();
      });
    }
  }

  render();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
