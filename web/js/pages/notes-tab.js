import { api } from "../api.js";
import { guardClick, guardForm } from "../busy.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { renderLoading } from "../components/loading.js";
import { canEdit } from "../trip-role.js";

// The trip notepad: one markdown document per trip, written in a textarea and
// read as rendered HTML.
//
// Two modes over one document, and which one you land in is the point of the
// tab rather than a preference to be remembered. A trip with nothing written
// down opens in the editor, because an empty rendered view is a wall with
// nothing on it and an invitation to click Edit for no reason. A trip with a
// note opens rendered, because reading it is what you came for; the editor is
// one button away.
//
// There is no Edit/Preview toggle inside the editor, unlike the location
// form's notes field. Here Save *is* the switch to the rendered view, so a
// preview would be a second way to see the same thing two seconds earlier.
// (Deferred deliberately -- see plans/todo.md.)
//
// The rendering is the server's: body_html comes back sanitized from
// internal/markdown, the same call the location view page and the markdown
// preview endpoint go through. See the innerHTML assignment below.
export async function renderNotesTab(container, trip) {
  const editable = canEdit(trip);

  renderLoading(container);
  let note;
  try {
    note = await api.get(`/trips/${trip.id}/notes`);
  } catch {
    container.innerHTML = `<p class="trip-notes__error" role="alert"></p>`;
    container.querySelector(".trip-notes__error").textContent = t("tripNotes.loadFailed");
    return;
  }

  // The saved state, replaced only by a save that succeeded. `editing` is the
  // mode; `draft` is what the textarea holds while in it, kept across
  // re-renders so a failed save does not lose what was typed.
  let editing = editable && note.body === "";
  let draft = note.body;
  let error = null;

  function render() {
    container.innerHTML = `
      <div class="trip-notes">
        <p class="trip-notes__error" role="alert" hidden></p>
        ${editing ? editorMarkup() : viewMarkup()}
      </div>
    `;
    translatePage(container);

    if (error) {
      const errorEl = container.querySelector(".trip-notes__error");
      errorEl.textContent = error;
      errorEl.hidden = false;
    }

    if (editing) bindEditor();
    else bindView();
  }

  function viewMarkup() {
    // A viewer, or an editor who has not pressed Edit yet. The empty line is
    // only ever seen by someone who cannot edit: an editor on an empty note is
    // put straight into the textarea instead.
    return `
      ${
        note.body
          ? `<div class="trip-notes__rendered"></div>`
          : `<p class="trip-notes__empty" data-i18n="tripNotes.empty"></p>`
      }
      ${
        editable
          ? `<div class="trip-notes__actions">
               <button type="button" class="btn trip-notes__edit">
                 ${icon("pencil")} <span data-i18n="tripNotes.edit"></span>
               </button>
             </div>`
          : ""
      }
    `;
  }

  function editorMarkup() {
    // Cancel only exists when there is something to go back to. On a note
    // nobody has written, the view it would return to is the empty state the
    // editor was opened instead of, so the button would undo nothing.
    return `
      <form class="trip-notes__form">
        <label class="sr-only" for="trip-notes-body" data-i18n="tripNotes.heading"></label>
        <textarea id="trip-notes-body" name="body" rows="12"
                  data-i18n-placeholder="tripNotes.placeholder"></textarea>
        <div class="trip-notes__actions">
          <button type="submit" class="btn btn-primary">
            ${icon("check")} <span data-i18n="common.save"></span>
          </button>
          ${
            note.body
              ? `<button type="button" class="btn btn-secondary trip-notes__cancel">
                   ${icon("x")} <span data-i18n="common.cancel"></span>
                 </button>`
              : ""
          }
        </div>
      </form>
    `;
  }

  function bindView() {
    const rendered = container.querySelector(".trip-notes__rendered");
    // Trusted: the server sanitized it (bluemonday, in internal/markdown),
    // which is the entire reason the note is rendered server-side at all --
    // the same justification as location-view-page.js.
    if (rendered) rendered.innerHTML = note.body_html;

    const editBtn = container.querySelector(".trip-notes__edit");
    editBtn?.addEventListener("click", () => {
      draft = note.body;
      error = null;
      editing = true;
      render();
      container.querySelector("textarea")?.focus();
    });
  }

  function bindEditor() {
    const form = container.querySelector(".trip-notes__form");
    const textarea = form.querySelector("textarea");
    textarea.value = draft;

    // Grow with the content rather than making the user scroll a fixed box.
    // Same arithmetic as the location form's notes field: everything is
    // border-box, so scrollHeight (content + padding) is 2px short without the
    // borders added back, which is enough to leave a scrollbar.
    function autoGrow() {
      textarea.style.height = "auto";
      const borders = textarea.offsetHeight - textarea.clientHeight;
      textarea.style.height = `${textarea.scrollHeight + borders}px`;
    }
    textarea.addEventListener("input", () => {
      draft = textarea.value;
      autoGrow();
    });
    autoGrow();

    // guardForm, not a hand-rolled submit listener: it owns the preventDefault
    // and applies it *before* the busy check, so the second of a double-tap is
    // swallowed rather than being allowed to submit the form for real.
    // `elements` names every button so Cancel is disabled during a save too.
    guardForm(
      form,
      async () => {
        draft = textarea.value;
        try {
          // Last write wins; the response is the new saved state, including
          // the HTML to show next.
          note = await api.put(`/trips/${trip.id}/notes`, { body: draft });
          error = null;
          // Back to reading -- unless the save cleared the note, in which case
          // there is nothing to read and the editor is where an editor
          // belongs, exactly as on first arrival.
          editing = note.body === "";
          draft = note.body;
          render();
        } catch {
          // Stay in the editor with the text intact: a failed save that threw
          // away what was typed would be worse than the failure.
          error = t("tripNotes.saveFailed");
          render();
        }
      },
      { elements: () => form.querySelectorAll("button") }
    );

    const cancel = form.querySelector(".trip-notes__cancel");
    if (cancel) {
      guardClick(cancel, () => {
        draft = note.body;
        error = null;
        editing = false;
        render();
      });
    }
  }

  render();
}
