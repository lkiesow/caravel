import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { renderTripForm } from "../components/trip-form.js";
import { renderImageField } from "../components/image-field.js";
import { icon } from "../icon.js";

// Two distinct layouts based on whether :tripId is present:
//
// - Create mode: one merged card (cover photo above the fields, so Save
//   sits at the very bottom of the whole form, not mid-page above unrelated
//   content) - there's only one action here, so it should read as one form.
//   The cover-photo picker runs in image-field.js's staging mode (no trip
//   exists yet): a pick is held in memory and previewed locally, then
//   uploaded and attached as part of the same save that creates the trip.
//   On success, this navigates straight to the trip's view page (not an
//   edit page) - unambiguous feedback that the save happened, matching how
//   opening any other trip works. If the staged image itself fails to
//   upload/attach (the trip was still created), it lands on the edit page
//   instead, since that's the one place a broken image can be retried.
// - Edit mode: three independent cards (Basic Info / Cover Photo / Delete),
//   each its own action - unchanged from Milestone 4.
export async function renderTripEditorPage(container, { tripId }) {
  let trip = null;
  let stagedImage = null;
  if (tripId) {
    try {
      trip = await api.get(`/trips/${tripId}`);
    } catch {
      container.innerHTML = `<p>${t("common.notFound")}</p><a href="/trips" data-link>${t("common.back")}</a>`;
      return;
    }
  }

  function render() {
    container.innerHTML = `
      <div class="page trip-editor">
        <a href="${trip ? `/trips/${trip.id}` : "/trips"}" data-link class="back-link">${icon("arrow-left")} <span data-i18n="${trip ? "common.back" : "common.home"}"></span></a>
        <div class="page__header">
          <h1 data-i18n="${trip ? "trip.editor.editTitle" : "trip.editor.newTitle"}"></h1>
        </div>
        ${
          trip
            ? `
          <div class="editor-card">
            <h4 data-i18n="trip.editor.basicInfo"></h4>
            <div class="trip-form-slot"></div>
          </div>
          <div class="editor-card">
            <h4 data-i18n="trip.overview.image"></h4>
            <div class="image-field-slot"></div>
          </div>
          <div class="editor-card trip-editor__actions">
            <button class="btn btn-danger" data-action="delete">${icon("trash-2")} <span data-i18n="common.delete"></span></button>
          </div>
        `
            : `
          <div class="editor-card">
            <div class="image-field-slot"></div>
            <div class="trip-form-slot"></div>
          </div>
        `
        }
      </div>
    `;
    translatePage(container);

    renderTripForm(container.querySelector(".trip-form-slot"), trip, {
      onSaved: async (saved) => {
        if (trip) {
          navigate(`/trips/${saved.id}`);
          return;
        }

        let imageFailed = false;
        if (stagedImage) {
          try {
            let asset;
            if (stagedImage.kind === "file") {
              const formData = new FormData();
              formData.append("file", stagedImage.file);
              const res = await fetch(`/api/trips/${saved.id}/media`, { method: "POST", body: formData, credentials: "same-origin" });
              asset = await res.json();
              if (!res.ok) throw new Error(asset.error || "upload failed");
            } else {
              asset = await api.post(`/trips/${saved.id}/media/url`, { url: stagedImage.url });
            }
            await api.put(`/trips/${saved.id}/preview-image`, { media_asset_id: asset.id });
          } catch (err) {
            // The trip was already created - don't block navigation on a
            // failed image upload. Land on the edit page instead of the
            // view page, since that's the only place this can be retried.
            imageFailed = true;
            window.alert(err.body?.error || err.message || t("common.error"));
          }
        }
        navigate(imageFailed ? `/trips/${saved.id}/edit` : `/trips/${saved.id}`);
      },
      onCancel: () => {
        if (stagedImage?.kind === "file" && stagedImage.previewUrl) URL.revokeObjectURL(stagedImage.previewUrl);
        navigate(trip ? `/trips/${trip.id}` : "/trips");
      },
    });

    renderImageField(container.querySelector(".image-field-slot"), {
      tripId: trip?.id,
      imageUrl: trip?.preview_image_url,
      attachPath: trip ? `/trips/${trip.id}/preview-image` : undefined,
      onChanged: (updated) => {
        trip = updated;
      },
      onStaged: (staged) => {
        stagedImage = staged;
      },
    });

    if (!trip) return;

    container.querySelector('[data-action="delete"]').addEventListener("click", async () => {
      if (!window.confirm(t("trip.deleteConfirm"))) return;
      await api.delete(`/trips/${trip.id}`);
      navigate("/trips");
    });
  }

  render();
}
