import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { renderItemForm } from "../components/location-form.js";
import { renderImageField } from "../components/image-field.js";
import { renderDocumentList } from "../components/document-list.js";
import { icon } from "../icon.js";

// Both modes now render the same cards, in the same order - Basic info,
// Cover photo, Location, Links, Dates, Documents - matching the read view's
// section order (location-view-page.js). What differs is when the writes
// happen:
//
// - Edit mode: each card saves on its own (its own Save button, or an
//   add/remove that hits the API immediately), plus a Delete card at the
//   end. The item ID exists, so every sub-resource endpoint is reachable.
// - Create mode: nothing is written until the single "Create location"
//   button at the bottom. Everything typed or picked before that is held in
//   memory - the cover photo via image-field.js's staging mode, documents
//   via document-list.js's, links and dates as plain arrays, coordinates
//   read straight off the form at flush time - and then written in one
//   sequence after the item POST returns an ID. The location, links, dates
//   and documents endpoints all require an existing item, and a multipart
//   document upload can't ride along in a JSON create request, so
//   post-create writes are the only shape available without a backend
//   change (a transactional create is a todo.md entry).
//
// That sequence isn't atomic: if the item is created but a link fails, the
// location exists half-populated. Same policy as the staged cover photo has
// always used - report what failed once, and land on the edit page rather
// than the view page, since that's where the missing pieces can be
// re-added.
export async function renderLocationEditorPage(container, { tripId, itemId }) {
  let item = null;
  // Create mode only; in edit mode the equivalents live on `item` (or are
  // already on the server, for documents).
  const staged = { image: null, links: [], dates: [], documents: [] };

  if (itemId) {
    try {
      item = await api.get(`/items/${itemId}`);
    } catch {
      container.innerHTML = `<p>${t("common.notFound")}</p><a href="/trips/${tripId}" data-link>${t("common.back")}</a>`;
      return;
    }
  }

  // The list-backing arrays: the item's own in edit mode, the staging ones
  // in create mode. Both are mutated in place by the add/remove handlers.
  const links = () => (item ? item.links : staged.links);
  const dates = () => (item ? item.dates : staged.dates);

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
            ${item ? `<button type="submit" class="btn btn-secondary btn-row">${icon("check")} <span data-i18n="item.detail.saveLocation"></span></button>` : ""}
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

        ${
          item
            ? `
          <div class="editor-card trip-editor__actions">
            <button class="btn btn-danger" data-action="delete">${icon("trash-2")} <span data-i18n="common.delete"></span></button>
          </div>
        `
            : `
          <div class="editor-actions">
            <button class="btn btn-primary" data-action="create">${icon("check")} <span data-i18n="location.editor.createButton"></span></button>
            <button class="btn btn-secondary" data-action="cancel">${icon("x")} <span data-i18n="common.cancel"></span></button>
          </div>
        `
        }
      </div>
    `;
    translatePage(container);
    setHeading();

    const itemForm = renderItemForm(container.querySelector(".item-form-slot"), item, {
      tripId,
      showActions: Boolean(item),
      onSaved: async (saved) => {
        if (item) {
          Object.assign(item, saved);
          setHeading();
          return;
        }
        await flushStaged(saved);
      },
      onCancel: cancel,
    });

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
        staged.image = image;
      },
    });

    renderLocationForm();
    renderLinksList();
    bindLinkForm();
    renderDatesList();
    bindDateForm();
    renderDocumentList(container.querySelector(".document-list-slot"), item ? `/items/${item.id}/documents` : null, { staged: staged.documents });

    if (item) {
      container.querySelector('[data-action="delete"]').addEventListener("click", async () => {
        if (!window.confirm(t("item.deleteConfirm"))) return;
        await api.delete(`/items/${item.id}`);
        navigate(`/trips/${tripId}`);
      });
      return;
    }

    container.querySelector('[data-action="create"]').addEventListener("click", () => itemForm.submit());
    container.querySelector('[data-action="cancel"]').addEventListener("click", cancel);
  }

  function cancel() {
    if (staged.image?.kind === "file" && staged.image.previewUrl) URL.revokeObjectURL(staged.image.previewUrl);
    navigate(item ? `/trips/${tripId}/locations/${item.id}` : `/trips/${tripId}`);
  }

  // Writes everything staged in create mode against the item that was just
  // created, in the order the cards appear, then navigates. Failures are
  // collected rather than thrown so one bad link doesn't silently drop the
  // dates after it, and reported once at the end.
  async function flushStaged(saved) {
    const failures = [];
    const fail = (err) => failures.push(err.body?.error || err.message || t("common.error"));

    if (staged.image) {
      try {
        let asset;
        if (staged.image.kind === "file") {
          const formData = new FormData();
          formData.append("file", staged.image.file);
          const res = await fetch(`/api/trips/${tripId}/media`, { method: "POST", body: formData, credentials: "same-origin" });
          asset = await res.json();
          if (!res.ok) throw new Error(asset.error || "upload failed");
        } else {
          asset = await api.post(`/trips/${tripId}/media/url`, { url: staged.image.url });
        }
        await api.put(`/items/${saved.id}/image`, { media_asset_id: asset.id });
      } catch (err) {
        fail(err);
      }
    }

    const location = readLocationForm();
    if (location) {
      try {
        await api.put(`/items/${saved.id}/location`, location);
      } catch (err) {
        fail(err);
      }
    }

    for (const link of staged.links) {
      try {
        await api.post(`/items/${saved.id}/links`, { url: link.url, label: link.label });
      } catch (err) {
        fail(err);
      }
    }

    for (const date of staged.dates) {
      try {
        await api.post(`/items/${saved.id}/dates`, { start_date: date.start_date, end_date: date.end_date, label: date.label });
      } catch (err) {
        fail(err);
      }
    }

    for (const doc of staged.documents) {
      try {
        const formData = new FormData();
        formData.append("file", doc.file);
        if (doc.note) formData.append("note", doc.note);
        const res = await fetch(`/api/items/${saved.id}/documents`, { method: "POST", body: formData, credentials: "same-origin" });
        const body = await res.json();
        if (!res.ok) throw new Error(body.error || "upload failed");
      } catch (err) {
        fail(err);
      }
    }

    if (failures.length) {
      window.alert(failures.join("\n"));
      navigate(`/trips/${tripId}/locations/${saved.id}/edit`);
      return;
    }
    navigate(`/trips/${tripId}/locations/${saved.id}`);
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

  // null when nothing was filled in, so an untouched Location card doesn't
  // produce a pointless all-null PUT on create.
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
    // Create mode has no per-card save button: the values are read back by
    // readLocationForm() when the whole page is committed.
    if (!item) return;
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      item.location = await api.put(`/items/${item.id}/location`, readLocationForm() ?? { lat: null, lng: null, address: null });
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
    list.innerHTML = links().length
      ? links()
          .map(
            (l, i) =>
              `<li><a href="${escapeAttr(l.url)}" target="_blank" rel="noopener">${escapeHtml(l.label || l.url)}</a> <button class="icon-remove" data-action="delete-link" data-index="${i}" aria-label="${t("common.remove")}">${icon("x")}</button></li>`
          )
          .join("")
      : `<li class="empty">${t("item.detail.linksEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-link"]').forEach((btn) => {
      btn.addEventListener("click", async () => {
        const i = Number(btn.getAttribute("data-index"));
        if (item) await api.delete(`/items/${item.id}/links/${item.links[i].id}`);
        links().splice(i, 1);
        renderLinksList();
      });
    });
  }

  function bindLinkForm() {
    const form = container.querySelector(".link-form");
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const link = { url: form.url.value, label: form.label.value || null };
      links().push(item ? await api.post(`/items/${item.id}/links`, link) : link);
      form.reset();
      renderLinksList();
    });
  }

  // Same split as renderLinksList()/bindLinkForm() above, same reason.
  function renderDatesList() {
    const list = container.querySelector(".date-list");
    list.innerHTML = dates().length
      ? dates()
          .map((d, i) => {
            const range = d.end_date ? `${escapeHtml(d.start_date || "")} – ${escapeHtml(d.end_date)}` : escapeHtml(d.start_date || "");
            return `<li>${range}${d.label ? " — " + escapeHtml(d.label) : ""} <button class="icon-remove" data-action="delete-date" data-index="${i}" aria-label="${t("common.remove")}">${icon("x")}</button></li>`;
          })
          .join("")
      : `<li class="empty">${t("item.detail.datesEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-date"]').forEach((btn) => {
      btn.addEventListener("click", async () => {
        const i = Number(btn.getAttribute("data-index"));
        if (item) await api.delete(`/items/${item.id}/dates/${item.dates[i].id}`);
        dates().splice(i, 1);
        renderDatesList();
      });
    });
  }

  function bindDateForm() {
    const form = container.querySelector(".date-form");
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const date = { start_date: form.startDate.value, end_date: form.endDate.value || null, label: form.label.value || null };
      dates().push(item ? await api.post(`/items/${item.id}/dates`, date) : date);
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
