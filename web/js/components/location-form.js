import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";

const CATEGORIES = ["site", "stay", "transport"];

// One id per instance, because a <label for> needs one and this component could
// in principle be rendered twice on a page.
let notesFieldSeq = 0;

// Renders the Basic info fields of a location into `container`. Pass an
// existing item to prefill them, or null to start empty.
//
// The empty `[data-assist-field]` divs are where the assistant puts a
// suggestion for that field. They live here, directly under the control each
// one is about, rather than in a list of their own: a suggested title three
// cards away from the title box cannot be compared with what is in the box,
// which is the whole job. assist-panel.js finds them by attribute and owns
// everything that goes inside them.
//
// This form does not save anything and owns no button. The location editor
// commits every card - basic info, coordinates, links, dates - in one
// request (see location-editor-page.js), so the submit control belongs at
// the bottom of all of them and the request belongs to the page. What this
// component exposes instead is `readValues()`, `showError()` and the
// `onSubmit` hook that fires when the user presses Enter in a field, so
// Enter and the page's Save button do the same thing.
export function renderItemForm(container, item, { onSubmit }) {
  const notesId = `notes-${++notesFieldSeq}`;
  container.innerHTML = `
    <form class="item-form" novalidate>
      <p class="item-form__error" role="alert" hidden></p>
      <label>
        <span data-i18n="location.form.title"></span>
        <input type="text" name="title" required />
      </label>
      <div data-assist-field="title"></div>
      <label>
        <span data-i18n="location.form.category"></span>
        <select name="category">
          ${CATEGORIES.map((c) => `<option value="${c}">${t(`item.category.${c}`)}</option>`).join("")}
        </select>
      </label>
      <div data-assist-field="category"></div>
      <label>
        <span data-i18n="location.form.type"></span>
        <input type="text" name="type" data-i18n-placeholder="location.form.typePlaceholder" />
      </label>
      <div data-assist-field="type"></div>
      <div class="notes-field">
        <div class="notes-field__header">
          <label for="${notesId}" data-i18n="location.form.notes"></label>
          <div class="notes-field__toggle" role="group" data-i18n-aria-label="location.form.notesMode">
            <button type="button" class="notes-field__tab" data-mode="edit" aria-pressed="true">
              ${icon("pencil")} <span data-i18n="location.form.notesEdit"></span>
            </button>
            <button type="button" class="notes-field__tab" data-mode="preview" aria-pressed="false">
              ${icon("eye")} <span data-i18n="location.form.notesPreview"></span>
            </button>
          </div>
        </div>
        <textarea id="${notesId}" name="notes" rows="6"></textarea>
        <div class="notes-field__preview" hidden></div>
        <p class="notes-field__empty" data-i18n="location.form.notesEmpty" hidden></p>
      </div>
      <div data-assist-field="notes"></div>
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

  // A <select> is never empty, so on a new location the category reads as
  // "site" before anybody has chosen anything. Left alone, that makes every
  // category suggestion look like it is about to replace a real decision --
  // and a warning that cries wolf on every new location is a warning people
  // stop reading. So an untouched select on a new location reports itself as
  // unset; the moment it is changed, or when editing something saved, it is a
  // choice like any other.
  let categoryChosen = Boolean(item);
  form.category.addEventListener("change", () => {
    categoryChosen = true;
  });

  // --- Edit / Preview -------------------------------------------------------
  //
  // Notes are markdown, and before Stage 15 Milestone 3 the only way to see
  // what they would look like was to save and leave the editor. The preview is
  // rendered by the server (POST /api/markdown/preview, which goes through the
  // same renderNotesHTML the item payload uses), so what it shows is what the
  // view page will show - a client-side renderer would be a second markdown
  // implementation free to disagree with the first.
  //
  // One request per switch into Preview, not one per keystroke, and skipped
  // entirely when the text has not changed since the last one. The textarea
  // keeps its value while hidden, so readValues() does not care which mode is
  // showing.
  const previewEl = container.querySelector(".notes-field__preview");
  const previewEmptyEl = container.querySelector(".notes-field__empty");
  const tabs = [...container.querySelectorAll(".notes-field__tab")];
  let previewedSource = null;

  function setMode(mode) {
    const preview = mode === "preview";
    for (const tab of tabs) tab.setAttribute("aria-pressed", String(tab.dataset.mode === mode));
    form.notes.hidden = preview;
    previewEl.hidden = !preview || !previewEl.innerHTML;
    previewEmptyEl.hidden = !preview || !!previewEl.innerHTML;
    // Coming back from a preview, the textarea has been display:none and its
    // scrollHeight was 0 while hidden, so the height it grew to is stale.
    if (!preview) autoGrowNotes();
  }

  async function showPreview() {
    const source = form.notes.value;
    if (!source.trim()) {
      previewEl.innerHTML = "";
      previewedSource = source;
      setMode("preview");
      return;
    }
    if (source === previewedSource) {
      setMode("preview");
      return;
    }
    try {
      // Trusted: the server sanitized it (bluemonday, in internal/markdown),
      // which is the entire reason the preview is a round trip.
      const { html } = await api.post("/markdown/preview", { markdown: source });
      previewEl.innerHTML = html;
      previewedSource = source;
      setMode("preview");
    } catch (err) {
      // Back to Edit rather than showing an empty box that looks like a note
      // which renders to nothing.
      setMode("edit");
      showError(err.body?.error);
    }
  }

  for (const tab of tabs) {
    tab.addEventListener("click", () => {
      if (tab.dataset.mode === "preview") showPreview();
      else setMode("edit");
    });
  }

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
    // Not in the notes textarea, where Enter is a newline - and not on a
    // button, where Enter means "press this one". Without the second
    // exclusion, Enter on the Preview tab would save the whole page instead of
    // switching mode (the tabs arrived in Stage 15 Milestone 3; this form had
    // no buttons at all before that).
    if (e.key !== "Enter" || e.target.tagName === "TEXTAREA" || e.target.tagName === "BUTTON") return;
    e.preventDefault();
    onSubmit?.();
  });

  // Hoisted out of the returned object: the preview's failure path needs it
  // too, and two copies of "put this message in the card's error line" is how
  // they drift.
  function showError(message) {
    errorEl.textContent = message || t("common.error");
    errorEl.hidden = false;
    errorEl.scrollIntoView({ block: "nearest" });
  }

  return {
    // No show_on_map here: it gates whether the item's *coordinates* put it
    // on the map, so the checkbox lives in the Location card next to them
    // (Stage 09 Milestone 3) and the page reads it from there.
    readValues: () => ({
      category: form.category.value,
      type: form.type.value,
      title: form.title.value,
      notes: form.notes.value || null,
    }),
    // Whether the category is a real choice rather than the select's default.
    // Only the assistant asks; readValues() always reports the value, because
    // saving a new location does have to pick one.
    isCategoryChosen: () => categoryChosen,
    // Write access, for the assistant panel: an accepted suggestion goes into
    // the form exactly as if it had been typed, and Save is still the only
    // thing that commits it. Only the fields this form owns; the address and
    // coordinates live in the Location card and the page applies those.
    setValues(partial) {
      for (const [name, value] of Object.entries(partial)) {
        const field = form.elements[name];
        if (!field) continue;
        field.value = value ?? "";
      }
      // The notes box sizes itself to its content, and a pasted-in paragraph
      // would otherwise open as a 6-row box with a scrollbar.
      if ("notes" in partial) autoGrowNotes();
      // Accepting a suggested category is a choice, so later runs should treat
      // it as one.
      if ("category" in partial) categoryChosen = true;
    },
    // The page reports save failures here, in the card the required fields
    // live in, rather than in a dialog at the bottom of the page.
    showError,
    clearError: () => {
      errorEl.hidden = true;
    },
  };
}
