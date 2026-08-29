import { api } from "../api.js";
import { translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { renderTripForm } from "../components/trip-form.js";
import { renderImageField } from "../components/image-field.js";
import { icon } from "../icon.js";

// Trip creation only ("/trips/new" - editing an existing trip now happens
// inline in its Settings tab, see settings-tab.js). Two cards, Basic info
// then Cover photo, in the same order the Settings tab edits them: naming
// the thing comes before decorating it, and it's the field you'd fill even
// if you skipped everything else. "Create trip"/Cancel live on the page
// below both cards rather than inside the form (renderTripForm's
// showActions: false), since there's only one action here and it belongs at
// the bottom of everything it commits, not mid-page. The cover-photo picker
// runs in image-field.js's staging mode (no trip exists yet): a pick is held
// in memory and previewed locally, then sent as part of the create.
//
// Since Stage 24 that is *one* request: buildCreateForm packs the trip and
// the staged cover into a multipart body and POST /api/trips validates,
// fetches and writes it all together (see internal/httpapi/trips_create.go),
// the same shape the location editor has used since Stage 23. Before that it
// was three - create, upload, attach - so a cover the server could not fetch
// left a trip already created with no picture, an alert after the fact, and a
// landing on the Settings tab to retry. Now a failure creates nothing, the
// error shows inline on the form, and the page stays where it is with the
// staged pick intact. On success this navigates straight to the trip's view
// page, unambiguous feedback that the save happened.
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
          <h2 data-i18n="trip.settings.image"></h2>
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
      createRequest: (body) => api.postForm("/trips", buildCreateForm(body)),
      onSaved: (saved) => navigate(`/trips/${saved.id}`),
    });

    renderImageField(container.querySelector(".image-field-slot"), {
      onStaged: (staged) => {
        stagedImage = staged;
      },
    });

    // The page's own Create button joins the form's busy set rather than
    // owning a second flag, so the one press it takes to save is the one press
    // it accepts - including while the staged cover photo is still uploading
    // inside onSaved.
    const createBtn = container.querySelector('[data-action="create"]');
    form.guard.watch(createBtn);
    createBtn.addEventListener("click", () => form.submit());

    container.querySelector('[data-action="cancel"]').addEventListener("click", () => {
      if (stagedImage?.kind === "file" && stagedImage.previewUrl) URL.revokeObjectURL(stagedImage.previewUrl);
      navigate("/trips");
    });
  }

  // The trip JSON and the staged cover in one multipart body, matching
  // buildCreateForm in location-editor-page.js.
  function buildCreateForm(body) {
    const form = new FormData();
    form.append("trip", JSON.stringify(body));

    if (stagedImage?.kind === "file") {
      form.append("image", stagedImage.file);
    } else if (stagedImage?.kind === "url") {
      form.append("image_url", stagedImage.url);
      // The provenance rides along. The old three-request flow posted only
      // {url}, so a cover picked with attribution lost it here - and unlike
      // the image itself, that cannot be recovered afterwards.
      const provenance = stagedImage.provenance ?? {};
      if (provenance.source_url) form.append("source_url", provenance.source_url);
      if (provenance.credit) form.append("credit", provenance.credit);
      if (provenance.license) form.append("license", provenance.license);
    }
    return form;
  }

  render();
}
