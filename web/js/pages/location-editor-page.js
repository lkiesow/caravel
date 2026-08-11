import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { renderItemForm } from "../components/location-form.js";
import { renderImageField } from "../components/image-field.js";
import { renderDocumentList } from "../components/document-list.js";
import { icon } from "../icon.js";

// Two distinct layouts based on whether :itemId is present:
//
// - Create mode: one merged card (cover photo above the fields, so the
//   "Create item" button sits at the true bottom of the form) - same
//   pattern as trip-editor-page.js, including staged cover-photo upload
//   (see image-field.js's staging mode). Location, links, dates, and
//   documents aren't offered here: they're independent sub-resources tied
//   to an item ID that doesn't exist until after this first save, and
//   staging a repeatable list (links/dates) or files (documents) ahead of
//   that is materially more complex than the single staged image - out of
//   scope here, same boundary the plan draws. On success, navigates to the
//   item's view page (unambiguous feedback, consistent with the trip
//   editor fix) - or its edit page if the staged image failed to
//   upload/attach, since that's the only place to retry it.
// - Edit mode: independent cards (Basic Info / Cover Photo / Location /
//   Links / Dates / Documents / Delete), each its own action.
export async function renderLocationEditorPage(container, { tripId, itemId }) {
  let item = null;
  let stagedImage = null;
  if (itemId) {
    try {
      item = await api.get(`/items/${itemId}`);
    } catch {
      container.innerHTML = `<p>${t("common.notFound")}</p><a href="/trips/${tripId}" data-link>${t("common.back")}</a>`;
      return;
    }
  }

  function render() {
    container.innerHTML = `
      <div class="page location-editor">
        <a href="${item ? `/trips/${tripId}/locations/${item.id}` : `/trips/${tripId}`}" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.back"></span></a>
        <div class="page__header">
          <h1 data-i18n="${item ? "location.editor.editTitle" : "location.editor.newTitle"}"></h1>
        </div>
        ${
          item
            ? `
          <div class="editor-card">
            <h4 data-i18n="location.editor.basicInfo"></h4>
            <div class="item-form-slot"></div>
          </div>
          <div class="editor-card">
            <h4 data-i18n="item.detail.image"></h4>
            <div class="image-field-slot"></div>
          </div>
          <div class="editor-card">
            <h4 data-i18n="item.detail.location"></h4>
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
              <button type="submit" class="btn btn-primary">${icon("check")} <span data-i18n="item.detail.saveLocation"></span></button>
            </form>
          </div>
          <div class="editor-card">
            <h4 data-i18n="item.detail.links"></h4>
            <ul class="link-list"></ul>
            <form class="link-form">
              <input type="url" name="url" data-i18n-placeholder="item.detail.linkUrl" required />
              <input type="text" name="label" data-i18n-placeholder="item.detail.linkLabel" />
              <button type="submit" class="btn btn-primary btn-collapse">${icon("plus")} <span data-i18n="item.detail.addLink"></span></button>
            </form>
          </div>
          <div class="editor-card">
            <h4 data-i18n="item.detail.dates"></h4>
            <ul class="date-list"></ul>
            <form class="date-form">
              <input type="date" name="startDate" required />
              <input type="date" name="endDate" data-i18n-placeholder="item.detail.endDate" />
              <input type="text" name="label" data-i18n-placeholder="item.detail.dateLabel" />
              <button type="submit" class="btn btn-primary btn-collapse">${icon("plus")} <span data-i18n="item.detail.addDate"></span></button>
            </form>
          </div>
          <div class="editor-card">
            <h4 data-i18n="item.detail.documents"></h4>
            <div class="document-list-slot"></div>
          </div>
          <div class="editor-card trip-editor__actions">
            <button class="btn btn-danger" data-action="delete">${icon("trash-2")} <span data-i18n="common.delete"></span></button>
          </div>
        `
            : `
          <div class="editor-card">
            <div class="image-field-slot"></div>
            <div class="item-form-slot"></div>
          </div>
        `
        }
      </div>
    `;
    translatePage(container);

    renderItemForm(container.querySelector(".item-form-slot"), item, {
      tripId,
      onSaved: async (saved) => {
        if (item) {
          Object.assign(item, saved);
          return;
        }

        let imageFailed = false;
        if (stagedImage) {
          try {
            let asset;
            if (stagedImage.kind === "file") {
              const formData = new FormData();
              formData.append("file", stagedImage.file);
              const res = await fetch(`/api/trips/${tripId}/media`, { method: "POST", body: formData, credentials: "same-origin" });
              asset = await res.json();
              if (!res.ok) throw new Error(asset.error || "upload failed");
            } else {
              asset = await api.post(`/trips/${tripId}/media/url`, { url: stagedImage.url });
            }
            await api.put(`/items/${saved.id}/image`, { media_asset_id: asset.id });
          } catch (err) {
            imageFailed = true;
            window.alert(err.body?.error || err.message || t("common.error"));
          }
        }
        navigate(imageFailed ? `/trips/${tripId}/locations/${saved.id}/edit` : `/trips/${tripId}/locations/${saved.id}`);
      },
      onCancel: () => {
        if (stagedImage?.kind === "file" && stagedImage.previewUrl) URL.revokeObjectURL(stagedImage.previewUrl);
        navigate(item ? `/trips/${tripId}/locations/${item.id}` : `/trips/${tripId}`);
      },
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
      onStaged: (staged) => {
        stagedImage = staged;
      },
    });

    if (!item) return;

    renderLocationForm();
    renderLinksList();
    bindLinkForm();
    renderDatesList();
    bindDateForm();
    renderDocumentList(container.querySelector(".document-list-slot"), `/items/${item.id}/documents`);

    container.querySelector('[data-action="delete"]').addEventListener("click", async () => {
      if (!window.confirm(t("item.deleteConfirm"))) return;
      await api.delete(`/items/${item.id}`);
      navigate(`/trips/${tripId}`);
    });
  }

  function renderLocationForm() {
    const form = container.querySelector(".location-form");
    if (item.location) {
      form.lat.value = item.location.lat ?? "";
      form.lng.value = item.location.lng ?? "";
      form.address.value = item.location.address ?? "";
    }
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const location = await api.put(`/items/${item.id}/location`, {
        lat: form.lat.value ? Number(form.lat.value) : null,
        lng: form.lng.value ? Number(form.lng.value) : null,
        address: form.address.value || null,
      });
      item.location = location;
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
    list.innerHTML = item.links.length
      ? item.links
          .map(
            (l) =>
              `<li data-link-id="${l.id}"><a href="${escapeAttr(l.url)}" target="_blank" rel="noopener">${escapeHtml(l.label || l.url)}</a> <button class="icon-remove" data-action="delete-link" data-id="${l.id}" aria-label="${t("common.remove")}">${icon("x")}</button></li>`
          )
          .join("")
      : `<li class="empty">${t("item.detail.linksEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-link"]').forEach((btn) => {
      btn.addEventListener("click", async () => {
        await api.delete(`/items/${item.id}/links/${btn.getAttribute("data-id")}`);
        item.links = item.links.filter((l) => l.id !== btn.getAttribute("data-id"));
        renderLinksList();
      });
    });
  }

  function bindLinkForm() {
    const form = container.querySelector(".link-form");
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const link = await api.post(`/items/${item.id}/links`, { url: form.url.value, label: form.label.value || null });
      item.links.push(link);
      form.reset();
      renderLinksList();
    });
  }

  // Same split as renderLinksList()/bindLinkForm() above, same reason.
  function renderDatesList() {
    const list = container.querySelector(".date-list");
    list.innerHTML = item.dates.length
      ? item.dates
          .map((d) => {
            const range = d.end_date ? `${escapeHtml(d.start_date || "")} – ${escapeHtml(d.end_date)}` : escapeHtml(d.start_date || "");
            return `<li data-date-id="${d.id}">${range}${d.label ? " — " + escapeHtml(d.label) : ""} <button class="icon-remove" data-action="delete-date" data-id="${d.id}" aria-label="${t("common.remove")}">${icon("x")}</button></li>`;
          })
          .join("")
      : `<li class="empty">${t("item.detail.datesEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-date"]').forEach((btn) => {
      btn.addEventListener("click", async () => {
        await api.delete(`/items/${item.id}/dates/${btn.getAttribute("data-id")}`);
        item.dates = item.dates.filter((d) => d.id !== btn.getAttribute("data-id"));
        renderDatesList();
      });
    });
  }

  function bindDateForm() {
    const form = container.querySelector(".date-form");
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const date = await api.post(`/items/${item.id}/dates`, {
        start_date: form.startDate.value,
        end_date: form.endDate.value || null,
        label: form.label.value || null,
      });
      item.dates.push(date);
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
