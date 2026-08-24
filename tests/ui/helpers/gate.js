// Holds a request open so a spec can look at the app mid-flight.
//
// The bug this exists for is a race: on a slow network a second press lands
// while the first request is still going, and the write happens twice. A fixed
// delay would be a race of its own -- fast enough to be worth having and the
// assertions run before the click, slow enough to be safe and the suite crawls.
// So the request is *held*, Node-side, until the spec says go: everything
// between holdRoute() and release() happens with the app provably waiting.
//
// Register this AFTER login(page). scenarios.js's blockExternalRequests
// installs a catch-all page.route("**/*"), and Playwright runs route handlers
// in reverse registration order, so the later, more specific one wins;
// route.fallback() hands anything this does not want back down to it.
export async function holdRoute(page, pattern, { method = "POST", status } = {}) {
  const seen = [];
  let open;
  const gate = new Promise((resolve) => (open = resolve));

  await page.route(pattern, async (route) => {
    if (route.request().method() !== method) return route.fallback();
    seen.push(route.request().url());
    await gate;
    // `status` turns the held request into a failure instead of letting it
    // through, which is how the re-enable-after-error path gets covered.
    if (status) return route.fulfill({ status, contentType: "application/json", body: JSON.stringify({ error: "held request rejected on purpose" }) });
    return route.continue();
  });

  return {
    // Every request that reached the gate, in order. Length is the assertion:
    // this is the number the bug made two.
    seen,
    // Waits until at least `count` requests have arrived, so a spec never
    // asserts on the busy state before the app has even sent anything.
    async arrived(count = 1, timeout = 5000) {
      const deadline = Date.now() + timeout;
      while (seen.length < count) {
        if (Date.now() > deadline) throw new Error(`only ${seen.length} of ${count} ${method} requests reached the gate within ${timeout}ms`);
        await new Promise((r) => setTimeout(r, 20));
      }
    },
    release: () => open(),
  };
}

// Presses a control twice in one synchronous turn, the way a double tap does,
// and reports how many requests are in flight afterwards.
//
// Not locator.click() twice: that auto-waits for the control to be enabled, so
// once the fix lands the second call would simply hang, and the spec would be
// asserting nothing at all. Both presses have to leave before the browser gets
// a chance to process either.
//
// The returned count is the assertion that matters, and it is why this reads
// the fetch tracker rather than only counting what arrives at the gate. A
// handler reaches its fetch() *synchronously* from the click, so by the time
// this evaluate returns, a second press that got through has already
// incremented `pending` -- whereas the request itself may still be crossing to
// the route handler, and counting only there passes against broken code if the
// spec looks a moment too early. That exact false pass happened while this
// suite was being written.
export async function doubleClick(page, selector) {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) throw new Error(`nothing matches ${sel}`);
    el.click();
    el.click();
    return window.__caravelFetches.pending;
  }, selector);
}

// The same, for the controls a spec has to name individually: `presses` is a
// list of selectors, clicked in order in one turn. A form is submitted rather
// than clicked, since Enter in a card is one of the paths being covered.
export async function pressAll(page, presses) {
  return page.evaluate((sels) => {
    for (const sel of sels) {
      const el = document.querySelector(sel);
      if (!el) throw new Error(`nothing matches ${sel}`);
      if (el.tagName === "FORM") el.requestSubmit();
      else el.click();
    }
    return window.__caravelFetches.pending;
  }, presses);
}
