// Double-submit guards.
//
// Nothing in this app used to stop a mutating handler from being re-entered
// while its request was still in flight, which on a slow network means a
// double-tapped Save creates the thing twice. Two controls guarded themselves
// (the password form, the assistant); the other thirty did not. Rather than
// repeat `disabled = true` at every call site and get it subtly wrong in the
// ones that re-render, the ones already disabled for another reason, and the
// ones whose click has to be swallowed either way, all of that lives here.
//
// A guard owns one in-flight flag and a set of controls. While it is running,
// its controls are `disabled` with `aria-busy="true"` and a second invocation
// is dropped. The busy look needs no new CSS - `.btn:disabled` and
// `.icon-btn:disabled` already carry it.
//
// The flag belongs to the guard rather than to any one element, which is the
// whole reason this is an object: the location editor reaches the same save()
// from three places (a button, the Basic info form, the Location card's form),
// and three separate flags would leave two doors open.
//
//   const g = createGuard({ elements: saveBtn });
//   const save = g.wrap(saveImpl);   // hand the *wrapped* one to all three
//
// `elements` may be an element, an array or NodeList of them, or a function
// returning any of those. A function is called at invocation time, so a
// control that a later render() replaced is still found.
export function createGuard({ elements = [], preventDefault = false, stopPropagation = false } = {}) {
  let busy = false;
  const sources = [elements];

  function resolve() {
    const found = new Set();
    for (const source of sources) {
      const value = typeof source === "function" ? source() : source;
      if (!value) continue;
      if (value instanceof Element) found.add(value);
      else for (const el of value) if (el instanceof Element) found.add(el);
    }
    return found;
  }

  async function run(fn, ...args) {
    if (busy) return undefined;
    busy = true;

    // Disabling the control the user just pressed drops focus to <body>, so
    // remember what to put it back on. Only what this call actually disables
    // is tracked: an element that was *already* disabled is left alone
    // entirely, or restoring would hand back an enabled state it never had -
    // the itinerary's move-up/move-down buttons are disabled at the ends of
    // their list and must stay that way.
    const held = [];
    const focused = document.activeElement;
    let refocus = null;

    for (const el of resolve()) {
      if (el.disabled) continue;
      if (focused && (el === focused || el.contains(focused))) refocus = focused;
      el.disabled = true;
      el.setAttribute("aria-busy", "true");
      held.push(el);
    }

    try {
      // Deliberately not caught: every call site already has its own
      // try/catch and its own error paragraph.
      return await fn(...args);
    } finally {
      busy = false;
      for (const el of held) {
        // A success path almost always ends in render()/load()/navigate(),
        // which throws these nodes away - so in practice only a failure
        // re-enables anything.
        if (!el.isConnected) continue;
        el.disabled = false;
        el.removeAttribute("aria-busy");
      }
      // Never steal focus: a render() that placed it deliberately wins, and
      // body is the only place a disable can have left it.
      if (refocus?.isConnected && document.activeElement === document.body) refocus.focus();
    }
  }

  return {
    run,
    // Wraps a handler so it can be used directly as a listener. preventDefault
    // and stopPropagation run *before* the busy check, on the first argument
    // when it looks like an event: the dropped second click still has to be
    // swallowed, or the itinerary's remove-day button folds the <details> it
    // sits in.
    wrap(handler) {
      return (...args) => {
        const event = args[0];
        if (preventDefault) event?.preventDefault?.();
        if (stopPropagation) event?.stopPropagation?.();
        return run(handler, ...args);
      };
    },
    // Adds controls to the busy set after construction, for the case where the
    // guard and the button live in different components: the trip create page
    // places its own Create button outside the form that owns the submit.
    watch(more) {
      sources.push(more);
    },
    get busy() {
      return busy;
    },
  };
}

// A guarded listener with no element to disable, or with one named explicitly.
export function guard(handler, options = {}) {
  return createGuard(options).wrap(handler);
}

// The form case, which is most of them: attaches the submit listener, owns the
// preventDefault, and finds the form's own submit buttons so no call site has
// to name one. `button:not([type])` is included because a bare <button> in a
// form submits it; type="button" ones (a Cancel, a mode switch) are not touched.
export function guardForm(form, handler, { elements, ...rest } = {}) {
  const guard = createGuard({
    elements: elements ?? (() => form.querySelectorAll('button[type="submit"], button:not([type])')),
    preventDefault: true,
    ...rest,
  });
  form.addEventListener("submit", guard.wrap(handler));
  return guard;
}

// The button case: disables the button that was clicked while its handler runs.
export function guardClick(el, handler, { elements, ...rest } = {}) {
  const guard = createGuard({ elements: elements ?? el, ...rest });
  el.addEventListener("click", guard.wrap(handler));
  return guard;
}
