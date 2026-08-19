import { t, translatePage } from "../i18n.js";

const CATEGORIES = ["site", "stay", "transport"];

// Renders the Basic info fields of a location into `container`. Pass an
// existing item to prefill them, or null to start empty.
//
// This form does not save anything and owns no button. The location editor
// commits every card - basic info, coordinates, links, dates - in one
// request (see location-editor-page.js), so the submit control belongs at
// the bottom of all of them and the request belongs to the page. What this
// component exposes instead is `readValues()`, `showError()` and the
// `onSubmit` hook that fires when the user presses Enter in a field, so
// Enter and the page's Save button do the same thing.
export function renderItemForm(container, item, { onSubmit }) {
  container.innerHTML = `
    <form class="item-form" novalidate>
      <p class="item-form__error" role="alert" hidden></p>
      <label>
        <span data-i18n="location.form.title"></span>
        <input type="text" name="title" required />
      </label>
      <label>
        <span data-i18n="location.form.category"></span>
        <select name="category">
          ${CATEGORIES.map((c) => `<option value="${c}">${t(`item.category.${c}`)}</option>`).join("")}
        </select>
      </label>
      <label>
        <span data-i18n="location.form.type"></span>
        <input type="text" name="type" data-i18n-placeholder="location.form.typePlaceholder" />
      </label>
      <label>
        <span data-i18n="location.form.notes"></span>
        <textarea name="notes" rows="6"></textarea>
      </label>
      <label class="item-form__checkbox">
        <input type="checkbox" name="showOnMap" checked />
        <span data-i18n="location.form.showOnMap"></span>
      </label>
    </form>
  `;
  translatePage(container);

  const form = container.querySelector("form");
  const errorEl = container.querySelector(".item-form__error");

  if (item) {
    form.title.value = item.title;
    form.category.value = item.category;
    form.type.value = item.type ?? "";
    form.notes.value = item.notes ?? "";
    form.showOnMap.checked = item.show_on_map;
  }

  // Notes grow with their content rather than making the user scroll a
  // 6-row box or drag it bigger every time. min-height in the CSS sets the
  // floor; clearing the height first lets it shrink again on delete. Run
  // once after prefill so an existing long note opens fully expanded.
  function autoGrowNotes() {
    const notes = form.notes;
    notes.style.height = "auto";
    // Everything is border-box (see base.css), so the borders have to be
    // added back: scrollHeight covers content + padding only, and leaving
    // them out shorts the box by 2px, which is enough to keep a scrollbar.
    const borders = notes.offsetHeight - notes.clientHeight;
    notes.style.height = `${notes.scrollHeight + borders}px`;
  }
  form.notes.addEventListener("input", autoGrowNotes);
  autoGrowNotes();

  // Enter in any single-line field means "save the page", the same as the
  // Save button at the bottom.
  //
  // Both listeners are needed. The submit one is the safety net: a form with
  // exactly one field that blocks implicit submission *does* submit natively
  // on Enter even with no submit button, which would reload the whole app, so
  // it must be caught even though this form currently has several fields. The
  // keydown one is what actually fires today: with several such fields and no
  // submit button, the implicit submission algorithm does nothing at all, so
  // without it Enter would silently do nothing (verified in Firefox).
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    onSubmit?.();
  });
  form.addEventListener("keydown", (e) => {
    // Not in the notes textarea, where Enter is a newline.
    if (e.key !== "Enter" || e.target.tagName === "TEXTAREA") return;
    e.preventDefault();
    onSubmit?.();
  });

  return {
    readValues: () => ({
      category: form.category.value,
      type: form.type.value,
      title: form.title.value,
      notes: form.notes.value || null,
      show_on_map: form.showOnMap.checked,
    }),
    // The page reports save failures here, in the card the required fields
    // live in, rather than in a dialog at the bottom of the page.
    showError: (message) => {
      errorEl.textContent = message || t("common.error");
      errorEl.hidden = false;
      errorEl.scrollIntoView({ block: "nearest" });
    },
    clearError: () => {
      errorEl.hidden = true;
    },
  };
}
