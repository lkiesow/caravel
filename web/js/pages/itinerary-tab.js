import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { confirmDialog } from "../components/dialog.js";
import { renderLoading } from "../components/loading.js";
import { canEdit } from "../trip-role.js";

const CATEGORY_COLORS = {
  site: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
};

export async function renderItineraryTab(container, trip) {
  // Four things on this tab write: removing a day, editing a day's notes,
  // adding an entry, and removing one. A viewer keeps the whole itinerary and
  // loses all four.
  const editable = canEdit(trip);
  renderLoading(container);
  let days = await api.get(`/trips/${trip.id}/itinerary`);
  days.forEach((d) => (d.entries ??= []));
  const items = await api.get(`/trips/${trip.id}/items`);

  // Which days are expanded. Seeded from the rule below and then owned by the
  // user: toggling a day updates this set, so a re-render (adding or removing
  // a day re-renders the whole list) doesn't collapse everything they had
  // opened. Keyed by date rather than by index because days get inserted in
  // the middle when one is added.
  const openDates = initialOpenDates();

  // A day is worth opening if it is still ahead and has something on it, plus
  // the next day coming up even when empty - that is the one being planned.
  // Past days and empty days stay closed; a 10-day trip used to render as ten
  // fully expanded cards and one unbroken scroll.
  function initialOpenDates() {
    // No date range means no "today" to measure against, and every day on such
    // a trip was added deliberately - the same reasoning isRemovable() uses.
    if (!trip.start_date || !trip.end_date) return new Set(days.map((d) => d.date));

    const today = todayISO();
    const open = new Set(days.filter((d) => d.date >= today && hasContent(d)).map((d) => d.date));
    const nextUpcoming = days.find((d) => d.date >= today);
    if (nextUpcoming) open.add(nextUpcoming.date);
    // A trip entirely in the past would otherwise be nothing but closed rows,
    // which reads as a page that failed to load rather than as a summary. Its
    // last day stands in for "where the trip got to".
    if (open.size === 0 && days.length) open.add(days[days.length - 1].date);
    return open;
  }

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
      // Open on arrival even if it's in the past: the user just asked for this
      // day, so they are about to put something on it.
      openDates.add(date);
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
    const el = document.createElement("details");
    el.className = "itinerary-day";
    el.open = openDates.has(day.date);
    // The chevron is drawn rather than left to the UA marker: `display: flex`
    // on a <summary> drops the native triangle, and the header has been a flex
    // row since Stage 04. Rotated by CSS on [open].
    el.innerHTML = `
      <summary class="itinerary-day__header">
        ${icon("chevron-down", { className: "itinerary-day__chevron" })}
        <h2>${formatDate(day.date)}</h2>
        <span class="itinerary-day__count"></span>
        ${editable && isRemovable(day) ? `<button class="icon-remove" data-action="remove-day" aria-label="${t("itinerary.removeDay")}">${icon("x")}</button>` : ""}
      </summary>
      ${
        editable
          ? `<textarea class="itinerary-day__notes" data-i18n-placeholder="itinerary.notesPlaceholder"></textarea>`
          : day.notes
            ? `<textarea class="itinerary-day__notes" readonly></textarea>`
            : ""
      }
      <ul class="itinerary-day__entries"></ul>
      <p class="itinerary-day__empty" data-i18n="itinerary.empty" hidden></p>
      ${
        editable
          ? `<form class="itinerary-day__add-item">
        <select name="itemId" aria-label="${escapeAttr(t("itinerary.addItemTo", { date: formatDate(day.date) }))}">
          <option value="" data-i18n="itinerary.selectItem"></option>
          ${items.map((i) => `<option value="${i.id}">${escapeHtml(i.title)}</option>`).join("")}
        </select>
        <button type="submit" class="btn btn-primary btn-collapse">${icon("plus")} <span data-i18n="itinerary.addItem"></span></button>
      </form>`
          : ""
      }
    `;
    translatePage(el);

    const notesEl = el.querySelector(".itinerary-day__notes");
    if (notesEl) notesEl.value = day.notes ?? "";
    notesEl?.addEventListener("blur", async () => {
      const value = notesEl.value || null;
      if (value === day.notes) return;
      const updated = await api.put(`/trips/${trip.id}/itinerary/days/${day.date}`, { notes: value });
      day.id = updated.id;
      day.notes = value;
    });

    renderEntries(el, day);

    // The user's own expand/collapse wins over the initial rule from here on,
    // including across the re-render that adding or removing a day triggers.
    el.addEventListener("toggle", () => {
      if (el.open) openDates.add(day.date);
      else openDates.delete(day.date);
    });

    el.querySelector('[data-action="remove-day"]')?.addEventListener("click", async (e) => {
      // Inside a <summary>, so without this a click would also toggle the
      // disclosure - the day would fold shut behind its own confirm dialog.
      e.preventDefault();
      e.stopPropagation();
      // Only confirm when there's something to lose. Removing an empty day
      // the user just mistyped shouldn't demand a dialog.
      if (hasContent(day) && !(await confirmDialog({ messageKey: "itinerary.removeDayConfirm", confirmKey: "common.remove" }))) return;
      await api.delete(`/itinerary/days/${day.id}`);
      days = days.filter((d) => d.date !== day.date);
      render();
    });

    el.querySelector(".itinerary-day__add-item")?.addEventListener("submit", async (e) => {
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

    // What a collapsed day says about itself - otherwise a closed row is just
    // a date with no hint whether anything is on it. Updated here rather than
    // in renderDay() so adding or removing an entry keeps it honest; CSS hides
    // it while the day is open, where the entries themselves are visible.
    const count = day.entries.length;
    el.querySelector(".itinerary-day__count").textContent =
      count === 0 ? t("itinerary.summaryEmpty") : t("itinerary.entryCount", { count }, count);

    day.entries.forEach((entry, index) => {
      const li = document.createElement("li");
      // A real <a href>, not a button with a click handler. The router
      // intercepts [data-link] clicks itself, so navigation still stays
      // client-side, but this is the app's primary way into a location from the
      // itinerary and as a link it also gets middle-click, open-in-new-tab,
      // "copy link address" and the browser's own focus and status-bar
      // affordances - none of which a <button> can offer. It was also 22px
      // tall, being styled purely as text; it now carries the tap-target
      // min-height at phone width like every other control.
      li.innerHTML = `
        <a href="/trips/${trip.id}/locations/${entry.item_id}" data-link class="itinerary-entry__link">
          ${
            entry.item_image_url
              ? `<img class="itinerary-entry__thumb" src="${escapeAttr(entry.item_image_url)}" alt="" />`
              : `<span class="dot" style="background:${CATEGORY_COLORS[entry.item_category] || "#71717a"}"></span>`
          }
          <span>${escapeHtml(entry.item_title)}</span>
        </a>
        ${entry.note ? `<span class="itinerary-entry__note">${escapeHtml(entry.note)}</span>` : ""}
        ${
          editable
            ? `<span class="itinerary-entry__actions">
          <button class="icon-btn" data-action="move-up" ${index === 0 ? "disabled" : ""} aria-label="${escapeAttr(t("itinerary.moveUp", { title: entry.item_title }))}">${icon("chevron-up")}</button>
          <button class="icon-btn" data-action="move-down" ${index === day.entries.length - 1 ? "disabled" : ""} aria-label="${escapeAttr(t("itinerary.moveDown", { title: entry.item_title }))}">${icon("chevron-down")}</button>
          <button class="icon-remove" data-action="remove" aria-label="${escapeAttr(t("common.remove"))}">${icon("x")}</button>
        </span>`
            : ""
        }
      `;
      li.querySelector('[data-action="remove"]')?.addEventListener("click", async () => {
        await api.delete(`/itinerary/days/${day.id}/entries/${entry.id}`);
        day.entries = day.entries.filter((e) => e.id !== entry.id);
        renderEntries(el, day);
      });
      li.querySelector('[data-action="move-up"]')?.addEventListener("click", () => moveEntry(el, day, index, -1));
      li.querySelector('[data-action="move-down"]')?.addEventListener("click", () => moveEntry(el, day, index, 1));
      list.appendChild(li);
    });
  }

  // Up/down rather than drag-and-drop. The entries are a list of real links
  // inside a <details> on a 324px phone: native HTML5 drag does not work on
  // touch at all, and a pointer-events reorder is its own piece of work with its
  // own gesture testing (see todo.md).
  //
  // Optimistic: the swap is applied locally and drawn immediately, then sent.
  // The request is the whole new order rather than "move this one" - the server
  // renumbers a day in one transaction, so a reorder cannot be observed
  // half-applied. On failure the day is reloaded from the server rather than
  // left showing an order that was not saved.
  async function moveEntry(el, day, index, delta) {
    const target = index + delta;
    if (target < 0 || target >= day.entries.length) return;

    const reordered = [...day.entries];
    [reordered[index], reordered[target]] = [reordered[target], reordered[index]];
    day.entries = reordered;
    renderEntries(el, day);
    // Keep the moved entry under the finger: after the re-render the button that
    // was just pressed belongs to a *different* row, so focus follows the entry
    // rather than the position - otherwise moving something twice means aiming
    // again between presses, and a keyboard user loses their place entirely.
    //
    // The button pressed may not exist to return to: an entry moved to the top
    // has its "up" disabled, and focusing a disabled button silently focuses
    // nothing (measured - it left document.activeElement on <body>). So the
    // same-direction button is preferred and the opposite one is the fallback,
    // which is always enabled because the entry just came from there.
    const movedRow = el.querySelector(`.itinerary-day__entries li:nth-child(${target + 1})`);
    const sameWay = movedRow?.querySelector(`[data-action="move-${delta < 0 ? "up" : "down"}"]`);
    const otherWay = movedRow?.querySelector(`[data-action="move-${delta < 0 ? "down" : "up"}"]`);
    (sameWay && !sameWay.disabled ? sameWay : otherWay)?.focus();

    try {
      await api.put(`/itinerary/days/${day.id}/entries/order`, {
        entry_ids: reordered.map((e) => e.id),
      });
    } catch (err) {
      console.error("reorder failed", err);
      // The server is the authority on the order, so re-read this day rather
      // than guessing how to undo the local swap. Only this day is refetched:
      // a failed reorder says nothing about the rest of the itinerary, and
      // rebuilding the whole list would collapse and redraw days the user was
      // not touching.
      try {
        const fresh = await api.get(`/trips/${trip.id}/itinerary`);
        const server = fresh.find((d) => d.id === day.id);
        if (server) {
          day.entries = server.entries ?? [];
          renderEntries(el, day);
        }
      } catch (reloadErr) {
        console.error("could not re-read the day after a failed reorder", reloadErr);
      }
    }
  }

  render();
}

// A day counts as having content if anything would be lost by removing it -
// used both for the remove-day confirmation and for deciding what to expand.
function hasContent(day) {
  return day.entries.length > 0 || (day.notes ?? "").trim() !== "";
}

// Today as YYYY-MM-DD in the *local* timezone, so it compares directly against
// the API's date strings. toISOString() would be wrong here: it converts to
// UTC first, so anyone east of Greenwich late in the evening would get
// tomorrow's date and see today's plans collapsed as past.
function todayISO() {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
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
