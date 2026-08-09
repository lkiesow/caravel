import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { renderTripForm } from "../components/trip-form.js";
import { renderImageField } from "../components/image-field.js";

// One page, two modes based on whether :tripId is present. In create mode
// there's no image field yet since a trip needs to exist before an image
// can attach to it (media uploads are always scoped to a trip) - on first
// save, the URL is swapped in place to the edit route, which re-renders
// with the image field and Delete action now available.
export async function renderTripEditorPage(container, { tripId }) {
  let trip = null;
  if (tripId) {
    try {
      trip = await api.get(`/trips/${tripId}`);
    } catch {
      container.innerHTML = `<p>${t("trips.empty")}</p><a href="/trips" data-link>${t("common.back")}</a>`;
      return;
    }
  }

  function render() {
    container.innerHTML = `
      <div class="page trip-editor">
        <a href="${trip ? `/trips/${trip.id}` : "/trips"}" data-link class="back-link" data-i18n="common.back"></a>
        <div class="page__header">
          <h1 data-i18n="${trip ? "trip.editor.editTitle" : "trip.editor.newTitle"}"></h1>
        </div>
        <div class="trip-form-slot"></div>
        ${
          trip
            ? `
          <h4 data-i18n="trip.overview.image"></h4>
          <div class="image-field-slot"></div>
          <div class="trip-editor__actions">
            <button data-action="delete" data-i18n="common.delete"></button>
          </div>
        `
            : ""
        }
      </div>
    `;
    translatePage(container);

    renderTripForm(container.querySelector(".trip-form-slot"), trip, {
      onSaved: (saved) => {
        if (trip) {
          navigate(`/trips/${saved.id}`);
        } else {
          trip = saved;
          window.history.replaceState({}, "", `/trips/${saved.id}/edit`);
          render();
        }
      },
      onCancel: () => navigate(trip ? `/trips/${trip.id}` : "/trips"),
    });

    if (!trip) return;

    renderImageField(container.querySelector(".image-field-slot"), {
      tripId: trip.id,
      imageUrl: trip.preview_image_url,
      attachPath: `/trips/${trip.id}/preview-image`,
      onChanged: (updated) => {
        trip = updated;
      },
    });

    container.querySelector('[data-action="delete"]').addEventListener("click", async () => {
      if (!window.confirm(t("trip.deleteConfirm"))) return;
      await api.delete(`/trips/${trip.id}`);
      navigate("/trips");
    });
  }

  render();
}
