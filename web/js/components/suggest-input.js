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
// Returns { destroy() }, which the caller must call before it replaces the DOM
// these nodes live in: a pending debounce timer or an open list's document
// listener would otherwise outlive the elements they point at.
export function bindSuggestInput(input, listEl, { search, minChars = 2, delay = 200, onPick } = {}) {
  let items = [];
  let activeIndex = -1;
  let timer = null;
  let lastQuery = null;
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
    if (!listEl.hidden) return;
    listEl.hidden = false;
    input.setAttribute("aria-expanded", "true");
    document.addEventListener("pointerdown", onOutside);
  }

  // Closing empties the list rather than only hiding it. A hidden list still
  // holding the previous query's people is a set of stale answers waiting to be
  // shown again by the next thing that opens it, and it reads as suggestions to
  // anything walking the DOM.
  function close() {
    activeIndex = -1;
    items = [];
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
    // Read before close(), which empties `items`.
    const item = items[i];
    if (!item) return;
    input.value = item.value;
    close();
    input.focus();
    onPick?.(item);
  }

  function onOutside(event) {
    // The tab this lives in can be torn out from under an open list without
    // anyone calling destroy() - a tab switch just re-renders the container.
    if (!input.isConnected) return destroy();
    if (event.target !== input && !listEl.contains(event.target)) close();
  }

  input.addEventListener("input", () => {
    const query = input.value.trim();
    // Debounced: this fires per keystroke and each one is a request.
    clearTimeout(timer);
    if (query.length < minChars) {
      close();
      return;
    }
    if (query === lastQuery) return;
    timer = setTimeout(async () => {
      lastQuery = query;
      const mine = ++seq;
      let found;
      try {
        found = await search(query);
      } catch {
        // Silent, deliberately: the field is fully usable by typing a name out,
        // so an error banner here would report a problem the user does not have.
        return;
      }
      if (mine !== seq || !input.isConnected || input.value.trim() !== query) return;
      items = found;
      if (!items.length) {
        // No "no matches" row. The field works when typed in full, and the
        // lookup is silent about its failures - an empty list would be the one
        // place it spoke up.
        close();
        return;
      }
      renderOptions();
      activeIndex = -1;
      input.removeAttribute("aria-activedescendant");
      open();
    }, delay);
  });

  input.addEventListener("keydown", (event) => {
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
