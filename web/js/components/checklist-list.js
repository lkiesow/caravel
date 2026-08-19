import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { confirmDialog } from "./dialog.js";

// Renders a trip's checklists: each an "editor-card" with a title, its
// checkable items, an "add item" input, and a delete-checklist action.
// Plus an "Add checklist" trigger below the list. `tripId` scopes the
// list/create calls; item-level calls are scoped by their own checklist id
// (see internal/httpapi/checklists.go's route shape).
export async function renderChecklistList(container, tripId) {
  let checklists = await api.get(`/trips/${tripId}/checklists`);

  function render() {
    container.innerHTML = `
      <div class="checklist-list"></div>
      <p class="checklists-empty" data-i18n="checklists.empty" hidden></p>
      <form class="checklist-new-form">
        <input type="text" name="title" data-i18n-placeholder="checklists.titlePlaceholder" required />
        <button type="submit" class="btn btn-primary btn-collapse">${icon("plus")} <span data-i18n="checklists.add"></span></button>
      </form>
    `;
    translatePage(container);

    const list = container.querySelector(".checklist-list");
    container.querySelector(".checklists-empty").hidden = checklists.length > 0;

    for (const checklist of checklists) {
      const card = document.createElement("div");
      card.className = "editor-card checklist-card";
      card.dataset.checklistId = checklist.id;
      card.innerHTML = `
        <div class="checklist-card__header">
          <h2>${escapeHtml(checklist.title)}</h2>
          <button class="icon-remove" data-action="delete-checklist" aria-label="${t("common.delete")}">${icon("trash-2")}</button>
        </div>
        <ul class="checklist-items"></ul>
        <form class="checklist-item-form">
          <input type="text" name="text" data-i18n-placeholder="checklists.itemPlaceholder" required />
          <button type="submit" class="btn btn-secondary btn-collapse">${icon("plus")} <span data-i18n="checklists.addItem"></span></button>
        </form>
      `;
      translatePage(card);

      const itemsList = card.querySelector(".checklist-items");
      for (const item of checklist.items) {
        const li = document.createElement("li");
        li.className = "checklist-item";
        li.innerHTML = `
          <label>
            <input type="checkbox" ${item.checked ? "checked" : ""} />
            <span class="${item.checked ? "checklist-item__text--done" : ""}">${escapeHtml(item.text)}</span>
          </label>
          <button class="icon-remove" data-action="delete-item" aria-label="${t("common.remove")}">${icon("x")}</button>
        `;
        li.querySelector('input[type="checkbox"]').addEventListener("change", async (e) => {
          const updated = await api.patch(`/checklists/${checklist.id}/items/${item.id}`, { checked: e.target.checked });
          item.checked = updated.checked;
          li.querySelector("span").className = item.checked ? "checklist-item__text--done" : "";
        });
        li.querySelector('[data-action="delete-item"]').addEventListener("click", async () => {
          await api.delete(`/checklists/${checklist.id}/items/${item.id}`);
          checklist.items = checklist.items.filter((i) => i.id !== item.id);
          render();
        });
        itemsList.appendChild(li);
      }

      card.querySelector('[data-action="delete-checklist"]').addEventListener("click", async () => {
        if (!(await confirmDialog({ messageKey: "checklists.deleteConfirm" }))) return;
        await api.delete(`/checklists/${checklist.id}`);
        checklists = checklists.filter((c) => c.id !== checklist.id);
        render();
      });

      card.querySelector(".checklist-item-form").addEventListener("submit", async (e) => {
        e.preventDefault();
        const input = e.target.elements.text;
        const text = input.value.trim();
        if (!text) return;
        const item = await api.post(`/checklists/${checklist.id}/items`, { text });
        checklist.items.push(item);
        render();
        // Re-render rebuilds the whole card (and drops focus with it) -
        // refocus this checklist's item input so several items can be
        // added in a row without reaching for the mouse each time.
        container.querySelector(`[data-checklist-id="${checklist.id}"] .checklist-item-form input`)?.focus();
      });

      list.appendChild(card);
    }

    container.querySelector(".checklist-new-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      const input = e.target.elements.title;
      const title = input.value.trim();
      if (!title) return;
      const checklist = await api.post(`/trips/${tripId}/checklists`, { title });
      checklists.push(checklist);
      render();
    });
  }

  render();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
