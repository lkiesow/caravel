import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { confirmDialog } from "../components/dialog.js";
import { renderLoading } from "../components/loading.js";

const CATEGORY_COLORS = {
  site: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
};

export async function renderItineraryTab(container, trip) {
  renderLoading(container);
  let days = await api.get(`/trips/${trip.id}/itinerary`);
  days.forEach((d) => (d.entries ??= []));
  const items = await api.get(`/trips/${trip.id}/items`);

  function render() {
    container.innerHTML = `
      <div class="itinerary">
        ${!trip.start_date || !trip.end_date ? `<p class="itinerary__hint">${t("itinerary.noDates")}</p>` : ""}
        <div class="itinerary-days"></div>
        <form class="itinerary-add-day">
          <input type="date" name="date" required data-i18n-aria-label="itinerary.addDayDate" />
          <button type="submit" class="btn btn-primary btn-collapse">${icon("plus")} <span data-i18n="itinerary.addDay"></span></button>
        </form>
      </div>
    `;
    translatePage(container);

    const list = container.querySelector(".itinerary-days");
    days.forEach((day) => list.appendChild(renderDay(day)));

    container.querySelector(".itinerary-add-day").addEventListener("submit", async (e) => {
      e.preventDefault();
      const date = e.target.date.value;
      if (!date || days.some((d) => d.date === date)) return;
      const day = await api.put(`/trips/${trip.id}/itinerary/days/${date}`, { notes: null });
      days.push(day);
      days.sort((a, b) => a.date.localeCompare(b.date));
      render();
    });
  }

  // A day is removable only if it exists as a row (in-range days with no
  // content are placeholders the API synthesizes, with no id and nothing to
  // delete) and falls outside the trip's range. Deleting an in-range day
  // would just bring the placeholder straight back, so the control would
  // read as broken. A trip with no dates set has no range to be inside, so
  // every day on it was added deliberately and can be removed.
  function isRemovable(day) {
    if (!day.id) return false;
    if (!trip.start_date || !trip.end_date) return true;
    return day.date < trip.start_date || day.date > trip.end_date;
  }

  function renderDay(day) {
    const el = document.createElement("div");
    el.className = "itinerary-day";
    el.innerHTML = `
      <div class="itinerary-day__header">
        <h2>${formatDate(day.date)}</h2>
        ${isRemovable(day) ? `<button class="icon-remove" data-action="remove-day" aria-label="${t("itinerary.removeDay")}">${icon("x")}</button>` : ""}
      </div>
      <textarea class="itinerary-day__notes" data-i18n-placeholder="itinerary.notesPlaceholder"></textarea>
      <ul class="itinerary-day__entries"></ul>
      <p class="itinerary-day__empty" data-i18n="itinerary.empty" hidden></p>
      <form class="itinerary-day__add-item">
        <select name="itemId" aria-label="${escapeAttr(t("itinerary.addItemTo", { date: formatDate(day.date) }))}">
          <option value="" data-i18n="itinerary.selectItem"></option>
          ${items.map((i) => `<option value="${i.id}">${escapeHtml(i.title)}</option>`).join("")}
        </select>
        <button type="submit" class="btn btn-primary btn-collapse">${icon("plus")} <span data-i18n="itinerary.addItem"></span></button>
      </form>
    `;
    translatePage(el);

    const notesEl = el.querySelector(".itinerary-day__notes");
    notesEl.value = day.notes ?? "";
    notesEl.addEventListener("blur", async () => {
      const value = notesEl.value || null;
      if (value === day.notes) return;
      const updated = await api.put(`/trips/${trip.id}/itinerary/days/${day.date}`, { notes: value });
      day.id = updated.id;
      day.notes = value;
    });

    renderEntries(el, day);

    el.querySelector('[data-action="remove-day"]')?.addEventListener("click", async () => {
      // Only confirm when there's something to lose. Removing an empty day
      // the user just mistyped shouldn't demand a dialog.
      const hasContent = day.entries.length > 0 || (day.notes ?? "").trim() !== "";
      if (hasContent && !(await confirmDialog({ messageKey: "itinerary.removeDayConfirm", confirmKey: "common.remove" }))) return;
      await api.delete(`/itinerary/days/${day.id}`);
      days = days.filter((d) => d.date !== day.date);
      render();
    });

    el.querySelector(".itinerary-day__add-item").addEventListener("submit", async (e) => {
      e.preventDefault();
      const select = e.target.itemId;
      if (!select.value) return;
      const dayRecord = await ensureDay(day);
      const entry = await api.post(`/itinerary/days/${dayRecord.id}/entries`, { item_id: select.value });
      day.id = dayRecord.id;
      day.entries.push(entry);
      select.value = "";
      renderEntries(el, day);
    });

    return el;
  }

  // Ensures `day` has a persisted id (creating it with empty notes if it
  // doesn't), since entries can only be added to a day that already exists.
  async function ensureDay(day) {
    if (day.id) return day;
    const created = await api.put(`/trips/${trip.id}/itinerary/days/${day.date}`, { notes: day.notes });
    day.id = created.id;
    return created;
  }

  function renderEntries(el, day) {
    const list = el.querySelector(".itinerary-day__entries");
    const emptyState = el.querySelector(".itinerary-day__empty");
    list.innerHTML = "";
    emptyState.hidden = day.entries.length > 0;

    for (const entry of day.entries) {
      const li = document.createElement("li");
      li.innerHTML = `
        <button type="button" class="itinerary-entry__link" data-action="open">
          ${
            entry.item_image_url
              ? `<img class="itinerary-entry__thumb" src="${escapeAttr(entry.item_image_url)}" alt="" />`
              : `<span class="dot" style="background:${CATEGORY_COLORS[entry.item_category] || "#71717a"}"></span>`
          }
          <span>${escapeHtml(entry.item_title)}</span>
        </button>
        ${entry.note ? `<span class="itinerary-entry__note">${escapeHtml(entry.note)}</span>` : ""}
        <button class="icon-remove" data-action="remove" aria-label="${t("common.remove")}">${icon("x")}</button>
      `;
      li.querySelector('[data-action="open"]').addEventListener("click", () => {
        navigate(`/trips/${trip.id}/locations/${entry.item_id}`);
      });
      li.querySelector('[data-action="remove"]').addEventListener("click", async () => {
        await api.delete(`/itinerary/days/${day.id}/entries/${entry.id}`);
        day.entries = day.entries.filter((e) => e.id !== entry.id);
        renderEntries(el, day);
      });
      list.appendChild(li);
    }
  }

  render();
}

function formatDate(dateStr) {
  const date = new Date(`${dateStr}T00:00:00`);
  return new Intl.DateTimeFormat(undefined, { weekday: "short", year: "numeric", month: "short", day: "numeric" }).format(date);
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}
