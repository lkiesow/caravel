import { t } from "../i18n.js";
import { icon } from "../icon.js";
import { safeHref } from "../url.js";

// The parts of an assistant run that are the same whatever was asked for: the
// event keys the stream may carry, the run trace, and the list of pages it
// read.
//
// Shared by the two screens that run the agent -- the panel in the location
// editor (one place, enriched) and the suggestion page (several places at
// once). Both consume the same progress / step / summary / error events from
// the same transport, so an account of what a run did belongs with the
// transport rather than with either screen.
//
// Extracted in Stage 27 Milestone 5 from assist-panel.js, which had the only
// copy. The alternative was a second one, which would have drifted the first
// time a step key was added on the server.

// The progress keys the server may send, spelled out for two reasons. An
// unknown key -- a server newer than this file -- falls back to something
// generic rather than rendering a raw key at the user. And it is the only way
// scripts/i18n.py can see these keys at all: they arrive at runtime, and its
// scanner cannot follow a variable into t(). See plans/todo.md.
const PROGRESS_KEYS = new Set([
  "assist.progress.thinking",
  "assist.progress.searching",
  "assist.progress.reading",
  "assist.progress.checkingMap",
  "assist.progress.checkingLinks",
  "assist.progress.composing",
  "assist.progress.wrappingUp",
]);

// The step keys the server may send, spelled out for the same two reasons.
const STEP_KEYS = new Set([
  "assist.step.thinking",
  "assist.step.searching",
  "assist.step.reading",
  "assist.step.checkingMap",
  "assist.step.checkingLinks",
  "assist.step.composing",
]);

const ERROR_KEYS = {
  assist_timeout: "assist.error.timeout",
  assist_budget: "assist.error.budget",
  assist_busy: "assist.error.busy",
  assist_failed: "assist.error.failed",
};

// The i18n key for one progress event, or the generic one when the server is
// newer than this file.
export function progressKey(key) {
  return PROGRESS_KEYS.has(key) ? key : "assist.progress.thinking";
}

// Whether a step event is one this client can render. An unknown key would
// render as a raw key, so callers drop it -- the count in the trace heading
// comes from the server's own total, so it still adds up.
export function isKnownStep(key) {
  return STEP_KEYS.has(key);
}

// The i18n key for a failure code, from an error event or from an ApiError
// body. Anything unrecognised is a plain failure.
export function errorKey(code) {
  return ERROR_KEYS[code] ?? "assist.error.failed";
}

// Seconds to one decimal, which is the resolution that distinguishes a step
// worth looking at from one that is not. Milliseconds would be noise and whole
// seconds would render every quick step as "0 s".
function seconds(ms) {
  return t("assist.trace.seconds", { seconds: (Math.max(0, ms) / 1000).toFixed(1) });
}

// createRunTrace collects a run's steps and renders them into slot.
//
// Always rendered, not gated on a setting. It costs one closed line, it
// describes the reader's own run, and "what did it do to get this" is a
// question about trust rather than about debugging. The server has the same
// account at debug level for whoever runs the instance.
export function createRunTrace(slot) {
  let steps = [];
  let totals = null;

  function clear() {
    steps = [];
    totals = null;
    slot.replaceChildren();
  }

  function addStep(step) {
    if (isKnownStep(step.key)) steps.push(step);
  }

  function setTotals(value) {
    totals = value;
  }

  // The heading line. Tokens are omitted rather than shown as zero: several
  // OpenAI-compatible servers report no usage at all, and a confident 0 there
  // reads as "this was free".
  function meta() {
    const parts = [];
    if (totals) {
      parts.push(seconds(totals.ms));
      parts.push(t("assist.trace.steps", { count: totals.steps }, totals.steps));
      if (totals.tokens > 0) parts.push(t("assist.trace.tokens", { count: totals.tokens }));
    } else {
      parts.push(t("assist.trace.steps", { count: steps.length }, steps.length));
    }
    return parts.join(" · ");
  }

  function render() {
    slot.replaceChildren();
    if (steps.length === 0) return;

    const details = document.createElement("details");
    details.className = "assist-trace";

    const summary = document.createElement("summary");
    summary.className = "assist-trace__summary";
    // Drawn, not the UA marker: `display: flex` on a <summary> removes the
    // triangle, which is the same trap itinerary-tab.js documents. icon()
    // returns markup rather than a node, and this is the one place in the file
    // where innerHTML is used -- safe because the string is a fixed literal
    // with no run data anywhere in it.
    summary.insertAdjacentHTML("afterbegin", icon("chevron-down", { className: "assist-trace__chevron" }));
    const title = document.createElement("span");
    title.className = "assist-trace__title";
    title.textContent = t("assist.trace.title");
    const metaEl = document.createElement("span");
    metaEl.className = "assist-trace__meta";
    metaEl.textContent = meta();
    summary.append(title, metaEl);

    const list = document.createElement("ol");
    list.className = "assist-trace__list";
    for (const step of steps) {
      const li = document.createElement("li");
      li.className = "assist-trace__step";
      if (step.failed) li.classList.add("assist-trace__step--failed");

      const label = document.createElement("span");
      label.className = "assist-trace__label";
      // Every value here came off a web page the agent read, so it is placed
      // as text and never as markup.
      label.textContent = t(step.key, step.params ?? {});
      li.appendChild(label);

      if (step.failed) {
        const failed = document.createElement("span");
        failed.className = "assist-trace__failed";
        failed.textContent = t("assist.step.failed");
        li.appendChild(failed);
      }

      const ms = document.createElement("span");
      ms.className = "assist-trace__ms";
      ms.textContent = seconds(step.ms);
      li.appendChild(ms);

      list.appendChild(li);
    }

    details.append(summary, list);
    slot.appendChild(details);
  }

  return { addStep, setTotals, clear, render };
}

// The pages a run read, rendered into slot and returned so a caller that has
// to clear it separately can hold on to it.
//
// Deliberately not counted as one of the editor panel's suggestions: the
// sources are part of the same answer but they are not something to accept or
// reject, and treating them as one is how that panel once came to report "1
// suggestion" with nothing left on screen.
//
// Every URL goes through safeHref even though the server only records pages it
// actually fetched, so none of them can be anything but http(s). The point is
// that every href in the app goes through the same gate; "cannot happen" is a
// poor reason to make one of them the exception.
export function renderSources(slot, sources) {
  if (!sources?.length) return null;

  const box = document.createElement("div");
  box.className = "assist-sources";

  const heading = document.createElement("h4");
  heading.textContent = t("assist.sources");

  const list = document.createElement("ul");
  for (const source of sources) {
    const href = safeHref(source.url);
    if (!href) continue;
    const li = document.createElement("li");
    const a = document.createElement("a");
    a.href = href;
    a.target = "_blank";
    a.rel = "noopener noreferrer";
    a.textContent = source.title || source.url;
    li.appendChild(a);
    list.appendChild(li);
  }

  const hint = document.createElement("p");
  hint.className = "assist-sources__hint";
  hint.textContent = t("assist.sourcesHint");

  box.append(heading, list, hint);
  slot.appendChild(box);
  return box;
}
