import { api } from "../api.js";
import { guard, guardClick, guardForm } from "../busy.js";
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
  const staged = { kind: null, file: null, url: null, previewUrl: null, provenance: null };

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

    // The input is what goes busy, so a second pick cannot start a second
    // upload of the same field - and the label reads as unavailable while the
    // first one is going.
    const fileInput = container.querySelector('input[type="file"]');
    fileInput.addEventListener(
      "change",
      guard(
        async (e) => {
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
        },
        { elements: fileInput }
      )
    );

    guardForm(container.querySelector(".image-field__url-form"), async (e) => {
      const input = e.target.url;
      if (!input.value) return;
      errorEl.hidden = true;

      await setFromURL(input.value);
    });

    const removeBtn = container.querySelector('[data-action="remove"]');
    if (removeBtn) {
      guardClick(removeBtn, async () => {
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
  }

  async function attach(mediaAssetId, url) {
    const updated = await api.put(attachPath, { media_asset_id: mediaAssetId });
    render(url);
    onChanged?.(updated);
  }

  // Set the image from a URL, optionally carrying where it came from.
  //
  // Shared by the URL form and by the assistant's cover suggestion, which is
  // the reason it takes provenance at all: an image the assistant found is
  // often a freely licensed photograph, and a freely licensed photograph is
  // not an unencumbered one. The credit has to travel with the image at the
  // moment it is stored, because it cannot be recovered afterwards.
  //
  // Exposed on the returned handle so the panel writes through this component
  // rather than reaching into its DOM -- the same shape renderItemForm's
  // setValues took in Stage 16.
  async function setFromURL(url, provenance = null) {
    if (!url) return;
    // Re-queried rather than closed over: render() rebuilds the markup, so an
    // element captured earlier is detached and setting it does nothing.
    const say = (msg) => {
      const el = container.querySelector(".image-field__error");
      if (!el) return;
      el.textContent = msg ?? "";
      el.hidden = msg == null;
    };
    say(null);

    if (isStaging) {
      if (staged.kind === "file" && staged.previewUrl) URL.revokeObjectURL(staged.previewUrl);
      staged.kind = "url";
      staged.file = null;
      staged.url = url;
      staged.previewUrl = url;
      staged.provenance = provenance;
      onStaged?.({ kind: "url", url, previewUrl: staged.previewUrl, provenance });
      render(staged.previewUrl);
      return;
    }

    try {
      const asset = await api.post(`/trips/${tripId}/media/url`, { url, ...(provenance ?? {}) });
      await attach(asset.id, asset.url);
    } catch (err) {
      console.error("image url fetch failed:", err.body?.error || err.message || err);
      say(t("image.fetchFailed"));
    }
  }

  render(imageUrl);

  return { setFromURL };
}

function escapeAttr(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
