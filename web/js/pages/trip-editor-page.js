import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { renderTripForm } from "../components/trip-form.js";
import { renderImageField } from "../components/image-field.js";
import { icon } from "../icon.js";

// Trip creation only ("/trips/new" - editing an existing trip now happens
// inline in its Settings tab, see settings-tab.js). One merged card (cover
// photo above the fields, so Save sits at the very bottom of the whole
// form, not mid-page above unrelated content) - there's only one action
// here, so it should read as one form. The cover-photo picker runs in
// image-field.js's staging mode (no trip exists yet): a pick is held in
// memory and previewed locally, then uploaded and attached as part of the
// same save that creates the trip. On success, this navigates straight to
// the trip's view page - unambiguous feedback that the save happened,
// matching how opening any other trip works. If the staged image itself
// fails to upload/attach (the trip was still created), it lands on the
// trip's Settings tab instead, since that's the one place a broken image
// can be retried.
export async function renderTripEditorPage(container) {
  let stagedImage = null;

  function render() {
    container.innerHTML = `
      <div class="page trip-editor">
        <a href="/trips" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.home"></span></a>
        <div class="page__header">
          <h1 data-i18n="trip.editor.newTitle"></h1>
        </div>
        <div class="editor-card">
          <div class="image-field-slot"></div>
          <div class="trip-form-slot"></div>
        </div>
      </div>
    `;
    translatePage(container);

    renderTripForm(container.querySelector(".trip-form-slot"), null, {
      onSaved: async (saved) => {
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
            // failed image upload. Land on the trip's Settings tab
            // instead of the view page, since that's the one place this
            // can be retried.
            imageFailed = true;
            window.alert(err.body?.error || err.message || t("common.error"));
          }
        }
        navigate(imageFailed ? `/trips/${saved.id}/settings` : `/trips/${saved.id}`);
      },
      onCancel: () => {
        if (stagedImage?.kind === "file" && stagedImage.previewUrl) URL.revokeObjectURL(stagedImage.previewUrl);
        navigate("/trips");
      },
    });

    renderImageField(container.querySelector(".image-field-slot"), {
      onStaged: (staged) => {
        stagedImage = staged;
      },
    });
  }

  render();
}
