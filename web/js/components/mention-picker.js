import { api } from "../api.js";
import { t } from "../i18n.js";
import { bindSuggestInput } from "./suggest-input.js";
import { caretCoords } from "./caret-coords.js";

// Typing @ in a markdown textarea offers the trip's locations; picking one
// inserts a plain markdown link to that location page at the caret.
//
// What lands in the text is an ordinary link -- [Kex Hostel](/trips/T/locations/I)
// -- not a reference resolved at render time. That keeps the note portable
// markdown and leaves internal/markdown and its sanitizer completely out of
// this: bluemonday's UGCPolicy already allows relative URLs, so the link
// survives rendering with no server change at all. The cost is that renaming a
// location afterwards leaves the old text behind, which is in plans/todo.md.
//
// The combobox itself is components/suggest-input.js, which this configures
// rather than reimplements: readQuery says where the @run is, applyPick
// replaces it, onOpen puts the list next to the caret instead of under the
// field.

// `@` at the start of the text or after whitespace or an opening/closing
// punctuation mark, so an email address in a note never opens this but
// "(@kex" or "„@kex" does, then a run containing no second @ and no newline.
// An allow-list rather than "anything but a letter": a slash is deliberately
// missing, or every pasted mastodon.social/@user URL would open the list, and
// so are - and +, which do occur in the local part of an address. Only the
// character before the @ matters -- what follows the caret is never read, so
// typing @ between a pair of brackets already works.
const TRIGGER = /(?:^|[\s()[\]{}<,.;:"„“”'‚‘’«»*_~])@([^@\n]{0,40})$/;

// Interior spaces are allowed because location titles are phrases -- "Blue
// Lagoon" has to be reachable by typing it -- but a whole sentence after a
// stray @ is not a query.
const MAX_SPACES = 2;

// CommonMark link text: a bare [ or ] would close or nest the label and break
// the link into literal text. The backslash itself goes first, or the escapes
// would themselves be escaped.
const escapeLinkText = (s) => s.trim().replace(/([\\[\]])/g, "\\$1");

export function bindMentionPicker(textarea, listEl, { tripId }) {
  let locationsPromise = null;

  // Lazily, once, on first focus -- the same shape as tag-field.js's
  // loadVocabulary. A failure resolves empty: the picker is a convenience on
  // top of a textarea that works without it, so it has nothing to report.
  function loadLocations() {
    if (!locationsPromise) {
      locationsPromise = api
        .get(`/trips/${tripId}/locations`)
        .then((list) => (Array.isArray(list) ? list : []))
        .catch(() => []);
    }
    return locationsPromise;
  }
  textarea.addEventListener("focus", loadLocations, { once: true });

  function readQuery(el) {
    // A selection is not a query, and replacing one would be a surprise.
    if (el.selectionStart !== el.selectionEnd) return null;
    const match = TRIGGER.exec(el.value.slice(0, el.selectionStart));
    if (!match) return null;
    const query = match[1];
    if (query.startsWith(" ")) return null;
    if ((query.match(/ /g) ?? []).length > MAX_SPACES) return null;
    return {
      query,
      // The index of the @ itself, which the insertion replaces along with
      // everything typed after it.
      start: el.selectionStart - query.length - 1,
      end: el.selectionStart,
    };
  }

  function applyPick(el, item, range) {
    const link = `[${escapeLinkText(item.label)}](/trips/${tripId}/locations/${item.value})`;
    el.focus();
    el.setSelectionRange(range.start, range.end);
    // execCommand is deprecated but is still the only way to put text into a
    // textarea that keeps the browser's undo stack -- and it fires a real
    // input event, which the note editor needs: its handler is what keeps the
    // draft in step. Assigning .value throws the undo history away, so that is
    // the fallback, with the event dispatched by hand.
    let inserted = false;
    try {
      inserted = document.execCommand("insertText", false, link);
    } catch {
      inserted = false;
    }
    if (inserted) return;
    el.value = el.value.slice(0, range.start) + link + el.value.slice(range.end);
    const caret = range.start + link.length;
    el.setSelectionRange(caret, caret);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  }

  // The list is absolutely positioned inside the .suggest wrapper, and the
  // stylesheet's `top: 100%` is no use here: these textareas grow to the full
  // height of what is written in them, so the bottom of the field can be a
  // screenful below the word being typed. Only the vertical offset is
  // computed -- the list keeps the field's full width, which is what it wants
  // at 324px anyway, and which is also why there is no RTL case to get wrong.
  function place(el) {
    const { top, lineHeight } = caretCoords(el);
    const gap = 4;
    const anchor = el.offsetTop + top;
    const viewport = window.visualViewport?.height ?? window.innerHeight;
    const caretTop = el.getBoundingClientRect().top + top;
    const height = listEl.offsetHeight;
    // Flip above the caret line when the list would not fit below it. On a
    // phone with the keyboard up that is most lines.
    const fitsBelow = caretTop + lineHeight + gap + height <= viewport;
    const above = anchor - height - gap;
    listEl.style.top = !fitsBelow && above >= 0 ? `${above}px` : `${anchor + lineHeight + gap}px`;
  }

  const suggest = bindSuggestInput(textarea, listEl, {
    readQuery,
    applyPick,
    // No ArrowDown on a phone keyboard, so without this the keyboard path
    // would be desktop-only. Enter therefore picks whenever the list is open;
    // Escape, a space and a backspace each close it in one keystroke.
    autoActivateFirst: true,
    onOpen: place,
    // Local data, already fetched: nothing to spare by waiting, and one letter
    // is a useful query. Same reasoning as the tag field's.
    minChars: 1,
    delay: 0,
    search: async (query) => {
      const all = await loadLocations();
      const q = query.trim().toLowerCase();
      const hits = all.filter((location) => location.title.toLowerCase().includes(q));
      // Prefix matches lead: typing "ke" for Kex Hostel should not put it third
      // behind places with "ke" in the middle of a word.
      hits.sort(
        (a, b) =>
          Number(b.title.toLowerCase().startsWith(q)) - Number(a.title.toLowerCase().startsWith(q)),
      );
      return hits.slice(0, 8).map((location) => ({
        value: location.id,
        label: location.title,
        hint: t(`location.category.${location.category}`),
      }));
    },
  });

  // Closed rather than repositioned: the Android soft keyboard opening fires
  // resize, and a popup chasing a moving viewport looks broken in a way a
  // closed one does not.
  const onViewportChange = () => suggest.close();
  window.addEventListener("resize", onViewportChange);
  window.addEventListener("orientationchange", onViewportChange);

  return {
    close: suggest.close,
    destroy() {
      window.removeEventListener("resize", onViewportChange);
      window.removeEventListener("orientationchange", onViewportChange);
      suggest.destroy();
    },
  };
}
