import { t } from "../i18n.js";
import { icon } from "../icon.js";

// In-app modal dialogs, replacing window.confirm()/window.alert().
//
// The native ones were the only part of the UI the app didn't draw: on a
// phone they render as a "localhost:8080 says" system sheet, visually
// disconnected from everything around them, and their text can't be styled
// or laid out. They're also synchronous, which is why every caller here had
// to be shaped around a blocking boolean.
//
// Built on <dialog> + showModal(), which gives three things for free that a
// hand-rolled div would have to reimplement: the focus trap, Escape to
// dismiss, and the top layer (no z-index fights with the map or the
// dropdowns). The dialog is created per call and removed on close - there's
// no long-lived instance to keep in sync, and at most one is ever open.
//
// Both helpers return a promise, so callers read as
//   if (!(await confirmDialog(...))) return;
// which is the same shape the window.confirm() calls had.

// Resolves true if the user confirmed, false on Cancel or Escape.
//
// `messageKey` is an i18n key for the whole prompt - the existing
// *.deleteConfirm strings are already self-contained sentences ("Delete this
// trip? This cannot be undone."), so there's no separate title, and the
// message doubles as the dialog's accessible name via aria-labelledby.
export function confirmDialog({ messageKey, confirmKey = "common.delete", danger = true }) {
  return open({
    messageKey,
    buttons: [
      // Cancel first, so it's what <dialog>'s autofocus lands on and what
      // Enter triggers. Deliberately the reverse of the app's usual
      // primary-first order (see .editor-actions): every caller of this is
      // destructive and irreversible, so the safe choice is the default one.
      { labelKey: "common.cancel", value: "cancel", className: "btn-secondary", iconName: "x" },
      { labelKey: confirmKey, value: "confirm", className: danger ? "btn-danger" : "btn-primary", iconName: danger ? "trash-2" : "check" },
    ],
  }).then((value) => value === "confirm");
}

// A message with a single dismiss button. Resolves when it's closed.
export function alertDialog({ messageKey, message }) {
  return open({
    messageKey,
    message,
    buttons: [{ labelKey: "item.detail.close", value: "close", className: "btn-primary", iconName: "check" }],
  }).then(() => undefined);
}

let nextId = 0;

// `message` (a ready string) wins over `messageKey` when both are given, for
// the rare caller that has to interpolate.
function open({ messageKey, message, buttons }) {
  const dialog = document.createElement("dialog");
  dialog.className = "dialog";
  const messageId = `dialog-message-${nextId++}`;
  dialog.setAttribute("aria-labelledby", messageId);

  const text = document.createElement("p");
  text.className = "dialog__message";
  text.id = messageId;
  text.textContent = message ?? t(messageKey);
  dialog.appendChild(text);

  const actions = document.createElement("div");
  actions.className = "dialog__actions";
  for (const button of buttons) {
    const el = document.createElement("button");
    el.type = "button";
    el.className = `btn ${button.className}`;
    el.value = button.value;
    el.innerHTML = `${icon(button.iconName)} <span></span>`;
    // textContent on the span rather than into the template, so a translated
    // label can never be parsed as markup.
    el.querySelector("span").textContent = t(button.labelKey);
    el.addEventListener("click", () => dialog.close(button.value));
    actions.appendChild(el);
  }
  dialog.appendChild(actions);

  document.body.appendChild(dialog);

  return new Promise((resolve) => {
    // One "close" event covers every exit: a button, or Escape (which the
    // browser turns into cancel + close with an empty returnValue).
    dialog.addEventListener("close", () => {
      const value = dialog.returnValue;
      dialog.remove();
      resolve(value);
    });
    dialog.showModal();
  });
}
