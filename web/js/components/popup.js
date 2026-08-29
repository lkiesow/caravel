// The open/close discipline every popup in this app shares.
//
// Extracted from menu.js in Stage 26 Milestone 4, unchanged, so the filter menu
// could be a second *shape* of popup without becoming a second implementation
// of one. What lives here is the part that is easy to get subtly wrong and that
// nobody should write twice:
//
//   - `hidden` on the dropdown is the single source of truth for open/closed.
//   - aria-expanded on the trigger is kept in sync with it.
//   - `menu__trigger--open` marks an engaged trigger, so a menu you have just
//     opened does not look inert.
//   - The outside-click and Escape listeners are added on open and removed
//     again on close, so a closed popup leaves nothing attached to `document`.
//
// That last point is the reason this is worth sharing: a popup that attaches a
// document listener and forgets to remove it is a leak that only shows up as a
// second popup misbehaving much later.
//
// `onOpen` and `onClose` fire whenever the popup opens or closes, however it
// did. The filter menu uses `onOpen` to reset to its root panel, so a menu that
// was left drilled into a submenu never reopens there. It is deliberately
// `onOpen` rather than `onClose`: resetting on the way out leaves "is it open"
// and "which panel is showing" as two states that can disagree if the popup is
// ever closed by something other than a close() call, whereas resetting on the
// way in makes opening at the root an invariant no sequence of events can
// break.
//
// Escape is handled but deliberately not stopped: menu.js never stopped it
// either, and something further out may also want to act on it. Compare
// suggest-input.js, which *does* stop Escape, because closing its list has to
// take precedence over closing the dialog around it.
export function bindPopup(container, trigger, dropdown, { onOpen, onClose } = {}) {
  function close() {
    if (dropdown.hidden) return;
    dropdown.hidden = true;
    trigger.classList.remove("menu__trigger--open");
    trigger.setAttribute("aria-expanded", "false");
    document.removeEventListener("click", onOutsideClick);
    document.removeEventListener("keydown", onKeydown);
    onClose?.();
  }

  function open() {
    if (!dropdown.hidden) return;
    onOpen?.();
    dropdown.hidden = false;
    trigger.classList.add("menu__trigger--open");
    trigger.setAttribute("aria-expanded", "true");
    document.addEventListener("click", onOutsideClick);
    document.addEventListener("keydown", onKeydown);
  }

  function onOutsideClick(e) {
    if (!container.contains(e.target)) close();
  }

  function onKeydown(e) {
    if (e.key === "Escape") close();
  }

  trigger.addEventListener("click", (e) => {
    e.stopPropagation();
    if (dropdown.hidden) open();
    else close();
  });

  return { open, close, isOpen: () => !dropdown.hidden };
}
