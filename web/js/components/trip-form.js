import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// Renders a create/edit form for a trip into `container`. Pass an existing
// trip object to edit it in place, or null to create a new one.
//
// With showActions: false the form renders no Save/Cancel row and the
// caller is expected to place its own submit control, driving it through
// the returned `submit()`. That's what the create page does: its fields
// come first and the cover-photo card after them, so a row of buttons
// inside the fields card would sit mid-page above content that's still
// part of the same single "create this trip" action.
export function renderTripForm(container, trip, { onSaved, onCancel, showActions = true }) {
  container.innerHTML = `
    <form class="trip-form" novalidate>
      <p class="trip-form__error" hidden></p>
      <label>
        <span data-i18n="trip.form.title"></span>
        <input type="text" name="title" required />
      </label>
      <div class="trip-form__dates">
        <label>
          <span data-i18n="trip.form.startDate"></span>
          <input type="date" name="startDate" />
        </label>
        <label>
          <span data-i18n="trip.form.endDate"></span>
          <input type="date" name="endDate" />
        </label>
      </div>
      <label>
        <span data-i18n="trip.form.subtitle"></span>
        <input type="text" name="subtitle" />
      </label>
      ${
        showActions
          ? `
        <div class="trip-form__actions">
          <button type="submit" class="btn btn-primary">${icon("check")} <span data-i18n="${trip ? "common.save" : "trip.editor.createButton"}"></span></button>
          <button type="button" class="btn btn-secondary" data-action="cancel">${icon("x")} <span data-i18n="common.cancel"></span></button>
        </div>
      `
          : ""
      }
    </form>
  `;
  translatePage(container);

  const form = container.querySelector("form");
  const errorEl = container.querySelector(".trip-form__error");

  if (trip) {
    form.title.value = trip.title;
    form.startDate.value = trip.start_date ?? "";
    form.endDate.value = trip.end_date ?? "";
    form.subtitle.value = trip.subtitle ?? "";
  }

  container.querySelector('[data-action="cancel"]')?.addEventListener("click", () => onCancel?.());

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    errorEl.hidden = true;

    const body = {
      title: form.title.value,
      start_date: form.startDate.value || null,
      end_date: form.endDate.value || null,
      subtitle: form.subtitle.value || null,
    };

    try {
      const saved = trip ? await api.patch(`/trips/${trip.id}`, body) : await api.post("/trips", body);
      onSaved?.(saved);
    } catch (err) {
      errorEl.textContent = err.body?.error || t("common.error");
      errorEl.hidden = false;
    }
  });

  // requestSubmit() (not submit()) so the form's own submit handler above
  // still runs, which is where saving and error display live.
  return { submit: () => form.requestSubmit() };
}
