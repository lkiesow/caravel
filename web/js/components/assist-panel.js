import { api } from "../api.js";
import { t, translatePage, getLocale } from "../i18n.js";
import { icon } from "../icon.js";
import { getCurrentUser } from "../session.js";

// The "Search via AI" control and the review panel it produces.
//
// # Nothing here writes
//
// Every accepted field goes into the editor's draft, exactly as if it had been
// typed. Save is still the only thing that commits, which is what makes the
// whole feature safe to try: the worst outcome of a bad suggestion is that you
// press Cancel.
//
// # Per-field review, and why the before/after matters
//
// A field that is currently empty shows only what is proposed. A field with
// something in it shows both, labelled, and is marked as an overwrite. That is
// the difference between a tool that fills gaps and one that can quietly
// destroy a paragraph somebody wrote from memory in a hotel lobby. The server
// computes `overwrites` so the rule lives in one place, but the visible
// before/after is what actually protects the user.
//
// # Hidden unless the server can do it
//
// Same shape as the address search above it: `getCurrentUser().assist` is a
// server capability, and a control that could only ever report "not enabled"
// is worse than no control.

// The progress keys the server may send, spelled out for two reasons. It lets
// an unknown key -- a server newer than this file -- fall back to something
// generic instead of rendering a raw key at the user. And it is the only way
// scripts/i18n.py can see these keys at all: they arrive at runtime, and its
// scanner cannot follow a variable into t(). See the note in docs/plans/todo.md.
const PROGRESS_KEYS = new Set([
  "assist.progress.thinking",
  "assist.progress.searching",
  "assist.progress.reading",
  "assist.progress.checkingMap",
  "assist.progress.checkingLinks",
  "assist.progress.composing",
  "assist.progress.wrappingUp",
]);

// Error codes the server sends with a failure event, mapped to copy. Anything
// unrecognised falls back to the generic line rather than showing a code.
const ERROR_KEYS = {
  assist_timeout: "assist.error.timeout",
  assist_budget: "assist.error.budget",
  assist_busy: "assist.error.busy",
  assist_failed: "assist.error.failed",
};

// The fields the panel knows how to label and apply. A field the server
// proposes that is not in here is ignored rather than rendered namelessly --
// again, a newer server should degrade rather than produce nonsense.
const FIELD_KEYS = {
  title: "assist.field.title",
  category: "assist.field.category",
  type: "assist.field.type",
  notes: "assist.field.notes",
  address: "assist.field.address",
};

export function renderAssistPanel(container, { tripId, readCurrent, applyField, applyLink, applyCoordinates }) {
  // The capability check, before anything is rendered.
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

      <div class="assist__result" hidden>
        <div class="assist__result-header">
          <h3 data-i18n="assist.proposalHeading"></h3>
          <button type="button" class="btn btn-secondary btn-row" data-action="assist-dismiss">
            <span data-i18n="assist.dismiss"></span>
          </button>
        </div>
        <p class="assist__empty" data-i18n="assist.noSuggestions" hidden></p>
        <ul class="assist__suggestions"></ul>
        <div class="assist__sources" hidden>
          <h4 data-i18n="assist.sources"></h4>
          <ul class="assist__source-list"></ul>
          <p class="assist__sources-hint" data-i18n="assist.sourcesHint"></p>
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
  const resultEl = container.querySelector(".assist__result");
  const emptyEl = container.querySelector(".assist__empty");
  const listEl = container.querySelector(".assist__suggestions");
  const sourcesEl = container.querySelector(".assist__sources");
  const sourceListEl = container.querySelector(".assist__source-list");

  let controller = null;

  function setRunning(running) {
    statusEl.hidden = !running;
    runBtn.disabled = running;
    promptEl.disabled = running;
    if (running) {
      errorEl.hidden = true;
      resultEl.hidden = true;
    }
  }

  function showError(key, params) {
    errorEl.textContent = t(key, params);
    errorEl.hidden = false;
  }

  function showProgress(event) {
    const key = PROGRESS_KEYS.has(event.key) ? event.key : "assist.progress.thinking";
    progressEl.textContent = t(key, event.params ?? {});
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

    controller = new AbortController();
    setRunning(true);
    progressEl.textContent = t("assist.progress.thinking");

    try {
      let proposal = null;
      let failure = null;
      await api.postStream(
        `/trips/${tripId}/assist/location`,
        {
          mode,
          prompt,
          ...current,
          include_trip_context: contextToggle.checked,
          locale: getLocale(),
        },
        {
          signal: controller.signal,
          onEvent: (event) => {
            if (event.name === "progress") showProgress(event.data);
            else if (event.name === "proposal") proposal = event.data;
            else if (event.name === "error") failure = event.data;
          },
        }
      );

      if (failure) {
        showError(ERROR_KEYS[failure.code] ?? "assist.error.failed");
        return;
      }
      if (!proposal) {
        // The stream ended without either. Rare, but a silent no-op would look
        // exactly like a bug in the page rather than one in the run.
        showError("assist.error.failed");
        return;
      }
      renderProposal(proposal);
    } catch (err) {
      // Cancelling is not a failure, and there is nothing to report.
      if (err?.name === "AbortError") return;
      showError(ERROR_KEYS[err?.body?.code] ?? "assist.error.failed");
    } finally {
      controller = null;
      setRunning(false);
    }
  }

  function renderProposal(proposal) {
    listEl.innerHTML = "";
    resultEl.hidden = false;

    let count = 0;
    for (const field of proposal.fields ?? []) {
      if (!FIELD_KEYS[field.name]) continue;
      listEl.appendChild(fieldRow(field));
      count++;
    }
    for (const link of proposal.links ?? []) {
      listEl.appendChild(linkRow(link));
      count++;
    }
    if (proposal.lat != null && proposal.lng != null) {
      listEl.appendChild(coordinateRow(proposal.lat, proposal.lng));
      count++;
    }

    emptyEl.hidden = count > 0;

    // Sources are shown and stored nowhere: once a suggestion is accepted it
    // is just the user's own data. They are here so the proposal can be
    // judged, not as provenance.
    sourceListEl.innerHTML = "";
    const sources = proposal.sources ?? [];
    sourcesEl.hidden = sources.length === 0;
    for (const source of sources) {
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.href = source.url;
      a.target = "_blank";
      a.rel = "noopener noreferrer";
      // textContent throughout: every string in a proposal came off a web
      // page the agent read, so none of it is ours to trust as markup.
      a.textContent = source.title || source.url;
      li.appendChild(a);
      sourceListEl.appendChild(li);
    }
  }

  // One suggestion row, with Accept and Reject. Built with DOM calls rather
  // than an HTML string for the reason above: these values are
  // attacker-influenced, and one forgotten escape in a template is an
  // injection.
  function suggestionRow({ labelKey, overwrites, currentValue, proposedValue, onAccept }) {
    const li = document.createElement("li");
    li.className = "assist-suggestion";
    if (overwrites) li.classList.add("assist-suggestion--overwrite");

    const head = document.createElement("div");
    head.className = "assist-suggestion__head";
    const name = document.createElement("span");
    name.className = "assist-suggestion__name";
    name.textContent = t(labelKey);
    head.appendChild(name);
    if (overwrites) {
      const badge = document.createElement("span");
      badge.className = "assist-suggestion__badge";
      badge.textContent = t("assist.replaces");
      head.appendChild(badge);
    }
    li.appendChild(head);

    // The before/after. Only shown when there is a before -- an empty field
    // needs no comparison, and a row of blanks is noise.
    if (overwrites) {
      li.appendChild(valueBlock("assist.current", currentValue, "assist-suggestion__current"));
    }
    li.appendChild(valueBlock("assist.proposed", proposedValue, "assist-suggestion__proposed"));

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

    accept.addEventListener("click", () => {
      onAccept();
      // The row stays, marked, rather than vanishing: a list that shortens as
      // you work through it loses your place, and "did I already take that
      // one?" is a question the panel should answer for itself.
      actions.remove();
      li.classList.add("assist-suggestion--accepted");
      const done = document.createElement("p");
      done.className = "assist-suggestion__done";
      done.textContent = t("assist.accepted");
      li.appendChild(done);
    });
    reject.addEventListener("click", () => li.remove());

    actions.append(accept, reject);
    li.appendChild(actions);
    return li;
  }

  function valueBlock(labelKey, value, className) {
    const wrap = document.createElement("div");
    wrap.className = `assist-suggestion__value ${className}`;
    const label = document.createElement("span");
    label.className = "assist-suggestion__label";
    label.textContent = t(labelKey);
    const body = document.createElement("p");
    body.textContent = value;
    wrap.append(label, body);
    return wrap;
  }

  function fieldRow(field) {
    // Category is an enum on the wire and a translated label in the select it
    // changes. Both sides of the before/after go through the same lookup, or
    // the row reads "site -> Stay" and looks like two different kinds of
    // thing rather than one value changing.
    const display = (value) => (field.name === "category" && value ? t(`item.category.${value}`) : value);
    return suggestionRow({
      labelKey: FIELD_KEYS[field.name],
      overwrites: !!field.overwrites,
      currentValue: display(field.current),
      proposedValue: display(field.proposed),
      onAccept: () => applyField(field.name, field.proposed),
    });
  }

  function linkRow(link) {
    return suggestionRow({
      labelKey: "assist.field.link",
      overwrites: false,
      proposedValue: link.label ? `${link.label} — ${link.url}` : link.url,
      onAccept: () => applyLink(link),
    });
  }

  function coordinateRow(lat, lng) {
    return suggestionRow({
      labelKey: "assist.field.coordinates",
      overwrites: false,
      // Six decimals is about 10cm; more is noise from a geocoder that does
      // not know the building to that precision anyway.
      proposedValue: `${Number(lat).toFixed(6)}, ${Number(lng).toFixed(6)}`,
      onAccept: () => applyCoordinates({ lat, lng }),
    });
  }

  runBtn.addEventListener("click", run);
  container.querySelector('[data-action="assist-cancel"]').addEventListener("click", () => controller?.abort());
  container.querySelector('[data-action="assist-dismiss"]').addEventListener("click", () => {
    resultEl.hidden = true;
    listEl.innerHTML = "";
  });

  // Enter in the prompt runs it, and must not reach the form's own keydown
  // handler, where Enter means "save the page".
  promptEl.addEventListener("keydown", (e) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    e.stopPropagation();
    run();
  });

  return {
    // Called when the page re-renders under the panel, so a run in flight does
    // not keep streaming into detached nodes.
    destroy() {
      controller?.abort();
      controller = null;
    },
  };
}
