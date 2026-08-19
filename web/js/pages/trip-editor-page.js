import { api } from "../api.js";
import { translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { renderTripForm } from "../components/trip-form.js";
import { renderImageField } from "../components/image-field.js";
import { icon } from "../icon.js";
import { alertDialog } from "../components/dialog.js";

// Trip creation only ("/trips/new" - editing an existing trip now happens
// inline in its Settings tab, see settings-tab.js). Two cards, Basic info
// then Cover photo, in the same order the Settings tab edits them: naming
// the thing comes before decorating it, and it's the field you'd fill even
// if you skipped everything else. "Create trip"/Cancel live on the page
// below both cards rather than inside the form (renderTripForm's
// showActions: false), since there's only one action here and it belongs at
// the bottom of everything it commits, not mid-page. The cover-photo picker
// runs in
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
          <h2 data-i18n="trip.editor.basicInfo"></h2>
          <div class="trip-form-slot"></div>
        </div>
        <div class="editor-card">
          <h2 data-i18n="trip.overview.image"></h2>
          <div class="image-field-slot"></div>
        </div>
        <div class="editor-actions">
          <button class="btn btn-primary" data-action="create">${icon("check")} <span data-i18n="trip.editor.createButton"></span></button>
          <button class="btn btn-secondary" data-action="cancel">${icon("x")} <span data-i18n="common.cancel"></span></button>
        </div>
      </div>
    `;
    translatePage(container);

    const form = renderTripForm(container.querySelector(".trip-form-slot"), null, {
      showActions: false,
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
            //
            // The message is the app's own, not the server's: this used to
            // alert() the Go error verbatim ("could not fetch image from url:
            // server returned status 403"), untranslated and in developer
            // language, while reading as though the whole create had failed.
            // The detail is still worth having, so it goes to the console.
            imageFailed = true;
            console.error("preview image upload failed:", err.body?.error || err.message || err);
            await alertDialog({ messageKey: "image.fetchFailed" });
          }
        }
        navigate(imageFailed ? `/trips/${saved.id}/settings` : `/trips/${saved.id}`);
      },
    });

    renderImageField(container.querySelector(".image-field-slot"), {
      onStaged: (staged) => {
        stagedImage = staged;
      },
    });

    container.querySelector('[data-action="create"]').addEventListener("click", () => form.submit());

    container.querySelector('[data-action="cancel"]').addEventListener("click", () => {
      if (stagedImage?.kind === "file" && stagedImage.previewUrl) URL.revokeObjectURL(stagedImage.previewUrl);
      navigate("/trips");
    });
  }

  render();
}
