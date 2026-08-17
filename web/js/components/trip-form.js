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
      <p class="trip-form__error" role="alert" hidden></p>
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

  // Keep the end-date picker from offering days before the start date at
  // all - prevention beats the error message below, which only fires once
  // the user has already committed to a choice. Deliberately one-directional:
  // capping the start date at the end date too would block the ordinary move
  // of shifting a whole trip later, where you pick the new start first.
  //
  // An end date that's already set and now precedes a newly picked start is
  // left exactly as the user typed it - the submit check reports it rather
  // than silently rewriting or clearing a date they entered. The form is
  // novalidate, so min never blocks submission on its own either.
  const syncEndDateMin = () => {
    if (form.startDate.value) form.endDate.min = form.startDate.value;
    else form.endDate.removeAttribute("min");
  };
  syncEndDateMin();
  form.startDate.addEventListener("change", syncEndDateMin);

  container.querySelector('[data-action="cancel"]')?.addEventListener("click", () => onCancel?.());

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    errorEl.hidden = true;

    // The API rejects this too (tripRequest.validate), but catching it here
    // keeps the message inline and next to the fields instead of arriving as
    // a round-trip error. Still needed alongside the end-date min above: a
    // date typed directly into the field bypasses the picker's constraint.
    if (form.startDate.value && form.endDate.value && form.endDate.value < form.startDate.value) {
      errorEl.textContent = t("trip.form.endBeforeStart");
      errorEl.hidden = false;
      return;
    }

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
