import { api } from "../api.js";
import { guardForm } from "../busy.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { CURRENCIES } from "../format.js";

// Renders a create/edit form for a trip into `container`. Pass an existing
// trip object to edit it in place, or null to create a new one.
//
// With showActions: false the form renders no Save/Cancel row and the
// caller is expected to place its own submit control, driving it through
// the returned `submit()`. That's what the create page does: its fields
// come first and the cover-photo card after them, so a row of buttons
// inside the fields card would sit mid-page above content that's still
// part of the same single "create this trip" action.
export function renderTripForm(container, trip, { onSaved, onCancel, showActions = true, createRequest }) {
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
      <label>
        <span data-i18n="trip.form.currency"></span>
        <select name="currency">
          ${CURRENCIES.map((code) => `<option value="${code}">${code}</option>`).join("")}
        </select>
      </label>
      <p class="trip-form__hint" data-i18n="trip.form.currencyHint"></p>
      <p class="trip-form__warning" role="status" hidden></p>
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
    // Falls back to the first option rather than to a literal: the server's
    // default and this list's first entry are the same code, and naming it
    // twice is how they drift.
    form.currency.value = trip.currency || CURRENCIES[0];
  }

  // A rate in trip_currencies converts its code into *the trip's main
  // currency*, but nothing records which main currency it was entered
  // against -- so switching the main currency leaves every rate reading as a
  // conversion into the new one. The number stays plausible and becomes wrong,
  // which is the kind of error nothing else in the app would ever surface.
  //
  // A warning at the moment of the change rather than a refusal: the switch is
  // legitimate (a trip really can be re-denominated), and the person making it
  // is the only one who knows whether the rates still mean anything. It names
  // the codes, because "check your rates" is advice and "check JPY and USD" is
  // an instruction.
  //
  // Shown only while the select differs from what was saved, so returning it to
  // the original value takes the warning away again.
  const currencyWarning = container.querySelector(".trip-form__warning");
  const savedCurrency = trip?.currency ?? null;
  const syncCurrencyWarning = () => {
    const codes = (trip?.currencies ?? []).map((c) => c.code);
    if (!codes.length || form.currency.value === savedCurrency) {
      currencyWarning.hidden = true;
      return;
    }
    currencyWarning.textContent = t("trip.form.currencySwitchWarning", {
      codes: codes.join(", "),
      previous: savedCurrency,
    });
    currencyWarning.hidden = false;
  };
  syncCurrencyWarning();
  form.currency.addEventListener("change", syncCurrencyWarning);

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

  const guard = guardForm(form, async () => {
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
      currency: form.currency.value,
    };

    let saved;
    try {
      // createRequest lets the create page send the cover photo in the same
      // request (see trip-editor-page.js). It is inside the try because with
      // one atomic request a failed cover *is* a failed create: nothing was
      // written, so the error belongs on this form and the page stays put.
      if (trip) {
        saved = await api.patch(`/trips/${trip.id}`, body);
      } else if (createRequest) {
        saved = await createRequest(body);
      } else {
        saved = await api.post("/trips", body);
      }
    } catch (err) {
      errorEl.textContent = err.body?.error || t("common.error");
      errorEl.hidden = false;
      return;
    }

    // Awaited, and outside the try - onSaved navigates, so releasing the guard
    // as soon as the create answered would re-enable Create halfway through
    // and let a second press make a second trip.
    await onSaved?.(saved);
  });

  // requestSubmit() (not submit()) so the form's own submit handler above
  // still runs, which is where saving and error display live.
  //
  // The guard goes out with it: a caller placing its own submit control
  // (showActions: false) needs that button in the *same* busy set, not a
  // second flag of its own - see trip-editor-page.js.
  return { submit: () => form.requestSubmit(), guard };
}
