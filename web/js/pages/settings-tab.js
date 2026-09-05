import { api } from "../api.js";
import { guardClick } from "../busy.js";
import { translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { renderTripForm } from "../components/trip-form.js";
import { renderImageField } from "../components/image-field.js";
import { renderTripCurrenciesField } from "../components/trip-currencies-field.js";
import { confirmDialog } from "../components/dialog.js";

// Renders the trip's own settings inline in the Settings tab - the same
// three cards (Basic Info / Cover Photo / Delete) trip-editor-page.js's
// edit mode used to show on a separate /trips/:tripId/edit page, now
// living alongside the other tabs instead. Unlike that page, there's no
// "page" to navigate away from: onSaved reports the updated trip back to
// the caller (which merges it into the live trip object and re-renders
// the whole tab bar/header in place) rather than navigating; onCancel
// just redraws these same cards from the trip's last-saved values,
// discarding whatever was typed but not saved.
// canEdit and canDelete are separate because the roles are: an editor may
// rename a trip and change its cover photo, but only the owner may delete it.
// Passed in rather than derived here so the tab has one source of truth for the
// role — trip-detail-page.js, which already consults trip-role.js for the other
// tabs.
export function renderSettingsTab(content, trip, { onTripUpdated, canEdit = true, canDelete = true }) {
  function render() {
    content.innerHTML = `
      ${
        canEdit
          ? `<div class="editor-card">
        <h2 data-i18n="trip.editor.basicInfo"></h2>
        <div class="trip-form-slot"></div>
      </div>
      <div class="editor-card">
        <h2 data-i18n="trip.settings.image"></h2>
        <div class="image-field-slot"></div>
      </div>
      <div class="editor-card">
        <h2 data-i18n="trip.currencies.heading"></h2>
        <div class="trip-currencies-slot"></div>
      </div>`
          : `<div class="editor-card">
        <h2 data-i18n="trip.tabs.settings"></h2>
        <p class="editor-card__hint" data-i18n="trip.settings.readOnly"></p>
      </div>`
      }
      ${
        canDelete
          ? `<div class="editor-card">
        <h2 data-i18n="trip.deleteHeading"></h2>
        <p class="editor-card__hint" data-i18n="trip.deleteDescription"></p>
        <button class="btn btn-danger" data-action="delete">${icon("trash-2")} <span data-i18n="common.delete"></span></button>
      </div>`
          : ""
      }
    `;
    translatePage(content);

    if (canEdit) {
      renderTripForm(content.querySelector(".trip-form-slot"), trip, {
        onSaved: (saved) => onTripUpdated(saved),
        onCancel: () => render(),
      });

      renderImageField(content.querySelector(".image-field-slot"), {
        tripId: trip.id,
        imageUrl: trip.preview_image_url,
        attachPath: `/trips/${trip.id}/preview-image`,
        searchSeed: () => trip.title ?? "",
        onChanged: (updated) => onTripUpdated(updated),
      });

      // Saves in place rather than through onTripUpdated: the currencies are
      // not part of the trip PATCH, and re-rendering the whole tab bar and
      // header for a rate change would throw away the rows being edited. The
      // component writes the new set back onto the trip object, which is what
      // the expenses tab reads when it next renders.
      renderTripCurrenciesField(content.querySelector(".trip-currencies-slot"), trip);
    }

    // The confirm is inside the guard: a second click while the dialog is open
    // would otherwise stack a second copy of it.
    const deleteBtn = content.querySelector('[data-action="delete"]');
    if (deleteBtn) {
      guardClick(deleteBtn, async () => {
        if (!(await confirmDialog({ messageKey: "trip.deleteConfirm" }))) return;
        await api.delete(`/trips/${trip.id}`);
        navigate("/trips");
      });
    }
  }

  render();
}
