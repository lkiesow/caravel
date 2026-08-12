import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";

const CATEGORIES = ["site", "stay", "transport"];

// Renders a create/edit form for an item's core fields into `container`.
// Pass an existing item to edit it, or null (with a tripId) to create one.
//
// With showActions: false the form renders no Save/Cancel row and the
// caller places its own submit control, driving it through the returned
// `submit()` - same arrangement as renderTripForm. That's what the create
// page does: creating a location commits several cards at once (photo,
// location, dates, links, documents), so the button belongs at the bottom
// of all of them rather than inside the first one.
export function renderItemForm(container, item, { tripId, onSaved, onCancel, showActions = true }) {
  container.innerHTML = `
    <form class="item-form" novalidate>
      <p class="item-form__error" hidden></p>
      <label>
        <span data-i18n="location.form.title"></span>
        <input type="text" name="title" required />
      </label>
      <label>
        <span data-i18n="location.form.category"></span>
        <select name="category">
          ${CATEGORIES.map((c) => `<option value="${c}">${t(`item.category.${c}`)}</option>`).join("")}
        </select>
      </label>
      <label>
        <span data-i18n="location.form.type"></span>
        <input type="text" name="type" data-i18n-placeholder="location.form.typePlaceholder" />
      </label>
      <label>
        <span data-i18n="location.form.notes"></span>
        <textarea name="notes" rows="6"></textarea>
      </label>
      <label class="item-form__checkbox">
        <input type="checkbox" name="showOnMap" checked />
        <span data-i18n="location.form.showOnMap"></span>
      </label>
      ${
        showActions
          ? `
        <div class="item-form__actions">
          <button type="submit" class="btn btn-primary">${icon("check")} <span data-i18n="${item ? "common.save" : "location.editor.createButton"}"></span></button>
          <button type="button" class="btn btn-secondary" data-action="cancel">${icon("x")} <span data-i18n="common.cancel"></span></button>
        </div>
      `
          : ""
      }
    </form>
  `;
  translatePage(container);

  const form = container.querySelector("form");
  const errorEl = container.querySelector(".item-form__error");

  if (item) {
    form.title.value = item.title;
    form.category.value = item.category;
    form.type.value = item.type ?? "";
    form.notes.value = item.notes ?? "";
    form.showOnMap.checked = item.show_on_map;
  }

  // Notes grow with their content rather than making the user scroll a
  // 6-row box or drag it bigger every time. min-height in the CSS sets the
  // floor; clearing the height first lets it shrink again on delete. Run
  // once after prefill so an existing long note opens fully expanded.
  function autoGrowNotes() {
    const notes = form.notes;
    notes.style.height = "auto";
    // Everything is border-box (see base.css), so the borders have to be
    // added back: scrollHeight covers content + padding only, and leaving
    // them out shorts the box by 2px, which is enough to keep a scrollbar.
    const borders = notes.offsetHeight - notes.clientHeight;
    notes.style.height = `${notes.scrollHeight + borders}px`;
  }
  form.notes.addEventListener("input", autoGrowNotes);
  autoGrowNotes();

  container.querySelector('[data-action="cancel"]')?.addEventListener("click", () => onCancel?.());

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    errorEl.hidden = true;

    const body = {
      category: form.category.value,
      type: form.type.value,
      title: form.title.value,
      notes: form.notes.value || null,
      show_on_map: form.showOnMap.checked,
    };

    try {
      const saved = item
        ? await api.patch(`/items/${item.id}`, body)
        : await api.post(`/trips/${tripId}/items`, body);
      onSaved?.(saved);
    } catch (err) {
      errorEl.textContent = err.body?.error || t("common.error");
      errorEl.hidden = false;
    }
  });

  // requestSubmit() (not submit()) so the handler above still runs - that's
  // where saving and error display live.
  return { submit: () => form.requestSubmit() };
}
