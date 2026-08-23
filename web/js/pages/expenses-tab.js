import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { confirmDialog } from "../components/dialog.js";
import { renderMenu } from "../components/menu.js";
import { renderLoading } from "../components/loading.js";
import { getCurrentUser } from "../session.js";
import { formatMoney, parseMoney, moneyPlaceholder, moneyExample, currencyExponent } from "../format.js";

// A trip's expenses: the total, the rows grouped by the day they were spent,
// and one form that both adds and edits.
//
// No visibility grouping, unlike files and checklists — every expense on a trip
// is visible to everyone on it, deliberately, because hidden rows in a shared
// ledger make an incorrect total look correct. So there are no sections and no
// per-row permission questions: `readOnly` is the only axis, and it comes from
// the trip role like every other tab's.
//
// One form for both verbs rather than an inline editor per row. An expense is
// three fields that are edited together, so a row that expanded into a copy of
// the form would be a second copy of the same markup and the same validation;
// this way the form scrolls into view with the row it is editing highlighted.
//
// The total comes from the server (`total_minor`), not from summing the rows
// here. That keeps one implementation of the arithmetic, and it stays right
// even if this ever shows part of a longer list.
export async function renderExpensesTab(container, trip, { readOnly = false } = {}) {
  renderLoading(container);

  let data = await api.get(`/trips/${trip.id}/expenses`);
  // The expense being edited, or null when the form is an add form.
  let editing = null;
  // Kept across re-renders so a failed save can say why without the message
  // being wiped by the render it triggers.
  let error = null;

  const currency = () => data.currency || trip.currency || "EUR";

  function render() {
    container.innerHTML = `
      <div class="expenses">
        <div class="expenses__summary">
          <span class="expenses__total-label" data-i18n="expenses.total"></span>
          <span class="expenses__total"></span>
        </div>
        <div class="expenses__days"></div>
        <p class="expenses__empty" data-i18n="expenses.empty" hidden></p>
        ${readOnly ? "" : `<div class="editor-card expenses__form-card"></div>`}
      </div>
    `;
    translatePage(container);

    container.querySelector(".expenses__total").textContent = formatMoney(data.total_minor, currency());
    container.querySelector(".expenses__empty").hidden = data.expenses.length > 0;
    renderDays(container.querySelector(".expenses__days"));
    if (!readOnly) renderForm(container.querySelector(".expenses__form-card"));
  }

  // Grouped by spent_on, in the order the server sent (newest day first), so
  // the grouping never disagrees with the sort.
  function renderDays(parent) {
    const days = [];
    for (const expense of data.expenses) {
      const last = days[days.length - 1];
      if (last && last.date === expense.spent_on) last.expenses.push(expense);
      else days.push({ date: expense.spent_on, expenses: [expense] });
    }

    for (const day of days) {
      const section = document.createElement("div");
      section.className = "expenses__day";
      const heading = document.createElement("p");
      heading.className = "expenses__day-heading";
      heading.textContent = formatDay(day.date);
      section.appendChild(heading);
      const list = document.createElement("ul");
      list.className = "expenses__list";
      for (const expense of day.expenses) renderRow(list, expense);
      section.appendChild(list);
      parent.appendChild(section);
    }
  }

  // The day heading, in the browser's locale like every other date in the app
  // (see format.js). Weekday included because "which day was that" is the
  // question you actually have when reading a trip's spending back.
  function formatDay(date) {
    const parsed = new Date(`${date}T00:00:00`);
    return new Intl.DateTimeFormat(undefined, { weekday: "short", month: "short", day: "numeric" }).format(parsed);
  }

  function renderRow(list, expense) {
    const li = document.createElement("li");
    li.className = "expenses__row";
    li.dataset.expenseId = expense.id;
    if (editing && editing.id === expense.id) li.classList.add("expenses__row--editing");
    li.innerHTML = `
      <span class="expenses__row-title"></span>
      <span class="expenses__row-amount"></span>
      <span class="expenses__row-actions"></span>
    `;
    // Assigned as text rather than interpolated, so a title containing markup
    // is a title containing markup.
    li.querySelector(".expenses__row-title").textContent = expense.title;
    li.querySelector(".expenses__row-amount").textContent = formatMoney(expense.amount_minor, currency());

    if (!readOnly) {
      renderMenu(li.querySelector(".expenses__row-actions"), {
        iconName: "ellipsis-vertical",
        chevron: false,
        triggerClass: "expenses__row-trigger",
        // Empty rather than omitted: everything here is an action, so the
        // trigger stays silent. Same reasoning as the checklist row's menu.
        label: "",
        ariaLabel: "expenses.rowActions",
        items: [
          { value: "edit", label: t("expenses.edit"), iconName: "pencil", action: true },
          { value: "delete", label: t("common.delete"), iconName: "trash-2", action: true, danger: true },
        ],
        onSelect: async (action) => {
          if (action === "edit") {
            editing = expense;
            error = null;
            render();
            const card = container.querySelector(".expenses__form-card");
            card?.scrollIntoView({ block: "nearest" });
            card?.querySelector('[name="title"]')?.focus();
            return;
          }
          if (!(await confirmDialog({ messageKey: "expenses.deleteConfirm" }))) return;
          await api.delete(`/expenses/${expense.id}`);
          await reload();
        },
      });
    }
    list.appendChild(li);
  }

  function renderForm(card) {
    const isEditing = editing !== null;
    card.innerHTML = `
      <h2 data-i18n="${isEditing ? "expenses.editHeading" : "expenses.addHeading"}"></h2>
      <form class="item-form expenses__form" novalidate>
        <p class="item-form__error" role="alert" hidden></p>
        <label>
          <span data-i18n="expenses.form.title"></span>
          <input type="text" name="title" required />
        </label>
        <label>
          <span class="expenses__amount-label"></span>
          <!-- text with inputmode rather than type="number": the parsing is
               exact and separator-tolerant in format.js, and a numeric keypad
               is what inputmode is for. -->
          <input type="text" name="amount" inputmode="decimal" required />
        </label>
        <label>
          <span data-i18n="expenses.form.date"></span>
          <input type="date" name="spentOn" required />
        </label>
        <div class="btn-row">
          <button type="submit" class="btn btn-primary">${icon("check")} <span data-i18n="${isEditing ? "common.save" : "expenses.add"}"></span></button>
          ${isEditing ? `<button type="button" class="btn btn-secondary" data-action="cancel">${icon("x")} <span data-i18n="common.cancel"></span></button>` : ""}
        </div>
      </form>
    `;
    translatePage(card);

    // The amount label names the currency, so the field says what unit it wants
    // without a second line of copy explaining it.
    card.querySelector(".expenses__amount-label").textContent = t("expenses.form.amount", { currency: currency() });

    const form = card.querySelector("form");
    form.elements.amount.placeholder = moneyPlaceholder(currency());
    const errorEl = card.querySelector(".item-form__error");

    if (isEditing) {
      form.elements.title.value = editing.title;
      form.elements.amount.value = majorUnits(editing.amount_minor);
      form.elements.spentOn.value = editing.spent_on;
    } else {
      // A new expense defaults to today, clamped into the trip's own dates when
      // it has them: entering yesterday's dinner is a correction, entering one
      // outside the trip entirely is usually a typo.
      form.elements.spentOn.value = defaultDate();
    }
    if (error) {
      errorEl.textContent = error;
      errorEl.hidden = false;
    }

    card.querySelector('[data-action="cancel"]')?.addEventListener("click", () => {
      editing = null;
      error = null;
      render();
    });

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const title = form.elements.title.value.trim();
      const amountMinor = parseMoney(form.elements.amount.value, currency());
      const spentOn = form.elements.spentOn.value;

      // Reported here rather than sent to be refused: the server checks all
      // three too, but a message beside the field beats one that arrives as a
      // failed request. The amount is the one that needs local parsing anyway.
      if (!title) return fail(t("expenses.error.title"));
      if (amountMinor === null) return fail(t("expenses.error.amount", { example: moneyExample(currency()) }));
      if (!spentOn) return fail(t("expenses.error.date"));

      const body = {
        title,
        amount_minor: amountMinor,
        spent_on: spentOn,
        // The payer is always sent explicitly. The server deliberately has no
        // default (see internal/httpapi/expenses.go), so "I paid for this" is
        // this client's decision to make -- and Milestone 4 turns it into a
        // control rather than an assumption. On edit the existing payer is kept
        // rather than reassigned to whoever is editing.
        payer_user_id: isEditing ? editing.payer_user_id : getCurrentUser()?.id || null,
      };

      try {
        if (isEditing) await api.patch(`/expenses/${editing.id}`, body);
        else await api.post(`/trips/${trip.id}/expenses`, body);
      } catch (err) {
        return fail(err?.body?.error || t("common.error"));
      }
      editing = null;
      error = null;
      await reload();
    });

    function fail(message) {
      error = message;
      errorEl.textContent = message;
      errorEl.hidden = false;
    }
  }

  // Shown in the amount field when editing: minor units back to the plain
  // decimal a person types, with no currency symbol or grouping in the way.
  function majorUnits(amountMinor) {
    const exponent = currencyExponent(currency());
    if (exponent <= 0) return String(amountMinor);
    return (amountMinor / 10 ** exponent).toFixed(exponent);
  }

  function defaultDate() {
    const today = new Date().toISOString().slice(0, 10);
    if (trip.start_date && today < trip.start_date) return trip.start_date;
    if (trip.end_date && today > trip.end_date) return trip.end_date;
    return today;
  }

  // Re-fetched rather than patched in place: the total is the server's answer,
  // so a local edit would have to recompute it here to stay consistent -- which
  // is the second implementation of the arithmetic this tab exists to avoid.
  async function reload() {
    data = await api.get(`/trips/${trip.id}/expenses`);
    render();
  }

  render();
}
