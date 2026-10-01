// A scripted compass, for the Stage 42 cone.
//
// There is no Playwright API for device orientation at all, so readings are
// dispatched as events on window -- built as a plain Event with the fields
// defined on it, rather than through the DeviceOrientationEvent constructor,
// because webkitCompassHeading (the iOS field) cannot be passed to that
// constructor anywhere and the constructor itself is not on every engine.
//
// Also counts the live listeners for both event types, which is how a test
// proves the compass is released rather than merely that the cone is hidden.

// platform: "absolute" (has deviceorientationabsolute, as Chrome and Firefox
// do), "plain" (an engine without it -- removed here so the fallback is
// exercised), or "ios" (requestPermission, answering `answer`).
export async function installFakeCompass(page, { platform = "plain", answer = "granted", angle = 0 } = {}) {
  await page.addInitScript(
    ({ platform, answer, angle }) => {
      const state = { listeners: 0, asked: 0, askedInClick: [] };
      const TYPES = new Set(["deviceorientation", "deviceorientationabsolute"]);
      const add = window.addEventListener.bind(window);
      const remove = window.removeEventListener.bind(window);
      window.addEventListener = (type, ...rest) => {
        if (TYPES.has(type)) state.listeners += 1;
        return add(type, ...rest);
      };
      window.removeEventListener = (type, ...rest) => {
        if (TYPES.has(type)) state.listeners -= 1;
        return remove(type, ...rest);
      };

      if (platform === "absolute") {
        window.ondeviceorientationabsolute = null;
      } else {
        delete window.ondeviceorientationabsolute;
        delete Window.prototype.ondeviceorientationabsolute;
      }

      if (platform === "ios") {
        // "Inside the gesture" is approximated as "while a click is being
        // dispatched": a capturing listener on window runs first and a
        // bubbling one runs last, so between them is the dispatch.
        let inClick = false;
        add("click", () => (inClick = true), true);
        add("click", () => (inClick = false));
        window.DeviceOrientationEvent ??= function DeviceOrientationEvent() {};
        window.DeviceOrientationEvent.requestPermission = () => {
          state.asked += 1;
          state.askedInClick.push(inClick);
          return Promise.resolve(answer);
        };
      }

      if (angle) {
        Object.defineProperty(screen, "orientation", {
          configurable: true,
          value: { angle, type: "landscape-primary", addEventListener() {}, removeEventListener() {} },
        });
      }

      state.read = (type, fields) => {
        const event = new Event(type);
        for (const [k, v] of Object.entries(fields)) Object.defineProperty(event, k, { value: v });
        window.dispatchEvent(event);
      };
      window.__compass = state;
    },
    { platform, answer, angle }
  );
}

export const readCompass = (page, type, fields) =>
  page.evaluate(([t, f]) => window.__compass.read(t, f), [type, fields]);

export const compassState = (page) =>
  page.evaluate(() => ({
    listeners: window.__compass.listeners,
    asked: window.__compass.asked,
    askedInClick: window.__compass.askedInClick,
  }));
