// A text field that offers matching suggestions as you type: the app's one
// combobox.
//
// This started life as a native <datalist>, which was the right first choice -
// the browser owns the keyboard handling, the screen-reader announcement and
// the touch behaviour, none of which a hand-rolled popup gets for free. It was
// dropped in favour of this because Firefox for Android never renders the
// datalist popup at all, so the suggestions were dead on exactly the platform
// where typing a username out in full hurts most. The field still works when
// typed in full on every browser; this is a convenience on top of that, which
// is why a failed lookup stays silent.
//
// Not built on components/menu.js, which is the app's other popup: that one is
// a trigger button plus a fixed list, role="menu"/menuitemradio, with focus
// moving into the popup. A combobox keeps focus in the text field and points at
// the active option with aria-activedescendant, rebuilds its items on every
// keystroke, and has no trigger - a mode switch on renderMenu covering all of
// that would cost more than this file does. What is copied is menu.js's open
// and close discipline: `hidden` and aria-expanded kept in sync, document
// listeners attached on open and removed again on close, so a closed list
// leaves nothing behind.
//
//   bindSuggestInput(input, listEl, { search, minChars, delay, onPick })
//
// `search(query)` resolves to [{ value, label, hint }] - `value` is what lands
// in the field, `label` the human name, `hint` the secondary line. It is called
// debounced, and its rejections are swallowed.
//
// By default the *whole field* is the query and a pick replaces the whole
// field, which is what a username or a tag wants. Two options lift that, so
// the same component can drive a picker over a run of text inside a textarea
// (see components/mention-picker.js):
//
//   readQuery(el) -> { query, start, end } | null
//     Where the query is. Returning null means "there is no query here" and
//     closes the list - that is how a trigger-character picker says the caret
//     has left the run it was completing.
//   applyPick(el, item, range) -> void
//     Puts the chosen item into the field. `range` is the readQuery result the
//     open list was built from, so an insertion knows what to replace.
//
// Plus `autoActivateFirst`, which highlights the first option as soon as the
// list opens so Enter picks it without an ArrowDown first (there is no arrow
// key on a phone keyboard), and `onOpen(el, listEl)`, called after the list is
// visible and populated - the hook a caller needs to position it somewhere
// other than where the stylesheet puts it.
//
// Returns { destroy() }, which the caller must call before it replaces the DOM
// these nodes live in: a pending debounce timer or an open list's document
// listener would otherwise outlive the elements they point at.
export function bindSuggestInput(input, listEl, opts = {}) {
  const { search, minChars = 2, delay = 200, onPick, autoActivateFirst = false, onOpen } = opts;

  // Whether the caller reads part of the field rather than all of it. Gates the
  // selectionchange listener below, so the two whole-field callers attach
  // exactly the listeners they attached before these options existed.
  const partial = Boolean(opts.readQuery);
  const readQuery =
    opts.readQuery ?? ((el) => ({ query: el.value.trim(), start: 0, end: el.value.length }));
  const applyPick =
    opts.applyPick ??
    ((el, item) => {
      el.value = item.value;
    });

  let items = [];
  let activeIndex = -1;
  let timer = null;
  let lastQuery = null;
  // The readQuery result the open list was built from, handed to applyPick.
  let range = null;
  // True between compositionstart and compositionend: an IME is mid-word and
  // neither its keystrokes nor its intermediate text are ours to act on.
  let composing = false;
  // Guards against a slower earlier lookup landing after a faster later one and
  // overwriting its results.
  let seq = 0;

  input.setAttribute("role", "combobox");
  input.setAttribute("aria-autocomplete", "list");
  input.setAttribute("aria-expanded", "false");
  input.setAttribute("aria-controls", listEl.id);
  listEl.hidden = true;

  const optionId = (i) => `${listEl.id}-opt-${i}`;

  function renderOptions() {
    listEl.replaceChildren(
      ...items.map((item, i) => {
        const li = document.createElement("li");
        li.className = "suggest__option";
        li.id = optionId(i);
        li.setAttribute("role", "option");
        li.setAttribute("aria-selected", "false");
        li.dataset.index = String(i);
        const label = document.createElement("span");
        // textContent throughout: names come from other people's accounts, and
        // building these by interpolation would need an escaping dance that
        // this avoids entirely (menu.js makes the same choice).
        label.textContent = item.label;
        li.appendChild(label);
        if (item.hint) {
          const hint = document.createElement("span");
          hint.className = "suggest__hint";
          hint.textContent = item.hint;
          li.appendChild(hint);
        }
        return li;
      }),
    );
  }

  function open() {
    if (listEl.hidden) {
      listEl.hidden = false;
      input.setAttribute("aria-expanded", "true");
      document.addEventListener("pointerdown", onOutside);
      if (partial) document.addEventListener("selectionchange", onSelection);
    }
    // Called even when the list was already open: a new batch of results is a
    // new height, and whoever placed it needs to place it again.
    onOpen?.(input, listEl);
  }

  // Closing empties the list rather than only hiding it. A hidden list still
  // holding the previous query's people is a set of stale answers waiting to be
  // shown again by the next thing that opens it, and it reads as suggestions to
  // anything walking the DOM.
  function close() {
    activeIndex = -1;
    items = [];
    range = null;
    // Forget the query too, so the same text typed again after an Escape asks
    // once more instead of being suppressed as a repeat. One redundant request
    // in that case is better than a field that has quietly stopped suggesting.
    lastQuery = null;
    input.removeAttribute("aria-activedescendant");
    listEl.replaceChildren();
    if (listEl.hidden) return;
    listEl.hidden = true;
    input.setAttribute("aria-expanded", "false");
    document.removeEventListener("pointerdown", onOutside);
    document.removeEventListener("selectionchange", onSelection);
  }

  function setActive(i) {
    const options = listEl.children;
    if (activeIndex >= 0 && options[activeIndex]) {
      options[activeIndex].classList.remove("suggest__option--active");
      options[activeIndex].setAttribute("aria-selected", "false");
    }
    activeIndex = i;
    const option = options[i];
    if (!option) {
      input.removeAttribute("aria-activedescendant");
      return;
    }
    option.classList.add("suggest__option--active");
    option.setAttribute("aria-selected", "true");
    input.setAttribute("aria-activedescendant", option.id);
    option.scrollIntoView({ block: "nearest" });
  }

  function pick(i) {
    // Read before close(), which empties `items` and forgets the range.
    const item = items[i];
    if (!item) return;
    const where = range;
    // Closed *before* the field is written to, not after: an insertion that
    // keeps the undo stack fires a real input event, which would otherwise
    // re-enter the handler below while this list was still live.
    close();
    applyPick(input, item, where);
    input.focus();
    onPick?.(item);
  }

  function onOutside(event) {
    // The tab this lives in can be torn out from under an open list without
    // anyone calling destroy() - a tab switch just re-renders the container.
    if (!input.isConnected) return destroy();
    if (event.target !== input && !listEl.contains(event.target)) close();
  }

  // Only attached in partial mode. Moving the caret out of the run being
  // completed - by clicking elsewhere in the text, or by an arrow key - has to
  // close the list, and neither of those fires an input event.
  function onSelection() {
    if (listEl.hidden) return;
    if (!input.isConnected) return destroy();
    const now = readQuery(input);
    if (!now || !range || now.start !== range.start) close();
  }

  input.addEventListener("input", () => {
    // Mid-composition text is the IME's scratchpad, not something to search on.
    if (composing) return;
    const found = readQuery(input);
    // Debounced: this fires per keystroke and each one is a request.
    clearTimeout(timer);
    if (!found || found.query.length < minChars) {
      close();
      return;
    }
    // Keyed by position as well as text: two identical runs in one textarea are
    // two different questions, and a text-only key would silence the second.
    const key = `${found.start}:${found.query}`;
    if (key === lastQuery) return;
    timer = setTimeout(async () => {
      lastQuery = key;
      const mine = ++seq;
      let list;
      try {
        list = await search(found.query);
      } catch {
        // Silent, deliberately: the field is fully usable by typing a name out,
        // so an error banner here would report a problem the user does not have.
        return;
      }
      // Re-read rather than re-compare the field's text: the caret may have
      // moved while the lookup was out, and the range a pick applies into has
      // to be the current one.
      const now = input.isConnected ? readQuery(input) : null;
      if (mine !== seq || !now || now.query !== found.query) return;
      items = list;
      if (!items.length) {
        // No "no matches" row. The field works when typed in full, and the
        // lookup is silent about its failures - an empty list would be the one
        // place it spoke up.
        close();
        return;
      }
      range = now;
      renderOptions();
      activeIndex = -1;
      input.removeAttribute("aria-activedescendant");
      open();
      if (autoActivateFirst) setActive(0);
    }, delay);
  });

  // An IME swallows the keys it is composing with. Without these, Enter picking
  // a Japanese candidate would also pick a suggestion.
  input.addEventListener("compositionstart", () => {
    composing = true;
  });
  input.addEventListener("compositionend", () => {
    composing = false;
    // Run the pass that was skipped, now that the text is final.
    input.dispatchEvent(new Event("input", { bubbles: false }));
  });

  input.addEventListener("keydown", (event) => {
    // isComposing is the standard signal; keyCode 229 is the older one some
    // Android keyboards still send instead.
    if (event.isComposing || event.keyCode === 229) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      if (listEl.hidden || !items.length) return;
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      // Nothing active yet: down starts at the top, up starts at the bottom.
      if (activeIndex < 0) setActive(step > 0 ? 0 : items.length - 1);
      else setActive((activeIndex + step + items.length) % items.length);
      return;
    }
    if (event.key === "Enter") {
      // Only when the list is open on a chosen option. Otherwise Enter is the
      // form's, which is what it was before this component existed.
      if (listEl.hidden || activeIndex < 0) return;
      event.preventDefault();
      pick(activeIndex);
      return;
    }
    if (event.key === "Escape") {
      if (listEl.hidden) return;
      // Stopped, so Escape closes the list rather than travelling on to
      // whatever else listens for it (a dialog, a menu).
      event.stopPropagation();
      close();
      return;
    }
    if (event.key === "Tab") close();
  });

  // Without this the input blurs before the tap resolves, which on touch is the
  // usual reason a hand-rolled suggestion list looks like it does nothing.
  listEl.addEventListener("pointerdown", (event) => event.preventDefault());
  listEl.addEventListener("click", (event) => {
    const option = event.target.closest("[role='option']");
    if (option) pick(Number(option.dataset.index));
  });

  function destroy() {
    clearTimeout(timer);
    // Bumped so an in-flight lookup cannot repopulate a detached list.
    seq++;
    close();
  }

  return { destroy, close };
}
