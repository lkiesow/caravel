import { api } from "../api.js";
import { t, translatePage, getLocale } from "../i18n.js";
import { icon } from "../icon.js";
import { createRunTrace, progressKey, errorKey, renderSources } from "./assist-run.js";
import { hasCapability } from "../session.js";

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
// `hasCapability("assist")` is a server capability. A control that could only
// ever report "not enabled" is worse than no control.

// The scalar fields the panel knows how to place. A field name the server
// invents has no slot and is skipped, rather than being rendered somewhere
// arbitrary.
const FIELD_NAMES = ["title", "category", "tags", "notes", "address"];

export function renderAssistPanel(container, { tripId, root, readCurrent, applyField, applyLink, applyCoordinates, applyCover }) {
  if (!hasCapability("assist")) {
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
  const trace = createRunTrace(container.querySelector(".assist__trace-slot"));

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

  // The cover photograph: the one suggestion whose value cannot be judged as
  // text. A URL tells you nothing about whether it is a picture of the right
  // building, so this shows the image.
  //
  // Built on the same row as every other suggestion -- same accept and reject,
  // same slot mechanism -- with the value replaced by a thumbnail and a
  // provenance line underneath.
  function addCoverSuggestion(cover) {
    addSuggestion("cover", {
      overwrites: false,
      onAccept: () => applyCover?.(cover),
      // A node rather than a string: addSuggestion renders text by default,
      // and this is the one case that needs markup it builds itself.
      node: () => {
        const wrap = document.createElement("div");
        wrap.className = "assist-cover";

        const img = document.createElement("img");
        img.className = "assist-cover__image";
        img.src = cover.thumb_url || cover.url;
        // Deliberately not lazy. The picture *is* the suggestion, it was asked
        // for by an explicit action, and an unloaded img with no intrinsic
        // size collapses to nothing -- so lazy loading made the row grow from
        // zero height exactly as the reader scrolled to it.
        // The image is decoration for a proposal that is described in words
        // below it; naming it again would be repetition for a screen reader.
        img.alt = "";
        // A thumbnail that will not load must not leave an invisible hole
        // where a picture should be -- image-field.js learned the same lesson
        // for its preview.
        img.addEventListener("error", () => {
          img.remove();
          wrap.classList.add("assist-cover--broken");
        });
        wrap.appendChild(img);

        const meta = document.createElement("p");
        meta.className = "assist-cover__meta";
        // Where it came from, always: an image with no stated origin is not
        // something to accept blind.
        const host = document.createElement("a");
        host.href = cover.source_url;
        host.target = "_blank";
        host.rel = "noopener noreferrer";
        host.textContent = hostOf(cover.source_url);
        meta.appendChild(host);
        // The credit, when the source states one. og:image never does;
        // Wikimedia nearly always does, and it is a condition of use.
        if (cover.credit || cover.license) {
          const credit = document.createElement("span");
          credit.className = "assist-cover__credit";
          credit.textContent = [cover.credit, cover.license].filter(Boolean).join(" · ");
          meta.append(document.createTextNode(" — "), credit);
        }
        wrap.appendChild(meta);
        return wrap;
      },
    });
  }

  // A URL reduced to its host, for display. The full URL is long and came off
  // a page the agent read; the host is the part a person actually reads.
  function hostOf(raw) {
    try {
      return new URL(raw).host;
    } catch {
      return raw;
    }
  }

  // One suggestion, built with DOM calls rather than a template string: every
  // value in a proposal came off a web page the agent read, so one forgotten
  // escape in a template is an injection.
  function addSuggestion(fieldName, { value, overwrites, onAccept, node }) {
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

    let body;
    if (node) {
      body = node();
      body.classList.add("assist-suggestion__value");
    } else {
      body = document.createElement("p");
      body.className = "assist-suggestion__value";
      body.textContent = value;
    }

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
    trace.clear();

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
              progressEl.textContent = t(progressKey(event.data.key), event.data.params ?? {});
            } else if (event.name === "step") trace.addStep(event.data);
            else if (event.name === "summary") trace.setTotals(event.data);
            else if (event.name === "proposal") proposal = event.data;
            else if (event.name === "error") failure = event.data;
          },
        }
      );

      // Rendered before the branches below, because a run that failed or found
      // nothing is exactly the one somebody wants an account of.
      trace.render();

      if (failure) {
        showError(errorKey(failure.code));
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
      showError(errorKey(err?.body?.code));
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

    if (proposal.cover?.url) addCoverSuggestion(proposal.cover);

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
    showSources(proposal.sources ?? []);

    if (outstanding.length === 0) {
      showNote("assist.noSuggestions");
      return;
    }

    // The first suggestion is usually below the fold on a phone.
    outstanding[0].el.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }

  // The pages the run read. An explanation of the proposal, not a part of it:
  // there is nothing here to accept or reject, so it is deliberately not in
  // `outstanding` and the counter never sees it -- but it is held in
  // sourcesBox so Dismiss all can still clear it.
  //
  // The box itself is built by the shared helper in assist-run.js; what stays
  // here is where it goes, which is this panel's own slot mechanism.
  function showSources(sources) {
    const slot = root.querySelector('[data-assist-field="sources"]');
    if (!slot) return;
    clearSources();
    sourcesBox = renderSources(slot, sources);
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
