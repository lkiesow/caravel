import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { bindSuggestInput } from "./suggest-input.js";

// One id per instance: the suggestion list needs one for aria-controls, and a
// page could in principle render two of these.
let tagFieldSeq = 0;

// The tag editor: a row of chips plus a text box that commits one.
//
// Tags are free text the app never interprets (see migration 0005), so this
// deliberately does not constrain the vocabulary. What it does is make the
// *set* easy to see and to correct, because that is where a plain
// comma-separated text box fails - by the fourth tag you are counting commas,
// and a typo three tags back has to be found by reading.
//
// Committing: Enter or comma. Enter is what people try first; comma is what
// they type out of habit from every other tag field on the web, and swallowing
// it here is cheaper than explaining that it is not a separator. Pasting a
// comma-separated list therefore also works, since paste lands as input and
// every comma in it commits. Blur commits too - a typed-but-uncommitted tag
// that vanished on Save would be the worst outcome available.
//
// Backspace on an empty box removes the last chip, which is the one convention
// every tag field shares and the only way to correct the previous tag without
// reaching for the mouse.
//
// Suggestions come from the trip vocabulary - GET /trips/{id}/tags, fetched
// once - filtered to what is not already on this location. That is the whole
// defence against Museum and museum both existing: the server deduplicates
// case-insensitively within one location but not across the trip, so the way
// spellings converge is people picking the one already there.
//
//   renderTagField(container, { tripId, tags }) -> { readTags, setTags, destroy }
export function renderTagField(container, { tripId, tags = [] }) {
  const listId = `tag-suggest-${++tagFieldSeq}`;
  let current = normalize(tags);
  // Fetched lazily on first focus rather than on render: the editor opens on
  // every location and most edits never touch the tags, so this stays off the
  // page-load path. A failure is silent - suggestions are a convenience and
  // typing a tag out in full always works.
  let vocabulary = null;
  let vocabularyPromise = null;

  container.innerHTML = `
    <div class="tag-field">
      <span class="tag-field__label" data-i18n="location.form.tags"></span>
      <ul class="tag-field__chips"></ul>
      <div class="suggest">
        <input type="text" class="tag-field__input" autocomplete="off"
               data-i18n-placeholder="location.form.tagsPlaceholder"
               data-i18n-aria-label="location.form.tagsAdd" />
        <ul class="suggest__list" id="${listId}" role="listbox" hidden></ul>
      </div>
    </div>
  `;
  translatePage(container);

  const chipList = container.querySelector(".tag-field__chips");
  const input = container.querySelector(".tag-field__input");

  // Trimmed and collapsed the same way the server does, and deduplicated
  // case-insensitively keeping the first spelling. Mirroring tags.Normalize in
  // internal/tags/tags.go on purpose: a chip that disappears on save
  // because the server folded it away would look like data loss.
  function normalize(list) {
    const out = [];
    const seen = new Set();
    for (const raw of list) {
      const tag = String(raw).split(/\s+/).filter(Boolean).join(" ");
      if (!tag) continue;
      const key = tag.toLowerCase();
      if (seen.has(key)) continue;
      seen.add(key);
      out.push(tag);
    }
    return out;
  }

  function renderChips() {
    chipList.replaceChildren(
      ...current.map((tag) => {
        const li = document.createElement("li");
        li.className = "tag-field__chip";
        // textContent, not interpolation: a tag is whatever somebody typed.
        const name = document.createElement("span");
        name.textContent = tag;
        const remove = document.createElement("button");
        remove.type = "button";
        remove.className = "tag-field__remove";
        remove.innerHTML = icon("x");
        // Named per tag rather than a row of identical "Remove" buttons, so
        // the accessible name says which one this is.
        remove.setAttribute("aria-label", t("location.form.tagsRemove", { tag }));
        remove.addEventListener("click", () => {
          current = current.filter((x) => x !== tag);
          renderChips();
          input.focus();
        });
        li.append(name, remove);
        return li;
      })
    );
  }

  function commit(raw) {
    const [tag] = normalize([raw]);
    if (!tag) return false;
    // Case-insensitive, so typing museum when Museum is already a chip is a
    // no-op rather than a second chip that the server would then merge away.
    if (!current.some((x) => x.toLowerCase() === tag.toLowerCase())) {
      current = [...current, tag];
      renderChips();
    }
    return true;
  }

  async function loadVocabulary() {
    if (vocabulary) return vocabulary;
    if (!vocabularyPromise) {
      vocabularyPromise = api
        .get(`/trips/${tripId}/tags`)
        .then((list) => (vocabulary = Array.isArray(list) ? list : []))
        .catch(() => (vocabulary = []));
    }
    return vocabularyPromise;
  }
  input.addEventListener("focus", loadVocabulary, { once: true });

  // minChars 1, unlike the member picker's 2: the vocabulary is small, local
  // and already fetched, so there is no request to spare and a one-letter trip
  // is genuinely useful when the tag is "UK".
  const suggest = bindSuggestInput(input, container.querySelector(".suggest__list"), {
    minChars: 1,
    delay: 0,
    search: async (query) => {
      const all = await loadVocabulary();
      const q = query.toLowerCase();
      const chosen = new Set(current.map((x) => x.toLowerCase()));
      return all
        .filter((tag) => !chosen.has(tag.toLowerCase()) && tag.toLowerCase().includes(q))
        .slice(0, 8)
        .map((tag) => ({ value: tag, label: tag }));
    },
    // onPick is handed the whole { value, label } item, not just the value --
    // this is its first caller in the tree, so that contract had not been
    // exercised before.
    onPick: (item) => {
      commit(item.value);
      input.value = "";
    },
  });

  // Registered after bindSuggestInput, so this runs after its handler and can
  // see what it did.
  //
  // The stopPropagation calls are the load-bearing part. The location form
  // treats Enter in any single-line field as Save (see location-form.js), by a
  // listener on the form that fires as the event bubbles -- so preventDefault
  // alone is not enough: without stopping the event here, adding a tag would
  // also save the page and navigate away from it.
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      // suggest-input already picked a highlighted suggestion, which its
      // onPick turned into a chip. The Enter is spent; it must not also save.
      if (e.defaultPrevented) {
        e.stopPropagation();
        return;
      }
      // Nothing typed: the Enter is not ours, and falls through to the form,
      // where it means Save. That is deliberate -- Enter in an empty tag box
      // should do what Enter in an empty field anywhere else on this form
      // does.
      if (!input.value.trim()) return;
      e.preventDefault();
      e.stopPropagation();
      commit(input.value);
      input.value = "";
      return;
    }
    if (e.key === "Backspace" && input.value === "" && current.length) {
      current = current.slice(0, -1);
      renderChips();
    }
  });

  input.addEventListener("input", () => {
    if (!input.value.includes(",")) return;
    // Every complete piece commits; whatever follows the last comma stays in
    // the box as the tag still being typed. This is what makes pasting a
    // comma-separated list work.
    const parts = input.value.split(",");
    input.value = parts.pop();
    parts.forEach(commit);
  });

  input.addEventListener("blur", () => {
    if (commit(input.value)) input.value = "";
  });

  renderChips();

  return {
    readTags: () => [...current],
    setTags(list) {
      current = normalize(list);
      renderChips();
    },
    destroy() {
      suggest.destroy();
    },
  };
}
