import { api } from "../api.js";
import { guardClick } from "../busy.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { CURRENCIES, parseRate, formatRate, RATE_ONE } from "../format.js";

// The extra currencies a trip records expenses in, and the rate for each.
// Stage 32 Milestone 4.
//
// A component of its own rather than fields inside trip-form.js, because that
// form is shared with the create page and rates are not a create-time concern:
// you rarely know them before the trip exists, and a repeatable row group would
// make the create form longer for a case most trips never have.
//
// Rows are edited locally and saved as a set. The endpoint is a replace-all
// PUT, so there is nothing to reconcile: what is on screen when Save is pressed
// is what the trip will have.
//
// The rate is typed the way it is looked up -- "1 JPY = 0.0058 EUR" -- and
// parseRate folds the two currencies' decimal exponents into the integer the
// server stores. See format.js; the server never learns what a decimal place
// is, which is the whole reason that function exists.
// Local, like every other module's copy of it -- the app has no shared DOM
// helper and adding one for a single call would be a change of a different
// kind. Needed because the rate is typed text going back into an attribute
// value on every re-render, and a code change re-renders the whole group.
function escapeHtml(s) {
  return String(s).replace(
    /[&<>"']/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c],
  );
}

export function renderTripCurrenciesField(container, trip, { onSaved } = {}) {
  // Working copy: the saved set is only replaced once the server accepts one.
  let rows = (trip.currencies ?? []).map((c) => ({
    code: c.code,
    rate: formatRate(c.rate_ppb, c.code, trip.currency),
  }));

  // A code may be chosen once. Its own row's code stays offered, or the select
  // could not display what it currently holds.
  const availableFor = (index) =>
    CURRENCIES.filter(
      (code) => code !== trip.currency && (code === rows[index]?.code || !rows.some((r) => r.code === code)),
    );

  const firstUnused = () => CURRENCIES.find((code) => code !== trip.currency && !rows.some((r) => r.code === code));

  function render() {
    container.innerHTML = `
      <p class="editor-card__hint">${t("trip.currencies.hint", { main: trip.currency })}</p>
      <p class="trip-currencies__error" role="alert" hidden></p>
      <div class="trip-currencies__rows">
        ${
          rows.length === 0
            ? `<p class="trip-currencies__empty">${t("trip.currencies.empty", { main: trip.currency })}</p>`
            : rows
                .map(
                  (row, i) => `
          <div class="trip-currencies__row" data-index="${i}">
            <span class="trip-currencies__one">1</span>
            <select class="trip-currencies__code" data-role="code">
              ${availableFor(i)
                .map((code) => `<option value="${code}"${code === row.code ? " selected" : ""}>${code}</option>`)
                .join("")}
            </select>
            <span class="trip-currencies__eq">=</span>
            <input class="trip-currencies__rate" data-role="rate" type="text" inputmode="decimal"
                   value="${escapeHtml(row.rate)}" aria-label="${t("trip.currencies.rateLabel", { code: row.code, main: trip.currency })}" />
            <span class="trip-currencies__main">${trip.currency}</span>
            <button type="button" class="icon-remove" data-action="remove"
                    aria-label="${t("trip.currencies.remove", { code: row.code })}">${icon("x")}</button>
          </div>`,
                )
                .join("")
        }
      </div>
      <div class="trip-currencies__actions">
        <button type="button" class="btn btn-secondary" data-action="add"${firstUnused() ? "" : " disabled"}>
          ${icon("plus")} <span data-i18n="trip.currencies.add"></span>
        </button>
        <button type="button" class="btn btn-primary" data-action="save">
          ${icon("check")} <span data-i18n="common.save"></span>
        </button>
      </div>
    `;
    translatePage(container);

    const errorEl = container.querySelector(".trip-currencies__error");

    // Typing is captured into the working copy rather than read back off the
    // DOM at save time, so a re-render (adding or removing a row) does not
    // discard what was already entered in the others.
    container.querySelectorAll(".trip-currencies__row").forEach((el) => {
      const i = Number(el.dataset.index);
      el.querySelector('[data-role="rate"]').addEventListener("input", (e) => {
        rows[i].rate = e.target.value;
      });
      el.querySelector('[data-role="code"]').addEventListener("change", (e) => {
        rows[i].code = e.target.value;
        // Re-rendered because every other row's options just changed, and
        // because this row's own labels name the code.
        render();
      });
      el.querySelector('[data-action="remove"]').addEventListener("click", () => {
        rows.splice(i, 1);
        render();
      });
    });

    container.querySelector('[data-action="add"]').addEventListener("click", () => {
      const code = firstUnused();
      if (!code) return;
      rows.push({ code, rate: "" });
      render();
    });

    guardClick(container.querySelector('[data-action="save"]'), async () => {
      errorEl.hidden = true;

      // Parsed here as well as on the server so the message lands next to the
      // rows rather than arriving as a round trip -- and because the server
      // never sees the typed text, only the integer this produces.
      const currencies = [];
      for (const row of rows) {
        const ratePPB = parseRate(row.rate, row.code, trip.currency);
        if (ratePPB === null) {
          errorEl.textContent = t("trip.currencies.error.rate", {
            code: row.code,
            example: formatRate(RATE_ONE, row.code, trip.currency),
          });
          errorEl.hidden = false;
          return;
        }
        currencies.push({ code: row.code, rate_ppb: ratePPB });
      }

      try {
        const saved = await api.put(`/trips/${trip.id}/currencies`, { currencies });
        // The server answers with the set it stored, ordered by code. Adopting
        // that rather than keeping the local order means the rows do not
        // reshuffle on the next load.
        trip.currencies = saved.currencies;
        rows = saved.currencies.map((c) => ({
          code: c.code,
          rate: formatRate(c.rate_ppb, c.code, trip.currency),
        }));
        render();
        onSaved?.(saved.currencies);
      } catch (err) {
        // The refusals worth reading are the server's own -- "JPY cannot be
        // removed: 2 expense(s) are recorded in it" names what to go and fix,
        // and no client-side copy could say it.
        errorEl.textContent = err?.message || t("common.error");
        errorEl.hidden = false;
      }
    });
  }

  render();
}
