import { api } from "../api.js";
import { t, translatePage, getLocale } from "../i18n.js";
import { icon } from "../icon.js";
import { getCurrentUser } from "../session.js";

// The "Search via AI" control, and the suggestions it produces.
//
// # Where the suggestions go
//
// Not into a list of their own. Each one is placed in the empty
// `[data-assist-field]` slot that sits directly under the control it is about:
// the suggested title under the title box, the suggested category under the
// category select, and so on. A suggestion three cards away from the field it
// concerns cannot be compared with what is already there, which is the only
// thing the reviewer is actually trying to do.
//
// That also removes the need to show the current value inside the suggestion.
// It is right there in the field above it. What stays is the *marking* -- an
// overwrite gets a red edge and a badge -- because "this will replace
// something" is the one thing the neighbouring field cannot tell you at a
// glance.
//
// # Nothing here writes
//
// Accepting fills the form, exactly as typing would; Save is still the only
// thing that commits. An accepted suggestion then removes itself, because it
// has become the field above it and leaving a copy behind is just a second
// thing to read. Rejecting removes it too. So working down the form empties it
// as you go, and whatever is left is what you have not decided yet.
//
// # Hidden unless the server can do it
//
// `getCurrentUser().assist` is a server capability. A control that could only
// ever report "not enabled" is worse than no control.

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

// The step keys the server may send, spelled out for the same two reasons as
// PROGRESS_KEYS above: an unknown key from a newer server falls back rather
// than rendering a raw key at somebody, and scripts/i18n.py cannot follow a
// variable into t().
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

// The scalar fields the panel knows how to place. A field name the server
// invents has no slot and is skipped, rather than being rendered somewhere
// arbitrary.
const FIELD_NAMES = ["title", "category", "type", "notes", "address"];

export function renderAssistPanel(container, { tripId, root, readCurrent, applyField, applyLink, applyCoordinates }) {
  if (!getCurrentUser()?.assist) {
    container.hidden = true;
    return { destroy() {} };
  }

  container.hidden = false;
  container.innerHTML = `
    <div class="assist">
      <div class="assist__row">
        <input type="search" class="assist__prompt" autocomplete="off"
               data-i18n-placeholder="assist.promptPlaceholder"
               data-i18n-aria-label="assist.promptPlaceholder" />
        <button type="button" class="btn btn-secondary btn-row" data-action="assist-run">
          ${icon("sparkles")} <span data-i18n="assist.run"></span>
        </button>
      </div>
      <p class="assist__hint" data-i18n="assist.hint"></p>
      <label class="assist__context">
        <input type="checkbox" class="assist__context-toggle" checked />
        <span data-i18n="assist.tripContext"></span>
      </label>

      <div class="assist__status" role="status" hidden>
        <span class="assist__spinner" aria-hidden="true"></span>
        <span class="assist__progress"></span>
        <button type="button" class="btn btn-secondary btn-row" data-action="assist-cancel">
          ${icon("x")} <span data-i18n="common.cancel"></span>
        </button>
      </div>

      <p class="assist__error" role="alert" hidden></p>
      <p class="assist__note" role="status" hidden></p>

      <div class="assist__trace-slot"></div>

      <div class="assist__bar" hidden>
        <span class="assist__count"></span>
        <div class="assist__bar-actions">
          <button type="button" class="btn btn-secondary btn-row" data-action="assist-accept-all">
            ${icon("check-check")} <span data-i18n="assist.acceptAll"></span>
          </button>
          <button type="button" class="btn btn-secondary btn-row" data-action="assist-dismiss-all">
            ${icon("x")} <span data-i18n="assist.dismissAll"></span>
          </button>
        </div>
      </div>
    </div>
  `;
  translatePage(container);

  const promptEl = container.querySelector(".assist__prompt");
  const runBtn = container.querySelector('[data-action="assist-run"]');
  const contextToggle = container.querySelector(".assist__context-toggle");
  const statusEl = container.querySelector(".assist__status");
  const progressEl = container.querySelector(".assist__progress");
  const errorEl = container.querySelector(".assist__error");
  const noteEl = container.querySelector(".assist__note");
  const barEl = container.querySelector(".assist__bar");
  const countEl = container.querySelector(".assist__count");
  const traceSlot = container.querySelector(".assist__trace-slot");

  let controller = null;
  // Every suggestion currently on the page, so Accept all / Dismiss all have
  // something to act on and the counter has something to count. Only
  // suggestions: the sources box below is part of the same proposal but is
  // not one of them, and putting it in here is exactly how the counter came
  // to floor at "1 suggestion" with nothing left on screen.
  let outstanding = [];
  // The sources box, tracked separately so "Dismiss all" can clear it without
  // it ever being counted or accepted.
  let sourcesBox = null;
  // The run trace: every step as it finishes, and the totals when the run
  // closes. Collected during the run and rendered once at the end rather than
  // grown row by row -- a list that reflows while you are reading the status
  // line above it is worse than one that appears complete.
  let steps = [];
  let totals = null;

  function setRunning(running) {
    statusEl.hidden = !running;
    runBtn.disabled = running;
    promptEl.disabled = running;
    if (running) {
      errorEl.hidden = true;
      noteEl.hidden = true;
    }
  }

  function showError(key, params) {
    errorEl.textContent = t(key, params);
    errorEl.hidden = false;
  }

  function showNote(key) {
    noteEl.textContent = t(key);
    noteEl.hidden = false;
  }

  function forget(entry) {
    entry.el.remove();
    outstanding = outstanding.filter((o) => o !== entry);
    syncBar();
  }

  function syncBar() {
    const n = outstanding.length;
    barEl.hidden = n === 0;
    countEl.textContent = t("assist.outstanding", { count: n }, n);
  }

  function clearSuggestions() {
    for (const entry of outstanding) entry.el.remove();
    outstanding = [];
    clearSources();
    syncBar();
  }

  function clearSources() {
    sourcesBox?.remove();
    sourcesBox = null;
  }

  function clearTrace() {
    steps = [];
    totals = null;
    traceSlot.replaceChildren();
  }

  // Seconds to one decimal, which is the resolution that distinguishes a step
  // worth looking at from one that is not. Milliseconds would be noise and
  // whole seconds would render every quick step as "0 s".
  function seconds(ms) {
    return t("assist.trace.seconds", { seconds: (Math.max(0, ms) / 1000).toFixed(1) });
  }

  // The run trace: what the assistant actually did, collapsed.
  //
  // Always rendered, not gated on a setting. It costs one closed line, it
  // describes the reader's own run, and "what did it do to get this" is a
  // question about trust rather than about debugging. The server has the same
  // account at debug level for whoever runs the instance.
  function renderTrace() {
    traceSlot.replaceChildren();
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
    const meta = document.createElement("span");
    meta.className = "assist-trace__meta";
    meta.textContent = traceMeta();
    summary.append(title, meta);

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
    traceSlot.appendChild(details);
  }

  // The heading line. Tokens are omitted rather than shown as zero: several
  // OpenAI-compatible servers report no usage at all, and a confident 0 there
  // reads as "this was free".
  function traceMeta() {
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

  // One suggestion, built with DOM calls rather than a template string: every
  // value in a proposal came off a web page the agent read, so one forgotten
  // escape in a template is an injection.
  function addSuggestion(fieldName, { value, overwrites, onAccept }) {
    const slot = root.querySelector(`[data-assist-field="${fieldName}"]`);
    // No slot means this page has nowhere sensible to put it -- a newer server
    // proposing a field this build does not have. Skipped rather than dumped
    // somewhere arbitrary.
    if (!slot) return;

    const el = document.createElement("div");
    el.className = "assist-suggestion";
    if (overwrites) el.classList.add("assist-suggestion--overwrite");

    const head = document.createElement("div");
    head.className = "assist-suggestion__head";
    const tag = document.createElement("span");
    tag.className = "assist-suggestion__tag";
    tag.textContent = t("assist.suggestionLabel");
    head.appendChild(tag);
    if (overwrites) {
      const badge = document.createElement("span");
      badge.className = "assist-suggestion__badge";
      badge.textContent = t("assist.replaces");
      head.appendChild(badge);
    }

    const body = document.createElement("p");
    body.className = "assist-suggestion__value";
    body.textContent = value;

    const actions = document.createElement("div");
    actions.className = "assist-suggestion__actions";
    const accept = document.createElement("button");
    accept.type = "button";
    accept.className = "btn btn-secondary btn-row";
    accept.textContent = t("assist.accept");
    const reject = document.createElement("button");
    reject.type = "button";
    reject.className = "btn btn-secondary btn-row";
    reject.textContent = t("assist.reject");
    actions.append(accept, reject);

    el.append(head, body, actions);
    slot.appendChild(el);

    const entry = {
      el,
      accept: () => {
        onAccept();
        forget(entry);
      },
    };
    accept.addEventListener("click", entry.accept);
    reject.addEventListener("click", () => forget(entry));
    outstanding.push(entry);
    syncBar();
  }

  async function run() {
    const prompt = promptEl.value.trim();
    const current = readCurrent();
    // Enrich when there is something to enrich, prompt when there is not. A
    // named location plus a prompt is still enrich: the prompt then narrows
    // what to look for rather than describing a different place.
    const mode = current.title.trim() ? "enrich" : "prompt";
    if (mode === "prompt" && !prompt) {
      showError("assist.error.promptRequired");
      promptEl.focus();
      return;
    }

    // A second run replaces the first run's suggestions rather than piling up
    // two opinions under one field, and its trace likewise describes this run
    // rather than the last one.
    clearSuggestions();
    clearTrace();

    controller = new AbortController();
    setRunning(true);
    progressEl.textContent = t("assist.progress.thinking");

    try {
      let proposal = null;
      let failure = null;
      await api.postStream(
        `/trips/${tripId}/assist/location`,
        { mode, prompt, ...current, include_trip_context: contextToggle.checked, locale: getLocale() },
        {
          signal: controller.signal,
          onEvent: (event) => {
            if (event.name === "progress") {
              const key = PROGRESS_KEYS.has(event.data.key) ? event.data.key : "assist.progress.thinking";
              progressEl.textContent = t(key, event.data.params ?? {});
            } else if (event.name === "step") {
              // An unknown key from a newer server would render as a raw key,
              // so it is dropped instead. The count in the heading comes from
              // the server's own total, so it still adds up.
              if (STEP_KEYS.has(event.data.key)) steps.push(event.data);
            } else if (event.name === "summary") totals = event.data;
            else if (event.name === "proposal") proposal = event.data;
            else if (event.name === "error") failure = event.data;
          },
        }
      );

      // Rendered before the branches below, because a run that failed or found
      // nothing is exactly the one somebody wants an account of.
      renderTrace();

      if (failure) {
        showError(ERROR_KEYS[failure.code] ?? "assist.error.failed");
        return;
      }
      if (!proposal) {
        // The stream ended with neither. Rare, but a silent no-op would look
        // like a bug in the page rather than one in the run.
        showError("assist.error.failed");
        return;
      }
      placeProposal(proposal);
    } catch (err) {
      // Cancelling is not a failure and there is nothing to report.
      if (err?.name === "AbortError") return;
      showError(ERROR_KEYS[err?.body?.code] ?? "assist.error.failed");
    } finally {
      controller = null;
      setRunning(false);
    }
  }

  function placeProposal(proposal) {
    for (const field of proposal.fields ?? []) {
      if (!FIELD_NAMES.includes(field.name)) continue;
      // Category is an enum on the wire and a translated label in the select
      // it changes; show what the control will show.
      const shown = field.name === "category" ? t(`item.category.${field.proposed}`) : field.proposed;
      addSuggestion(field.name, {
        value: shown,
        overwrites: !!field.overwrites,
        onAccept: () => applyField(field.name, field.proposed),
      });
    }

    for (const link of proposal.links ?? []) {
      addSuggestion("links", {
        value: link.label ? `${link.label} — ${link.url}` : link.url,
        overwrites: false,
        onAccept: () => applyLink(link),
      });
    }

    if (proposal.lat != null && proposal.lng != null) {
      addSuggestion("coordinates", {
        // Six decimals is about 10cm; more is noise from a geocoder that does
        // not know the building to that precision anyway.
        value: `${Number(proposal.lat).toFixed(6)}, ${Number(proposal.lng).toFixed(6)}`,
        overwrites: false,
        onAccept: () => applyCoordinates({ lat: proposal.lat, lng: proposal.lng }),
      });
    }

    // Sources are shown so the proposal can be judged, and stored nowhere:
    // once a suggestion is accepted it is just the user's own data. Rendered
    // before the empty check below, because a run that found nothing worth
    // suggesting still owes an account of where it looked -- and because the
    // early return used to skip this entirely.
    renderSources(proposal.sources ?? []);

    if (outstanding.length === 0) {
      showNote("assist.noSuggestions");
      return;
    }

    // The first suggestion is usually below the fold on a phone.
    outstanding[0].el.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }

  // The pages the run read. An explanation of the proposal, not a part of it:
  // there is nothing here to accept or reject, so it is deliberately not in
  // `outstanding` and the counter never sees it.
  function renderSources(sources) {
    const slot = root.querySelector('[data-assist-field="sources"]');
    if (!slot) return;
    clearSources();
    if (sources.length === 0) return;

    const box = document.createElement("div");
    box.className = "assist-sources";
    const heading = document.createElement("h4");
    heading.textContent = t("assist.sources");
    const list = document.createElement("ul");
    for (const source of sources) {
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.href = source.url;
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
    // Held here rather than in `outstanding` so Dismiss all can still clear it
    // -- the sources belong to a proposal nobody is looking at any more --
    // without it being counted as something you have yet to decide.
    sourcesBox = box;
  }

  runBtn.addEventListener("click", run);
  container.querySelector('[data-action="assist-cancel"]').addEventListener("click", () => controller?.abort());
  container.querySelector('[data-action="assist-accept-all"]').addEventListener("click", () => {
    // A copy, because accept() mutates the list it is iterating.
    for (const entry of [...outstanding]) entry.accept();
  });
  // Dismiss all clears the proposal, not the account of how it was reached:
  // the run still happened, and the trace is the answer to "why was that
  // useless?" -- which is the likeliest question at that moment.
  container.querySelector('[data-action="assist-dismiss-all"]').addEventListener("click", clearSuggestions);

  // Enter in the prompt runs it, and must not reach the form's own keydown
  // handler, where Enter means "save the page".
  promptEl.addEventListener("keydown", (e) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    e.stopPropagation();
    run();
  });

  syncBar();

  return {
    // Called when the page re-renders under the panel, so a run in flight does
    // not keep streaming into detached nodes.
    destroy() {
      controller?.abort();
      controller = null;
    },
  };
}
