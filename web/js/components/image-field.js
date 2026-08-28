import { api } from "../api.js";
import { guard, guardClick, guardForm } from "../busy.js";
import { getLocale, t, translatePage } from "../i18n.js";
import { formatBytes } from "../format.js";
import { icon } from "../icon.js";
import { hasCapability } from "../session.js";

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
// The server's own limit (maxImageUploadBytes in internal/httpapi/media.go).
// Duplicated deliberately, for the same reason as the file list's copy: a
// number that has to match on both sides reads better as a named constant
// pointing at the other one than as an extra round trip to ask for it.
const MAX_IMAGE_BYTES = 50 * 1024 * 1024;

export function renderImageField(container, { tripId, imageUrl, attachPath, onChanged, onStaged, searchSeed }) {
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
          ${canSearchImages() ? `<button type="button" class="btn btn-secondary btn-row" data-action="search-image">${icon("search")} <span data-i18n="image.search"></span></button>` : ""}
          ${currentUrl ? `<button type="button" class="btn btn-secondary" data-action="remove">${icon("x")} <span data-i18n="image.remove"></span></button>` : ""}
        </div>
        <div class="image-search" hidden></div>
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

          // Checked here as well as by the server, which answers an oversized
          // upload with a 413 whose body is about multipart parsing rather
          // than about this file - see maxImageUploadBytes in
          // internal/httpapi/media.go. Before the staging branch below, so a
          // trip being created rejects the pick now rather than after the
          // trip has already been saved.
          if (file.size > MAX_IMAGE_BYTES) {
            showError(t("files.tooLarge", { name: file.name, limit: formatBytes(MAX_IMAGE_BYTES) }));
            e.target.value = "";
            return;
          }

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

    bindImageSearch();

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


  // "Search for an image": the picker, and the one image feature with no
  // assistant in it.
  //
  // Hidden unless the server reports the capability *and* there is a trip to
  // scope the search to - which is why a brand-new trip does not get it: the
  // endpoint is trip-scoped and authorized as an edit of that trip, and there
  // is nothing yet to authorize against. Every other caller has one.
  //
  // It never searches per keystroke. One press can be three calls to Wikimedia
  // and one to a metered search API, none of them ours - the same reasoning
  // that put the address search behind a button (see bindPlaceSearch).
  function canSearchImages() {
    return Boolean(tripId) && hasCapability("image_search");
  }

  function bindImageSearch() {
    const openBtn = container.querySelector('[data-action="search-image"]');
    if (!openBtn) return;
    const panel = container.querySelector(".image-search");

    openBtn.addEventListener("click", () => {
      if (!panel.hidden) {
        panel.hidden = true;
        panel.innerHTML = "";
        return;
      }
      panel.hidden = false;
      panel.innerHTML = `
        <form class="image-search__form">
          <input type="search" name="q" data-i18n-placeholder="image.searchPlaceholder" />
          <button type="submit" class="btn btn-secondary btn-row">${icon("search")} <span data-i18n="image.searchSubmit"></span></button>
        </form>
        <p class="image-search__status" role="status" hidden></p>
        <div class="image-search__groups"></div>
      `;
      translatePage(panel);

      const input = panel.querySelector('input[name="q"]');
      // Seeded from the title the user already typed, which is usually the
      // whole search - and searched straight away, because a control that
      // opens with the answer already filled in and makes you press again is
      // asking for a press it does not need.
      const seed = (searchSeed?.() ?? "").trim();
      input.value = seed;
      guardForm(panel.querySelector(".image-search__form"), async () => {
        await runImageSearch(panel, input.value.trim());
      });
      input.focus();
      if (seed.length >= 2) runImageSearch(panel, seed);
    });
  }

  async function runImageSearch(panel, query) {
    const status = panel.querySelector(".image-search__status");
    const groups = panel.querySelector(".image-search__groups");
    const say = (key) => {
      status.textContent = key ? t(key) : "";
      status.hidden = !key;
    };
    groups.innerHTML = "";
    if (query.length < 2) {
      say("image.searchTooShort");
      return;
    }
    say("image.searching");

    let found;
    try {
      found = await api.get(`/trips/${tripId}/image-search?q=${encodeURIComponent(query)}&lang=${encodeURIComponent(getLocale())}`);
    } catch (err) {
      // The Go text is for the console; the user gets one sentence. The
      // upstream services' own words are not ours to forward.
      console.error("image search failed:", err.body?.error || err.message || err);
      say("image.searchFailed");
      return;
    }
    if (!found.groups?.length) {
      say("image.searchNoResults");
      return;
    }
    say(null);

    for (const group of found.groups) {
      const section = document.createElement("section");
      section.className = "image-search__group";

      const heading = document.createElement("h4");
      // Wikipedia is named; a web search is named by its provider, because
      // "some search engine found this" is not something to be vague about.
      heading.textContent =
        group.source === "wikipedia" ? t("image.searchFromWikipedia") : t("image.searchFromWeb", { provider: group.source });
      section.appendChild(heading);

      if (group.source !== "wikipedia") {
        const note = document.createElement("p");
        note.className = "image-search__note";
        note.textContent = t("image.searchNoLicence");
        section.appendChild(note);
      }

      const grid = document.createElement("div");
      grid.className = "image-search__grid";
      for (const result of group.results) {
        grid.appendChild(renderCandidate(result, panel));
      }
      section.appendChild(grid);
      groups.appendChild(section);
    }
  }

  function renderCandidate(result, panel) {
    const cell = document.createElement("button");
    cell.type = "button";
    cell.className = "image-search__result";

    const img = document.createElement("img");
    // A thumbnail is a preview only: what gets stored is the full-size URL,
    // fetched server-side by POST /media/url exactly as a pasted link is.
    img.src = result.thumb_url || result.url;
    // Empty, and the caption below carries the name: an image in a grid of
    // candidates is described by the label right under it, and reading the
    // same words twice is worse than reading them once.
    img.alt = "";
    // A dead thumbnail must not leave an invisible cell that still clicks -
    // hotlink-blocked hosts and moved files are common, and image-field's own
    // preview already learned this lesson. The whole cell goes.
    img.addEventListener("error", () => {
      cell.remove();
      pruneEmptyGroups(panel);
    });
    cell.appendChild(img);

    const caption = document.createElement("span");
    caption.className = "image-search__caption";
    caption.textContent = result.title || hostOf(result.source_url || result.url);
    cell.appendChild(caption);

    const meta = document.createElement("span");
    meta.className = "image-search__meta";
    // Licence when there is one, host when there is not - so a Wikipedia
    // result says what may be done with it and a web result says only where
    // it was found, which is the whole of what is known about it.
    meta.textContent = result.license || hostOf(result.source_url || result.url);
    cell.appendChild(meta);

    cell.addEventListener("click", async () => {
      panel.hidden = true;
      panel.innerHTML = "";
      await setFromURL(result.url, {
        source_url: result.source_url || "",
        credit: result.credit || "",
        license: result.license || "",
      });
    });
    return cell;
  }

  // A group whose every thumbnail died is a heading over an empty box, which
  // reads as a bug rather than as an answer. It goes, and if that was the last
  // group the panel says what it would have said if nothing had been found -
  // because from where the user sits, nothing was.
  function pruneEmptyGroups(panel) {
    const groups = panel.querySelector(".image-search__groups");
    if (!groups) return;
    for (const section of groups.querySelectorAll(".image-search__group")) {
      if (!section.querySelector(".image-search__result")) section.remove();
    }
    if (!groups.querySelector(".image-search__group")) {
      const status = panel.querySelector(".image-search__status");
      if (!status) return;
      status.textContent = t("image.searchNoResults");
      status.hidden = false;
    }
  }

  function hostOf(url) {
    try {
      return new URL(url).host;
    } catch {
      return "";
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
