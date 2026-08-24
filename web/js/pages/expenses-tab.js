import { api } from "../api.js";
import { guardForm } from "../busy.js";
import { t, translatePage, getLocale } from "../i18n.js";
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
// per-row permission questions: `readOnly` is one axis and comes from the trip
// role like every other tab's.
//
// `shared` is the other, and it decides whether *who paid* is a question worth
// asking. On a solo trip the answer is always you: the select would have one
// option, the payer line under every row would say the same name, and the
// per-person summary would be a table with one row in it. So all three appear
// only when somebody else is on the trip — the same reasoning file-list.js and
// checklist-list.js use for their visibility controls, applied to a different
// question.
//
// One form for both verbs rather than an inline editor per row. An expense is
// three fields that are edited together, so a row that expanded into a copy of
// the form would be a second copy of the same markup and the same validation;
// this way the form scrolls into view with the row it is editing highlighted.
//
// The total comes from the server (`total_minor`), not from summing the rows
// here. That keeps one implementation of the arithmetic, and it stays right
// even if this ever shows part of a longer list.
export async function renderExpensesTab(container, trip, { readOnly = false, shared = false } = {}) {
  renderLoading(container);

  let data = await api.get(`/trips/${trip.id}/expenses`);
  // Everyone who could have paid. Fetched only when it is going to be offered:
  // on a solo trip this is a request whose answer is a list of one, and the
  // endpoint needs only viewer so a read-only member can still load it to
  // render the names beside the rows.
  let members = [];
  if (shared) {
    try {
      members = await api.get(`/trips/${trip.id}/members`);
    } catch {
      // A failed member list must not take the ledger down with it: the amounts
      // are the point, and the payer select falls back to "just me" below.
      members = [];
    }
  }
  // The expense being edited, or null when the form is an add form.
  let editing = null;
  // Kept across re-renders so a failed save can say why without the message
  // being wiped by the render it triggers.
  let error = null;

  const currency = () => data.currency || trip.currency || "EUR";

  function render() {
    container.innerHTML = `
      <div class="expenses">
        <!-- Total at the top because it is the headline number; who paid, the
             nets and the transfers all sit below the rows and the form, because
             they are conclusions drawn from them. Reading a balance before
             seeing what it is made of is what made the subset case
             inexplicable. -->
        <div class="expenses__summary">
          <span class="expenses__total-label" data-i18n="expenses.total"></span>
          <span class="expenses__total"></span>
        </div>
        <div class="expenses__days"></div>
        <p class="expenses__empty" data-i18n="expenses.empty" hidden></p>
        ${readOnly ? "" : `<div class="editor-card expenses__form-card"></div>`}
        ${
          shared
            ? `<div class="editor-card expenses__summary-card">
          <h2 data-i18n="expenses.summaryHeading"></h2>
          <div class="expenses__payers"></div>
          <div class="expenses__balances"></div>
        </div>`
            : ""
        }
      </div>
    `;
    translatePage(container);

    container.querySelector(".expenses__total").textContent = formatMoney(data.total_minor, currency());
    container.querySelector(".expenses__empty").hidden = data.expenses.length > 0;
    if (shared) {
      const payers = container.querySelector(".expenses__payers");
      const balances = container.querySelector(".expenses__balances");
      renderPayers(payers);
      renderBalances(balances);
      // A card with nothing but its heading in it is worse than no card, and
      // both sections decline to render on a trip with no expenses yet. Asked
      // after the fact rather than predicted, so the two rules for "is there
      // anything to say" stay inside the functions that own them.
      container.querySelector(".expenses__summary-card").hidden = !payers.children.length && !balances.children.length;
    }
    renderDays(container.querySelector(".expenses__days"));
    if (!readOnly) renderForm(container.querySelector(".expenses__form-card"));
  }

  // Who paid, and how much of the total each of them covered. The rows come
  // from the server already grouped and ordered (see payerTotals in
  // internal/httpapi/expenses.go), so nothing here decides what anybody paid.
  //
  // Not shown at all until there is something to show: a trip whose only
  // expenses are unattributed would otherwise render a one-row table restating
  // the total.
  function renderPayers(parent) {
    parent.replaceChildren();
    const rows = data.payers || [];
    if (!rows.length || (rows.length === 1 && !rows[0].user_id)) return;

    const heading = document.createElement("p");
    heading.className = "expenses__payers-heading";
    heading.textContent = t("expenses.whoPaid");
    parent.appendChild(heading);

    const list = document.createElement("ul");
    list.className = "expenses__payers-list";
    for (const row of rows) {
      const li = document.createElement("li");
      li.className = "expenses__payer";
      const name = document.createElement("span");
      name.className = "expenses__payer-name";
      name.textContent = payerLabel(row);
      if (!row.user_id) name.classList.add("expenses__payer-name--none");
      const paid = document.createElement("span");
      paid.className = "expenses__payer-paid";
      paid.textContent = formatMoney(row.paid_minor, currency());
      li.append(name, paid);
      list.appendChild(li);
    }
    parent.appendChild(list);
  }

  // What to call a payer. A null id is an expense nobody is recorded as having
  // paid for -- an account that has since been deleted, or somebody outside the
  // trip -- and it gets a plain label rather than a blank, because an empty
  // name reads as a rendering bug rather than as a fact about the row.
  function payerLabel(row) {
    if (!row.user_id) return t("expenses.payer.none");
    return row.display_name || t("expenses.payer.none");
  }

  // Who owes whom. Every number here comes from the server (see
  // computeBalances in internal/httpapi/expense_balances.go) -- recomputing a
  // balance in the client would be a second implementation of the rounding rule
  // in splitAmount, and the two would eventually disagree about what somebody
  // owes depending on which screen they were looking at.
  //
  // Two sections: where everyone stands, and what would settle it. The second
  // is advice, not a record: nothing here marks a debt as paid, which is why
  // the heading says "to settle up" rather than anything more official.
  function renderBalances(parent) {
    parent.replaceChildren();
    const balances = data.balances;
    if (!balances) return;

    // A trip where nobody owes anybody renders the heading and the "settled"
    // line, not an empty box: "you are square" is worth saying.
    const unsettled = (balances.people || []).some((p) => p.net_minor !== 0);
    if (!unsettled && !balances.unattributed_minor) {
      if (!data.expenses.length) return;
      const settled = document.createElement("p");
      settled.className = "expenses__balances-settled";
      settled.textContent = t("expenses.balances.settled");
      parent.append(headingEl(t("expenses.balances.heading")), settled);
      return;
    }

    parent.appendChild(headingEl(t("expenses.balances.heading")));

    const list = document.createElement("ul");
    list.className = "expenses__balance-list";
    for (const person of balances.people || []) {
      const li = document.createElement("li");
      li.className = "expenses__balance";
      const name = document.createElement("span");
      name.className = "expenses__balance-name";
      name.textContent = person.display_name || t("expenses.payer.none");
      const value = document.createElement("span");
      value.className = "expenses__balance-net";
      // Rendered as a direction plus a positive amount rather than a signed
      // number: "owes 12.50" is read correctly first time, where "-12.50"
      // needs the reader to work out whose sign convention it is.
      if (person.net_minor > 0) {
        value.textContent = t("expenses.balances.isOwed", { amount: formatMoney(person.net_minor, currency()) });
        value.classList.add("expenses__balance-net--credit");
      } else if (person.net_minor < 0) {
        value.textContent = t("expenses.balances.owes", { amount: formatMoney(-person.net_minor, currency()) });
        value.classList.add("expenses__balance-net--debt");
      } else {
        value.textContent = t("expenses.balances.even");
        value.classList.add("expenses__balance-net--even");
      }
      li.append(name, value);
      list.appendChild(li);
    }
    parent.appendChild(list);

    if ((balances.transfers || []).length) {
      parent.appendChild(headingEl(t("expenses.balances.settleHeading")));
      const transfers = document.createElement("ul");
      transfers.className = "expenses__transfer-list";
      for (const transfer of balances.transfers) {
        const li = document.createElement("li");
        li.className = "expenses__transfer";
        // Split into who-pays-whom and how-much, so the amount lands in the
        // same right-hand column as every other amount on the tab rather than
        // trailing off the end of a sentence. The copy therefore holds no
        // {amount}: see expenses.balances.transfer.
        const who = document.createElement("span");
        who.className = "expenses__transfer-who";
        who.textContent = t("expenses.balances.transfer", {
          from: transfer.from_display_name || t("expenses.payer.none"),
          to: transfer.to_display_name || t("expenses.payer.none"),
        });
        const amount = document.createElement("span");
        amount.className = "expenses__transfer-amount";
        amount.textContent = formatMoney(transfer.amount_minor, currency());
        li.append(who, amount);
        transfers.appendChild(li);
      }
      parent.appendChild(transfers);
    }

    // Said out loud rather than folded into the numbers above: an expense
    // nobody is recorded as paying cannot be attributed, so it is left out of
    // the balance entirely. Splitting it would be a confidently wrong number.
    if (balances.unattributed_minor) {
      const note = document.createElement("p");
      note.className = "expenses__balances-note";
      note.textContent = t("expenses.balances.unattributed", {
        amount: formatMoney(balances.unattributed_minor, currency()),
      });
      parent.appendChild(note);
    }
  }

  function headingEl(text) {
    const heading = document.createElement("p");
    heading.className = "expenses__payers-heading";
    heading.textContent = text;
    return heading;
  }

  // The people an expense was shared with, as a readable list -- or null when
  // it was shared with the whole trip, which needs no explaining.
  //
  // Intl.ListFormat rather than joining with commas: "Anna, Ben and you" needs
  // a conjunction that differs per language, and the browser already knows it.
  //
  // Given getLocale() explicitly, unlike the money and date formatters in
  // format.js, which take the browser's locale. The distinction is real: a
  // thousands separator is a preference about how *you* like numbers, but the
  // word "and" is part of a translated sentence. Left on the browser locale
  // this produced "Nur für Other User and dich" -- an English conjunction
  // inside German copy.
  function subsetShareNames(expense) {
    const ids = expense.share_user_ids || [];
    if (!members.length || ids.length >= members.length) return null;

    const names = [];
    let unknown = 0;
    for (const id of ids) {
      const member = members.find((m) => m.user_id === id);
      if (!member) unknown++;
      else names.push(member.is_self ? t("expenses.shares.you") : member.display_name);
    }
    // Somebody in the share set who has since left the trip is not in the
    // member list. Naming only the people we can name would understate the
    // split, which is the exact confusion this line exists to remove, so their
    // presence is stated instead of dropped.
    if (unknown > 0) names.push(t("expenses.shares.former"));
    if (!names.length) return null;

    try {
      return new Intl.ListFormat(getLocale(), { style: "long", type: "conjunction" }).format(names);
    } catch {
      return names.join(", ");
    }
  }

  // What the reading user owes for one expense. `share_minor` is null when they
  // are not among the people it was for, which is a different statement from a
  // share of zero and reads differently.
  function shareLabel(expense) {
    if (expense.share_minor === null || expense.share_minor === undefined) return t("expenses.notShared");
    return t("expenses.yourShare", { amount: formatMoney(expense.share_minor, currency()) });
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
      <span class="expenses__row-main">
        <span class="expenses__row-title"></span>
        ${shared ? `<span class="expenses__row-payer"></span>` : ""}
        ${shared ? `<span class="expenses__row-share"></span>` : ""}
        ${shared ? `<span class="expenses__row-shares"></span>` : ""}
      </span>
      <span class="expenses__row-amount"></span>
      <span class="expenses__row-actions"></span>
    `;
    // Assigned as text rather than interpolated, so a title containing markup
    // is a title containing markup.
    li.querySelector(".expenses__row-title").textContent = expense.title;
    li.querySelector(".expenses__row-amount").textContent = formatMoney(expense.amount_minor, currency());
    // A second line under the title rather than a fourth column: at 324px the
    // row already gives the amount and the menu fixed width, and a name is the
    // piece that would have had to truncate to nothing.
    //
    // It carries who paid and what the reader themselves owes, which is the one
    // number a person reading a shared ledger is actually looking for. The
    // whole split is not spelled out per row -- that is what the balances are
    // for.
    const payerEl = li.querySelector(".expenses__row-payer");
    if (payerEl) {
      payerEl.textContent = t("expenses.paidBy", {
        name: payerLabel({ user_id: expense.payer_user_id, display_name: expense.payer_display_name }),
      });
    }
    // On its own line rather than joined to the payer with a "·". Together they
    // ran to about 38 characters, which does not fit the row at 324px -- and
    // the half that got truncated was the amount, the one number on the line
    // somebody is actually looking for.
    const shareEl = li.querySelector(".expenses__row-share");
    if (shareEl) shareEl.textContent = shareLabel(expense);

    // Who the expense was for, but only when that is not everybody. This is the
    // line the balances were missing: with three people on a trip and an
    // expense shared by two of them, the rows gave no way to see where the
    // final numbers came from. An expense split with the whole trip says
    // nothing here, which keeps the common case as quiet as it was.
    const sharesEl = li.querySelector(".expenses__row-shares");
    if (sharesEl) {
      const names = subsetShareNames(expense);
      if (names) sharesEl.textContent = t("expenses.onlyFor", { people: names });
      else sharesEl.hidden = true;
    }

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
        ${
          shared && members.length
            ? `<label>
          <span data-i18n="expenses.form.payer"></span>
          <select name="payer">
            ${members.map((m) => `<option value="${m.user_id}"></option>`).join("")}
            <option value=""></option>
          </select>
        </label>
        <div class="expenses__shares">
          <!-- Everything-splits-evenly is the overwhelmingly common case, so it
               is one checkbox and the member list only appears when somebody
               actually wants a subset. The form used to show every member's
               checkbox on every expense, which was a row of controls to read
               past for a decision almost nobody was making. -->
          <label class="expenses__share-choice expenses__share-all">
            <input type="checkbox" name="shareAll" checked />
            <span data-i18n="expenses.form.shareAll"></span>
          </label>
          <span id="expense-shares-label" data-i18n="expenses.form.shares" hidden></span>
          <div class="expenses__shares-group" role="group" aria-labelledby="expense-shares-label" hidden>
            ${members
              .map(
                (m) => `<label class="expenses__share-choice">
              <input type="checkbox" name="share" value="${m.user_id}" />
              <span></span>
            </label>`
              )
              .join("")}
          </div>
        </div>`
            : ""
        }
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

    // Option labels as text, not interpolated markup: a display name is
    // whatever somebody typed into their profile.
    if (form.elements.payer) {
      const options = [...form.elements.payer.options];
      members.forEach((m, i) => {
        options[i].textContent = m.is_self ? t("expenses.payer.me", { name: m.display_name }) : m.display_name;
      });
      // The last option is the deliberate "nobody": an expense somebody outside
      // the trip paid for. Absent from the list until now, because Milestone 3
      // could only produce it by omitting the field.
      options[options.length - 1].textContent = t("expenses.payer.none");
    }

    // Share checkboxes, labelled the same way as the payer options.
    const shareBoxes = [...card.querySelectorAll('[name="share"]')];
    shareBoxes.forEach((box, i) => {
      const m = members[i];
      box.nextElementSibling.textContent = m.is_self ? t("expenses.payer.me", { name: m.display_name }) : m.display_name;
    });

    const shareAll = form.elements.shareAll;
    const shareGroup = card.querySelector(".expenses__shares-group");
    const shareGroupLabel = card.querySelector("#expense-shares-label");
    function syncShareGroup() {
      const choosing = shareAll && !shareAll.checked;
      if (shareGroup) shareGroup.hidden = !choosing;
      if (shareGroupLabel) shareGroupLabel.hidden = !choosing;
      // Opening the picker starts from everyone rather than from nothing: it is
      // a list to narrow, not one to build from scratch.
      if (choosing && !shareBoxes.some((b) => b.checked)) {
        for (const box of shareBoxes) box.checked = true;
      }
    }
    shareAll?.addEventListener("change", syncShareGroup);

    if (isEditing) {
      form.elements.title.value = editing.title;
      form.elements.amount.value = majorUnits(editing.amount_minor);
      form.elements.spentOn.value = editing.spent_on;
      // The expense's own payer, not the person editing. If they have since
      // left the trip they are not in the select at all, and the value falls
      // through to "nobody" rather than silently reassigning the expense to
      // whoever opened the form.
      if (form.elements.payer) form.elements.payer.value = editing.payer_user_id || "";
      // The server sends the *effective* set, so an expense that was never
      // pinned arrives listing everybody -- which is exactly what it means, and
      // the toggle stays on "everyone". A genuine subset opens the picker with
      // its own people ticked.
      const editingIDs = editing.share_user_ids || [];
      for (const box of shareBoxes) box.checked = editingIDs.includes(box.value);
      if (shareAll) shareAll.checked = shareBoxes.every((b) => b.checked);
    } else {
      // A new expense defaults to today, clamped into the trip's own dates when
      // it has them: entering yesterday's dinner is a correction, entering one
      // outside the trip entirely is usually a typo.
      form.elements.spentOn.value = defaultDate();
      // Defaults to you, which is what the server deliberately does not do.
      if (form.elements.payer) form.elements.payer.value = getCurrentUser()?.id || "";
      // A new expense is for everyone by default: splitting the whole trip is
      // the common case, and narrowing it is the deliberate act.
      for (const box of shareBoxes) box.checked = true;
      if (shareAll) shareAll.checked = true;
    }
    syncShareGroup();
    if (error) {
      errorEl.textContent = error;
      errorEl.hidden = false;
    }

    card.querySelector('[data-action="cancel"]')?.addEventListener("click", () => {
      editing = null;
      error = null;
      render();
    });

    guardForm(form, async () => {
      const title = form.elements.title.value.trim();
      const amountMinor = parseMoney(form.elements.amount.value, currency());
      const spentOn = form.elements.spentOn.value;

      // Reported here rather than sent to be refused: the server checks all
      // three too, but a message beside the field beats one that arrives as a
      // failed request. The amount is the one that needs local parsing anyway.
      if (!title) return fail(t("expenses.error.title"));
      if (amountMinor === null) return fail(t("expenses.error.amount", { example: moneyExample(currency()) }));
      if (!spentOn) return fail(t("expenses.error.date"));
      // Unticking everybody has no meaning -- an expense for nobody cannot be
      // split -- and the server would read an empty set as "everyone", so it
      // is refused here rather than silently doing the opposite. Only asked
      // when the picker is open; the toggle cannot express "nobody".
      if (shareAll && !shareAll.checked && !shareBoxes.some((b) => b.checked)) {
        return fail(t("expenses.error.shares"));
      }

      const body = {
        title,
        amount_minor: amountMinor,
        spent_on: spentOn,
        // Always sent explicitly: the server deliberately has no default (see
        // internal/httpapi/expenses.go), so "who paid" is this client's
        // decision. On a shared trip it is the select's value, empty meaning
        // nobody; on a solo trip there is no control and it is always you, and
        // when editing there it is the payer the expense already had rather
        // than whoever opened the form.
        payer_user_id: payerFromForm(form),
        share_user_ids: sharesFromForm(),
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

    // Everybody ticked is sent as an empty list, not as the full list. Both
    // mean "everyone" today, but only the empty one *keeps* meaning it when
    // somebody joins the trip -- so an unrelated edit does not quietly pin an
    // expense that was never pinned.
    function sharesFromForm() {
      if (!shareBoxes.length || (shareAll && shareAll.checked)) return [];
      const checked = shareBoxes.filter((b) => b.checked);
      if (checked.length === shareBoxes.length) return [];
      return checked.map((b) => b.value);
    }

    function payerFromForm(form) {
      if (form.elements.payer) return form.elements.payer.value || null;
      if (isEditing) return editing.payer_user_id;
      return getCurrentUser()?.id || null;
    }

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
