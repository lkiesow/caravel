import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { renderItemForm } from "../components/location-form.js";
import { renderImageField } from "../components/image-field.js";
import { renderDocumentList } from "../components/document-list.js";
import { icon } from "../icon.js";
import { confirmDialog } from "../components/dialog.js";

// Both modes render the same cards, in the same order - Basic info, Cover
// photo, Location, Links, Dates, Documents - matching the read view's
// section order (location-view-page.js), and both commit through the same
// single Save button at the bottom.
//
// Everything typed on the page is held in `draft` until then, and goes out
// as ONE request: the item's own fields plus nested location/links/dates,
// which handleCreateItem/handleUpdateItem write in a transaction (Stage 09
// Milestone 1). Before that the page had five independent save paths, and
// the visually primary Save only carried Basic info - so coordinates typed
// into the card below it were silently discarded. One request also means
// there is nothing to guard against navigating away from: either Save was
// pressed and everything landed, or it wasn't and nothing did.
//
// Two things still cannot ride along in a JSON body, and both are uploads:
// the cover photo and documents. In edit mode they write immediately
// against the existing item (image-field.js and document-list.js each take
// a path and own their own request). In create mode there is no item to
// attach them to yet, so they stage in memory and flushUploads() writes
// them after the create returns an ID - the one remaining non-atomic step,
// reported through the Basic info card's error line if it fails.
export async function renderLocationEditorPage(container, { tripId, itemId }) {
  let item = null;

  if (itemId) {
    try {
      item = await api.get(`/items/${itemId}`);
    } catch {
      container.innerHTML = `<p>${t("common.notFound")}</p><a href="/trips/${tripId}" data-link>${t("common.back")}</a>`;
      return;
    }
  }

  // The page's working copy, in both modes. links/dates are edited here and
  // sent as whole lists (the API replaces the set), so the add/remove
  // handlers just push and splice - no per-row request, and no difference
  // between creating and editing. image/documents are the upload staging
  // slots, used in create mode only.
  // Set by render(); save() and the per-card Enter handlers reach it through
  // this binding rather than being passed it, since they're all closures over
  // the same single render.
  let itemForm;

  const draft = {
    image: null,
    documents: [],
    links: (item?.links ?? []).map((l) => ({ url: l.url, label: l.label ?? null })),
    dates: (item?.dates ?? []).map((d) => ({ start_date: d.start_date, end_date: d.end_date, label: d.label ?? null })),
  };

  function render() {
    container.innerHTML = `
      <div class="page location-editor">
        <a href="${item ? `/trips/${tripId}/locations/${item.id}` : `/trips/${tripId}`}" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.back"></span></a>
        <div class="page__header">
          <h1></h1>
        </div>

        <div class="editor-card">
          <h2 data-i18n="location.editor.basicInfo"></h2>
          <div class="item-form-slot"></div>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.image"></h2>
          <div class="image-field-slot"></div>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.location"></h2>
          <form class="location-form">
            <label>
              <span data-i18n="item.detail.lat"></span>
              <input type="number" step="any" name="lat" />
            </label>
            <label>
              <span data-i18n="item.detail.lng"></span>
              <input type="number" step="any" name="lng" />
            </label>
            <label>
              <span data-i18n="item.detail.address"></span>
              <input type="text" name="address" />
            </label>
            <label class="location-form__checkbox">
              <input type="checkbox" name="showOnMap" checked />
              <span data-i18n="location.form.showOnMap"></span>
            </label>
            <p class="location-form__hint" data-i18n="location.form.showOnMapHint" hidden></p>
          </form>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.links"></h2>
          <ul class="link-list"></ul>
          <form class="link-form">
            <input type="url" name="url" data-i18n-placeholder="item.detail.linkUrl" required />
            <input type="text" name="label" data-i18n-placeholder="item.detail.linkLabel" />
            <button type="submit" class="btn btn-secondary btn-row">${icon("plus")} <span data-i18n="item.detail.addLink"></span></button>
          </form>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.dates"></h2>
          <ul class="date-list"></ul>
          <form class="date-form">
            <input type="date" name="startDate" required data-i18n-aria-label="item.detail.startDate" />
            <input type="date" name="endDate" data-i18n-placeholder="item.detail.endDate" data-i18n-aria-label="item.detail.endDate" />
            <input type="text" name="label" data-i18n-placeholder="item.detail.dateLabel" />
            <button type="submit" class="btn btn-secondary btn-row">${icon("plus")} <span data-i18n="item.detail.addDate"></span></button>
          </form>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.documents"></h2>
          <div class="document-list-slot"></div>
        </div>

        <div class="editor-actions">
          <button class="btn btn-primary" data-action="save">${icon("check")} <span data-i18n="${item ? "common.save" : "location.editor.createButton"}"></span></button>
          <button class="btn btn-secondary" data-action="cancel">${icon("x")} <span data-i18n="common.cancel"></span></button>
        </div>

        ${
          item
            ? `
          <div class="editor-card">
            <h2 data-i18n="item.deleteHeading"></h2>
            <p class="editor-card__hint" data-i18n="item.deleteDescription"></p>
            <button class="btn btn-danger" data-action="delete">${icon("trash-2")} <span data-i18n="common.delete"></span></button>
          </div>
        `
            : ""
        }
      </div>
    `;
    translatePage(container);
    setHeading();

    itemForm = renderItemForm(container.querySelector(".item-form-slot"), item, { onSubmit: save });

    renderImageField(container.querySelector(".image-field-slot"), {
      tripId,
      imageUrl: item?.image_url,
      attachPath: item ? `/items/${item.id}/image` : undefined,
      onChanged: (updated) => {
        if (item) {
          item.image_id = updated.image_id;
          item.image_url = updated.image_url;
        }
      },
      onStaged: (image) => {
        draft.image = image;
      },
    });

    renderLocationForm();
    renderLinksList();
    bindLinkForm();
    renderDatesList();
    bindDateForm();
    renderDocumentList(container.querySelector(".document-list-slot"), item ? `/items/${item.id}/documents` : null, { staged: draft.documents });

    container.querySelector('[data-action="save"]').addEventListener("click", save);
    container.querySelector('[data-action="cancel"]').addEventListener("click", cancel);

    container.querySelector('[data-action="delete"]')?.addEventListener("click", async () => {
      if (!(await confirmDialog({ messageKey: "item.deleteConfirm" }))) return;
      await api.delete(`/items/${item.id}`);
      navigate(`/trips/${tripId}`);
    });
  }

  // The page's one and only write of the item itself: basic info plus the
  // nested location, links and dates, committed together server-side.
  async function save() {
    itemForm.clearError();

    // show_on_map is a field of the item itself, not of its nested location,
    // even though its checkbox sits in the Location card - see readValues().
    const body = {
      ...itemForm.readValues(),
      show_on_map: container.querySelector('.location-form [name="showOnMap"]').checked,
      links: draft.links,
      dates: draft.dates,
    };

    // Absent means "leave it alone", so only send the key when there is
    // something to say: the typed coordinates, or explicit nulls to clear a
    // location the item already had. An untouched card on a location that
    // never had one sends nothing rather than creating an empty row.
    const location = readLocationForm();
    if (location) body.location = location;
    else if (item?.location) body.location = { lat: null, lng: null, address: null };

    let saved;
    try {
      saved = item ? await api.patch(`/items/${item.id}`, body) : await api.post(`/trips/${tripId}/items`, body);
    } catch (err) {
      itemForm.showError(err.body?.error);
      return;
    }

    // Uploads staged in create mode have an item to attach to now. A failure
    // here leaves the location itself saved, so it reports and stays put
    // rather than navigating away from a half-finished page.
    if (!item) {
      const failure = await flushUploads(saved.id);
      if (failure) {
        itemForm.showError(failure);
        return;
      }
    }

    navigate(`/trips/${tripId}/locations/${saved.id}`);
  }

  function cancel() {
    if (draft.image?.kind === "file" && draft.image.previewUrl) URL.revokeObjectURL(draft.image.previewUrl);
    navigate(item ? `/trips/${tripId}/locations/${item.id}` : `/trips/${tripId}`);
  }

  // Writes the cover photo and documents staged during create against the
  // item that was just created - the two things a JSON body can't carry.
  // Returns an error message, or null when everything landed.
  async function flushUploads(savedId) {
    try {
      if (draft.image) {
        let asset;
        if (draft.image.kind === "file") {
          const formData = new FormData();
          formData.append("file", draft.image.file);
          const res = await fetch(`/api/trips/${tripId}/media`, { method: "POST", body: formData, credentials: "same-origin" });
          asset = await res.json();
          if (!res.ok) throw new Error(asset.error || "upload failed");
        } else {
          asset = await api.post(`/trips/${tripId}/media/url`, { url: draft.image.url });
        }
        await api.put(`/items/${savedId}/image`, { media_asset_id: asset.id });
      }

      for (const doc of draft.documents) {
        const formData = new FormData();
        formData.append("file", doc.file);
        if (doc.note) formData.append("note", doc.note);
        const res = await fetch(`/api/items/${savedId}/documents`, { method: "POST", body: formData, credentials: "same-origin" });
        const body = await res.json();
        if (!res.ok) throw new Error(body.error || "upload failed");
      }
    } catch (err) {
      return err.body?.error || err.message || t("common.error");
    }
    return null;
  }

  // "Edit {title}" needs the item's title interpolated into the string,
  // but translatePage() only ever calls t(key) with no params (see
  // i18n.js) - so this bypasses data-i18n on the <h1> entirely and sets
  // its text directly. Called once after the initial render, and again
  // after Basic Info is saved so the heading picks up a changed title
  // without needing a full page reload.
  function setHeading() {
    container.querySelector(".page__header h1").textContent = item
      ? t("location.editor.editTitle", { title: item.title })
      : t("location.editor.newTitle");
  }

  // null when nothing was filled in, so an untouched Location card says
  // nothing about the item's location rather than clearing it.
  function readLocationForm() {
    const form = container.querySelector(".location-form");
    if (!form.lat.value && !form.lng.value && !form.address.value) return null;
    return {
      lat: form.lat.value ? Number(form.lat.value) : null,
      lng: form.lng.value ? Number(form.lng.value) : null,
      address: form.address.value || null,
    };
  }

  function renderLocationForm() {
    const form = container.querySelector(".location-form");
    if (item?.location) {
      form.lat.value = item.location.lat ?? "";
      form.lng.value = item.location.lng ?? "";
      form.address.value = item.location.address ?? "";
    }
    // Checked by default for a new location, matching the API's own default.
    if (item) form.showOnMap.checked = item.show_on_map;

    // "Show on map" only does anything once there are coordinates to show -
    // GET /trips/{id}/map filters on lat AND lng being present as well as on
    // this flag - and before Milestone 3 the checkbox sat in the Basic info
    // card, several cards above the fields it depends on. It's next to them
    // now, and says so when they're empty rather than silently doing nothing.
    // The box stays enabled either way: unchecking it isn't what's missing,
    // and the user's intent should survive until they fill the coordinates in.
    const hint = container.querySelector(".location-form__hint");
    const syncHint = () => {
      const hasCoordinates = Boolean(form.lat.value && form.lng.value);
      hint.hidden = hasCoordinates || !form.showOnMap.checked;
    };
    form.lat.addEventListener("input", syncHint);
    form.lng.addEventListener("input", syncHint);
    form.showOnMap.addEventListener("change", syncHint);
    syncHint();
    // The card has no button of its own any more - these values are read
    // back by save(). Enter in a coordinate field saves the page, via the
    // same submit-plus-keydown pair the Basic info card uses and for the same
    // reason (see location-form.js): the submit listener catches the native
    // reload, the keydown is what actually fires for a multi-field form with
    // no submit button.
    const saveFromForm = (e) => {
      e.preventDefault();
      save();
    };
    form.addEventListener("submit", saveFromForm);
    form.addEventListener("keydown", (e) => {
      if (e.key === "Enter") saveFromForm(e);
    });
  }

  // renderLinksList()/bindLinkForm() are split (rather than one combined
  // function) so the submit listener can be attached exactly once, from
  // render() below, instead of being re-attached on every add/delete - a
  // form.addEventListener() call inside a function that both handles
  // submit *and* gets re-invoked by its own handler stacks one more
  // listener on the same persistent <form> node every time, doubling on
  // each submit (1 -> 2 -> 4 -> ...). renderLinksList() itself stays safe
  // to call repeatedly: it only touches the <ul>, and the per-item delete
  // buttons it wires are freshly created nodes every time.
  function renderLinksList() {
    const list = container.querySelector(".link-list");
    list.innerHTML = draft.links.length
      ? draft.links
          .map(
            (l, i) =>
              `<li><a href="${escapeAttr(l.url)}" target="_blank" rel="noopener">${escapeHtml(l.label || l.url)}</a> <button class="icon-remove" data-action="delete-link" data-index="${i}" aria-label="${t("common.remove")}">${icon("x")}</button></li>`
          )
          .join("")
      : `<li class="empty">${t("item.detail.linksEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-link"]').forEach((btn) => {
      btn.addEventListener("click", () => {
        draft.links.splice(Number(btn.getAttribute("data-index")), 1);
        renderLinksList();
      });
    });
  }

  function bindLinkForm() {
    const form = container.querySelector(".link-form");
    form.addEventListener("submit", (e) => {
      e.preventDefault();
      draft.links.push({ url: form.url.value, label: form.label.value || null });
      form.reset();
      renderLinksList();
    });
  }

  // Same split as renderLinksList()/bindLinkForm() above, same reason.
  function renderDatesList() {
    const list = container.querySelector(".date-list");
    list.innerHTML = draft.dates.length
      ? draft.dates
          .map((d, i) => {
            const range = d.end_date ? `${escapeHtml(d.start_date || "")} – ${escapeHtml(d.end_date)}` : escapeHtml(d.start_date || "");
            return `<li>${range}${d.label ? " — " + escapeHtml(d.label) : ""} <button class="icon-remove" data-action="delete-date" data-index="${i}" aria-label="${t("common.remove")}">${icon("x")}</button></li>`;
          })
          .join("")
      : `<li class="empty">${t("item.detail.datesEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-date"]').forEach((btn) => {
      btn.addEventListener("click", () => {
        draft.dates.splice(Number(btn.getAttribute("data-index")), 1);
        renderDatesList();
      });
    });
  }

  function bindDateForm() {
    const form = container.querySelector(".date-form");
    form.addEventListener("submit", (e) => {
      e.preventDefault();
      draft.dates.push({ start_date: form.startDate.value, end_date: form.endDate.value || null, label: form.label.value || null });
      form.reset();
      renderDatesList();
    });
  }

  render();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}
