import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";

const CATEGORIES = ["site", "stay", "transport"];

// Renders a create/edit form for an item's core fields into `container`.
// Pass an existing item to edit it, or null (with a tripId) to create one.
export function renderItemForm(container, item, { tripId, onSaved, onCancel }) {
  container.innerHTML = `
    <form class="item-form" novalidate>
      <p class="item-form__error" hidden></p>
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
        <span data-i18n="location.form.title"></span>
        <input type="text" name="title" required />
      </label>
      <label>
        <span data-i18n="location.form.notes"></span>
        <textarea name="notes" rows="3"></textarea>
      </label>
      <label class="item-form__checkbox">
        <input type="checkbox" name="showOnMap" checked />
        <span data-i18n="location.form.showOnMap"></span>
      </label>
      <div class="item-form__actions">
        <button type="submit" class="btn btn-primary" data-i18n="${item ? "common.save" : "location.editor.createButton"}"></button>
        <button type="button" class="btn btn-secondary" data-action="cancel" data-i18n="common.cancel"></button>
      </div>
    </form>
  `;
  translatePage(container);

  const form = container.querySelector("form");
  const errorEl = container.querySelector(".item-form__error");

  if (item) {
    form.category.value = item.category;
    form.type.value = item.type ?? "";
    form.title.value = item.title;
    form.notes.value = item.notes ?? "";
    form.showOnMap.checked = item.show_on_map;
  }

  container.querySelector('[data-action="cancel"]').addEventListener("click", () => onCancel?.());

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
}
