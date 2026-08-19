import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// Renders an image picker (upload a file, or paste a URL) plus a preview
// and remove button. `tripId` scopes the upload/url endpoints (media is
// always created under a trip — see plan Section 3.4). `attachPath` is the
// resource-specific endpoint that attaches/clears the resulting media asset
// (e.g. `/trips/{id}/preview-image` or `/items/{id}/image`).
//
// If `tripId`/`attachPath` aren't set yet (the entity doesn't exist yet,
// e.g. a trip being created), this runs in staging mode instead: picks are
// held in memory and previewed locally rather than uploaded immediately.
// `onStaged({ kind: "file", file, previewUrl } | { kind: "url", url, previewUrl } | null)`
// reports the current pick so the caller can upload it once the entity
// exists.
export function renderImageField(container, { tripId, imageUrl, attachPath, onChanged, onStaged }) {
  const isStaging = !tripId || !attachPath;
  const staged = { kind: null, file: null, url: null, previewUrl: null };

  function render(currentUrl) {
    container.innerHTML = `
      <div class="image-field">
        ${currentUrl ? `<img class="image-field__preview" src="${escapeAttr(currentUrl)}" alt="" />` : ""}
        <div class="image-field__controls">
          <label class="image-field__upload">
            <span data-i18n="image.upload"></span>
            <input type="file" accept="image/*" hidden />
          </label>
          <form class="image-field__url-form">
            <input type="url" name="url" data-i18n-placeholder="image.urlPlaceholder" />
            <button type="submit" class="btn btn-secondary btn-row">${icon("check")} <span data-i18n="image.setUrl"></span></button>
          </form>
          ${currentUrl ? `<button type="button" class="btn btn-secondary" data-action="remove">${icon("x")} <span data-i18n="image.remove"></span></button>` : ""}
        </div>
        <p class="image-field__error" role="alert" hidden></p>
      </div>
    `;
    translatePage(container);

    const errorEl = container.querySelector(".image-field__error");
    const showError = (msg) => {
      errorEl.textContent = msg;
      errorEl.hidden = false;
    };

    // A picture the browser can't fetch (dead link, hotlink-blocked host, a
    // file that isn't really an image) renders as an alt="" element, which
    // collapses to nothing - so the field looked exactly like "no preview
    // was set" while the URL had in fact been accepted. Say so instead, and
    // drop the empty box.
    const previewEl = container.querySelector(".image-field__preview");
    previewEl?.addEventListener("error", () => {
      previewEl.hidden = true;
      showError(t("image.loadFailed"));
    });

    container.querySelector('input[type="file"]').addEventListener("change", async (e) => {
      const file = e.target.files[0];
      if (!file) return;
      errorEl.hidden = true;

      if (isStaging) {
        if (staged.kind === "file" && staged.previewUrl) URL.revokeObjectURL(staged.previewUrl);
        staged.kind = "file";
        staged.file = file;
        staged.url = null;
        staged.previewUrl = URL.createObjectURL(file);
        onStaged?.({ kind: "file", file, previewUrl: staged.previewUrl });
        render(staged.previewUrl);
        return;
      }

      try {
        const formData = new FormData();
        formData.append("file", file);
        const res = await fetch(`/api/trips/${tripId}/media`, { method: "POST", body: formData, credentials: "same-origin" });
        const asset = await res.json();
        if (!res.ok) throw new Error(asset.error || "upload failed");
        await attach(asset.id, asset.url);
      } catch (err) {
        // Translated copy, developer detail to the console - the server's
        // message here is Go error text ("server returned status 403", or a
        // whole dial tcp: lookup ... failure) and was previously rendered
        // verbatim into the card, untranslated even in the German UI.
        console.error("image upload failed:", err.message || err);
        showError(t("image.uploadFailed"));
      }
    });

    container.querySelector(".image-field__url-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      const input = e.target.url;
      if (!input.value) return;
      errorEl.hidden = true;

      if (isStaging) {
        if (staged.kind === "file" && staged.previewUrl) URL.revokeObjectURL(staged.previewUrl);
        staged.kind = "url";
        staged.file = null;
        staged.url = input.value;
        staged.previewUrl = input.value;
        onStaged?.({ kind: "url", url: input.value, previewUrl: staged.previewUrl });
        render(staged.previewUrl);
        return;
      }

      try {
        const asset = await api.post(`/trips/${tripId}/media/url`, { url: input.value });
        await attach(asset.id, asset.url);
      } catch (err) {
        console.error("image url fetch failed:", err.body?.error || err.message || err);
        showError(t("image.fetchFailed"));
      }
    });

    const removeBtn = container.querySelector('[data-action="remove"]');
    removeBtn?.addEventListener("click", async () => {
      errorEl.hidden = true;

      if (isStaging) {
        if (staged.kind === "file" && staged.previewUrl) URL.revokeObjectURL(staged.previewUrl);
        staged.kind = null;
        staged.file = null;
        staged.url = null;
        staged.previewUrl = null;
        onStaged?.(null);
        render(null);
        return;
      }

      try {
        await attach(null, null);
      } catch (err) {
        showError(err.body?.error || t("common.error"));
      }
    });
  }

  async function attach(mediaAssetId, url) {
    const updated = await api.put(attachPath, { media_asset_id: mediaAssetId });
    render(url);
    onChanged?.(updated);
  }

  render(imageUrl);
}

function escapeAttr(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
