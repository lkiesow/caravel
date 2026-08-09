import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { renderItemForm } from "./location-form.js";
import { renderImageField } from "./image-field.js";
import { renderDocumentList } from "./document-list.js";

export async function renderItemDetail(container, itemId, { onDeleted, onClose }) {
  const item = await api.get(`/items/${itemId}`);

  function render() {
    container.innerHTML = `
      <div class="item-detail">
        <div class="item-detail__header">
          <h3></h3>
          <button data-action="close" data-i18n="item.detail.close"></button>
        </div>
        <div class="item-core-slot"></div>

        <section>
          <h4 data-i18n="item.detail.image"></h4>
          <div class="image-field-slot"></div>
        </section>

        <section>
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
            <button type="submit" data-i18n="item.detail.saveLocation"></button>
          </form>
        </section>

        <section>
          <h4 data-i18n="item.detail.links"></h4>
          <ul class="link-list"></ul>
          <form class="link-form">
            <input type="url" name="url" data-i18n-placeholder="item.detail.linkUrl" required />
            <input type="text" name="label" data-i18n-placeholder="item.detail.linkLabel" />
            <button type="submit" data-i18n="item.detail.addLink"></button>
          </form>
        </section>

        <section>
          <h4 data-i18n="item.detail.dates"></h4>
          <ul class="date-list"></ul>
          <form class="date-form">
            <input type="date" name="startDate" required />
            <input type="text" name="label" data-i18n-placeholder="item.detail.dateLabel" />
            <button type="submit" data-i18n="item.detail.addDate"></button>
          </form>
        </section>

        <section>
          <h4 data-i18n="item.detail.documents"></h4>
          <div class="document-list-slot"></div>
        </section>

        <button class="item-detail__delete" data-action="delete" data-i18n="common.delete"></button>
      </div>
    `;
    translatePage(container);
    container.querySelector("h3").textContent = item.title;

    container.querySelector('[data-action="close"]').addEventListener("click", () => onClose?.());

    renderCore();
    renderImageField(container.querySelector(".image-field-slot"), {
      tripId: item.trip_id,
      imageUrl: item.image_url,
      attachPath: `/items/${item.id}/image`,
      onChanged: (updated) => {
        item.image_id = updated.image_id;
        item.image_url = updated.image_url;
      },
    });
    renderLocationForm();
    renderLinks();
    renderDates();
    renderDocumentList(container.querySelector(".document-list-slot"), `/items/${item.id}/documents`);

    container.querySelector('[data-action="delete"]').addEventListener("click", async () => {
      if (!window.confirm(t("item.deleteConfirm"))) return;
      await api.delete(`/items/${item.id}`);
      onDeleted?.();
    });
  }

  function renderCore() {
    const slot = container.querySelector(".item-core-slot");
    renderItemForm(slot, item, {
      onSaved: (updated) => {
        Object.assign(item, updated);
        render();
      },
      onCancel: () => {},
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

  function renderLinks() {
    const list = container.querySelector(".link-list");
    list.innerHTML = item.links.length
      ? item.links
          .map(
            (l) =>
              `<li data-link-id="${l.id}"><a href="${escapeAttr(l.url)}" target="_blank" rel="noopener">${escapeHtml(l.label || l.url)}</a> <button data-action="delete-link" data-id="${l.id}" aria-label="${t("common.remove")}">&times;</button></li>`
          )
          .join("")
      : `<li class="empty">${t("item.detail.linksEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-link"]').forEach((btn) => {
      btn.addEventListener("click", async () => {
        await api.delete(`/items/${item.id}/links/${btn.getAttribute("data-id")}`);
        item.links = item.links.filter((l) => l.id !== btn.getAttribute("data-id"));
        renderLinks();
      });
    });

    const form = container.querySelector(".link-form");
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const link = await api.post(`/items/${item.id}/links`, { url: form.url.value, label: form.label.value || null });
      item.links.push(link);
      form.reset();
      renderLinks();
    });
  }

  function renderDates() {
    const list = container.querySelector(".date-list");
    list.innerHTML = item.dates.length
      ? item.dates
          .map(
            (d) =>
              `<li data-date-id="${d.id}">${escapeHtml(d.start_date || "")}${d.label ? " — " + escapeHtml(d.label) : ""} <button data-action="delete-date" data-id="${d.id}" aria-label="${t("common.remove")}">&times;</button></li>`
          )
          .join("")
      : `<li class="empty">${t("item.detail.datesEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-date"]').forEach((btn) => {
      btn.addEventListener("click", async () => {
        await api.delete(`/items/${item.id}/dates/${btn.getAttribute("data-id")}`);
        item.dates = item.dates.filter((d) => d.id !== btn.getAttribute("data-id"));
        renderDates();
      });
    });

    const form = container.querySelector(".date-form");
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const date = await api.post(`/items/${item.id}/dates`, { start_date: form.startDate.value, label: form.label.value || null });
      item.dates.push(date);
      form.reset();
      renderDates();
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
