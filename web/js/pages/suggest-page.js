import { api } from "../api.js";
import { t, translatePage, getLocale } from "../i18n.js";
import { navigate } from "../router.js";
import { icon } from "../icon.js";
import { canEdit } from "../trip-role.js";
import { hasCapability } from "../session.js";
import { renderLoading } from "../components/loading.js";
import { renderNotFoundPage } from "./not-found-page.js";
import { createRunTrace, progressKey, errorKey, renderSources } from "../components/assist-run.js";
import { safeHref } from "../url.js";

// Asking the assistant for several places at once, and reviewing them.
//
// # Why a route rather than a panel on the locations tab
//
// Two reasons, and the second is the practical one. Six candidate cards want
// the whole width of a 324px screen, not the space under a toolbar. And
// renderItemsTab is re-run from scratch on every tab render, with all of its
// state in closure variables -- so a panel living inside it would be destroyed
// by switching to the map and back, in the middle of a run that costs money.
//
// # How this differs from the assistant in the location editor
//
// That one places each suggestion in a slot under the field it concerns, so
// there is nothing to select: accept or reject, field by field. Here the unit
// is a whole place, several of them, and the question is which ones to keep.
// So each candidate is a card with a checkbox, selected by default, and one
// button commits the ticked ones together.
//
// Nothing is written until that button is pressed, which is the same guarantee
// internal/assist makes: the agent proposes and a person decides.

// The categories a candidate may carry, for rendering its label. The server
// validates against the same three and sends an empty string rather than a
// guess, which renders as no category at all.
const CATEGORIES = ["site", "stay", "transport"];

export async function renderSuggestPage(container, { tripId }) {
  if (!hasCapability("assist")) {
    // Typeable URL, and an instance with no assistant has no such page. Not a
    // 403: the route genuinely does not exist here.
    renderNotFoundPage(container);
    return;
  }

  renderLoading(container);

  let trip;
  try {
    trip = await api.get(`/trips/${tripId}`);
  } catch {
    renderNotFoundPage(container);
    return;
  }
  if (!canEdit(trip)) {
    // A viewer could not save the result, and the run may carry the trip's
    // title and dates to a third party -- the same reasoning the server
    // applies in refusing them.
    renderNotFoundPage(container);
    return;
  }

  container.innerHTML = `
    <div class="page suggest-page">
      <a href="/trips/${tripId}/locations" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.back"></span></a>
      <div class="page__header">
        <h1 data-i18n="suggest.title"></h1>
      </div>

      <div class="editor-card">
        <h2 data-i18n="suggest.ask"></h2>
        <p class="editor-card__hint" data-i18n="suggest.hint"></p>

        <div class="suggest-page__ask">
          <!-- The card heading above already names this field, so a visible
               label would say the same thing twice. The accessible name is
               kept, because a heading is not a label. -->
          <input id="suggest-prompt" class="suggest-page__prompt" type="text"
                 data-i18n-placeholder="suggest.promptPlaceholder" data-i18n-aria-label="suggest.promptLabel" />
          <button type="button" class="btn btn-primary" data-action="suggest-run">
            ${icon("sparkles")} <span data-i18n="suggest.run"></span>
          </button>
        </div>
        <label class="suggest-page__context">
          <input type="checkbox" class="suggest-page__context-toggle" checked />
          <span data-i18n="assist.tripContext"></span>
        </label>

        <p class="suggest-page__status" role="status" hidden>
          <span class="spinner" aria-hidden="true"></span>
          <span class="suggest-page__progress"></span>
          <button type="button" class="btn btn-secondary btn-row" data-action="suggest-cancel" data-i18n="common.cancel"></button>
        </p>
        <p class="suggest-page__error" role="alert" hidden></p>
        <div class="suggest-page__trace-slot"></div>
      </div>

      <p class="suggest-page__note" hidden></p>
      <ul class="suggest-page__list"></ul>
      <div class="editor-card suggest-page__sources-slot" hidden></div>

      <div class="suggest-page__bar" hidden>
        <span class="suggest-page__count"></span>
        <button type="button" class="btn btn-primary" data-action="suggest-add"></button>
      </div>
    </div>
  `;
  translatePage(container);

  const promptEl = container.querySelector(".suggest-page__prompt");
  const runBtn = container.querySelector('[data-action="suggest-run"]');
  const addBtn = container.querySelector('[data-action="suggest-add"]');
  const contextToggle = container.querySelector(".suggest-page__context-toggle");
  const statusEl = container.querySelector(".suggest-page__status");
  const progressEl = container.querySelector(".suggest-page__progress");
  const errorEl = container.querySelector(".suggest-page__error");
  const noteEl = container.querySelector(".suggest-page__note");
  const listEl = container.querySelector(".suggest-page__list");
  const sourcesSlot = container.querySelector(".suggest-page__sources-slot");
  const barEl = container.querySelector(".suggest-page__bar");
  const countEl = container.querySelector(".suggest-page__count");
  const trace = createRunTrace(container.querySelector(".suggest-page__trace-slot"));

  let controller = null;
  // One entry per candidate on screen: the candidate as the server sent it,
  // and the checkbox that decides whether it is added.
  let cards = [];

  function setRunning(running) {
    statusEl.hidden = !running;
    runBtn.disabled = running;
    promptEl.disabled = running;
    addBtn.disabled = running;
    if (running) {
      errorEl.hidden = true;
      noteEl.hidden = true;
    }
  }

  function showError(key) {
    errorEl.textContent = t(key);
    errorEl.hidden = false;
  }

  function syncBar() {
    const n = cards.filter((c) => c.checkbox.checked).length;
    // Shown whenever there is anything to add, disabled when nothing is
    // ticked, rather than disappearing: a button that vanishes as you untick
    // the last card takes the layout with it.
    barEl.hidden = cards.length === 0;
    addBtn.disabled = n === 0;
    countEl.textContent = t("suggest.selected", { count: n }, n);
    addBtn.textContent = t("suggest.add", { count: n }, n);
  }

  function clearResults() {
    cards = [];
    listEl.replaceChildren();
    sourcesSlot.replaceChildren();
    sourcesSlot.hidden = true;
    noteEl.hidden = true;
    syncBar();
  }

  // One candidate.
  //
  // Built with DOM calls rather than a template string, for the reason
  // assist-panel.js gives: every value here came off a web page the agent
  // read, and the difference between textContent and innerHTML is the whole
  // defence.
  function renderCard(candidate) {
    const li = document.createElement("li");
    li.className = "suggest-card";

    const label = document.createElement("label");
    label.className = "suggest-card__select";

    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    // Ticked by default: the model was asked for places worth adding, and the
    // reviewing act is removing the wrong ones rather than picking the right
    // ones out of a blank list.
    checkbox.checked = true;
    checkbox.addEventListener("change", syncBar);
    label.appendChild(checkbox);

    const body = document.createElement("div");
    body.className = "suggest-card__body";

    if (candidate.cover?.thumb_url || candidate.cover?.url) {
      const img = document.createElement("img");
      img.className = "suggest-card__cover";
      img.src = candidate.cover.thumb_url || candidate.cover.url;
      img.alt = "";
      img.loading = "lazy";
      // A proposed cover is a URL on somebody else's server, and it may well
      // be gone. An image that fails to load leaves a broken icon, which reads
      // as a broken page rather than as a missing photograph.
      img.addEventListener("error", () => img.remove());
      body.appendChild(img);
    }

    const title = document.createElement("h2");
    title.className = "suggest-card__title";
    title.textContent = candidate.title;
    body.appendChild(title);

    const meta = [];
    if (CATEGORIES.includes(candidate.category)) meta.push(t(`item.category.${candidate.category}`));
    if (candidate.tags) meta.push(candidate.tags);
    if (meta.length) {
      const metaEl = document.createElement("p");
      metaEl.className = "suggest-card__meta";
      metaEl.textContent = meta.join(" · ");
      body.appendChild(metaEl);
    }

    if (candidate.notes) {
      const notes = document.createElement("p");
      notes.className = "suggest-card__notes";
      // The notes are markdown, and this is a preview rather than the place
      // they are read. Rendered as plain text: the location page renders them
      // properly once they are saved, through the server's sanitizer.
      notes.textContent = candidate.notes;
      body.appendChild(notes);
    }

    // Address and coordinates in one line, because what a reader wants to know
    // is "does the app know where this is", and either answers it.
    const place = candidate.address || (candidate.lat != null ? t("suggest.located") : "");
    if (place) {
      const placeEl = document.createElement("p");
      placeEl.className = "suggest-card__place";
      placeEl.textContent = place;
      body.appendChild(placeEl);
    }

    if (candidate.links?.length) {
      const links = document.createElement("ul");
      links.className = "suggest-card__links";
      for (const link of candidate.links) {
        const href = safeHref(link.url);
        // The server only ever offers links it has fetched, so this cannot
        // reject one in practice -- it is here because every href in the app
        // goes through the same gate, and "cannot happen" is not a reason to
        // make this the exception.
        if (!href) continue;
        const item = document.createElement("li");
        const a = document.createElement("a");
        a.href = href;
        a.target = "_blank";
        a.rel = "noopener";
        a.textContent = link.label || link.url;
        item.appendChild(a);
        links.appendChild(item);
      }
      if (links.children.length) body.appendChild(links);
    }

    if (candidate.cover?.credit || candidate.cover?.license) {
      const credit = document.createElement("p");
      credit.className = "suggest-card__credit";
      credit.textContent = [candidate.cover.credit, candidate.cover.license].filter(Boolean).join(" · ");
      body.appendChild(credit);
    }

    li.append(label, body);
    listEl.appendChild(li);
    cards.push({ candidate, checkbox });
  }

  async function run() {
    const prompt = promptEl.value.trim();
    if (!prompt) {
      showError("suggest.promptRequired");
      promptEl.focus();
      return;
    }

    // A second run replaces the first one's candidates rather than appending
    // to them: two answers to two different questions in one list would be
    // added together by the button at the bottom.
    clearResults();
    trace.clear();

    controller = new AbortController();
    setRunning(true);
    progressEl.textContent = t("assist.progress.thinking");

    try {
      let answer = null;
      let failure = null;
      await api.postStream(
        `/trips/${tripId}/assist/locations`,
        { prompt, include_trip_context: contextToggle.checked, locale: getLocale() },
        {
          signal: controller.signal,
          onEvent: (event) => {
            if (event.name === "progress") {
              progressEl.textContent = t(progressKey(event.data.key), event.data.params ?? {});
            } else if (event.name === "step") trace.addStep(event.data);
            else if (event.name === "summary") trace.setTotals(event.data);
            else if (event.name === "suggestions") answer = event.data;
            else if (event.name === "error") failure = event.data;
          },
        }
      );

      // Before the branches below, because a run that failed or found nothing
      // is exactly the one somebody wants an account of.
      trace.render();

      if (failure) {
        showError(errorKey(failure.code));
        return;
      }
      if (!answer) {
        showError("assist.error.failed");
        return;
      }

      for (const candidate of answer.candidates ?? []) renderCard(candidate);
      // The card is shown only if there is anything in it: an empty bordered
      // box under the results reads as something that failed to load.
      sourcesSlot.hidden = !renderSources(sourcesSlot, answer.sources);
      syncBar();

      // Two different notes, and the difference matters to the reader. "It
      // found four and two are already yours" is a good outcome; "it found
      // nothing" is not, and they should not read the same.
      if (answer.dropped > 0) {
        noteEl.textContent = t("suggest.dropped", { count: answer.dropped }, answer.dropped);
        noteEl.hidden = false;
      } else if (cards.length === 0) {
        noteEl.textContent = t("suggest.empty");
        noteEl.hidden = false;
      }
    } catch (err) {
      if (err?.name === "AbortError") return;
      showError(errorKey(err?.body?.code));
    } finally {
      controller = null;
      setRunning(false);
    }
  }

  async function addSelected() {
    const chosen = cards.filter((c) => c.checkbox.checked).map((c) => c.candidate);
    if (chosen.length === 0) return;

    addBtn.disabled = true;
    errorEl.hidden = true;

    try {
      // One request, one transaction. Separate posts would be several chances
      // to half-finish, with no honest way to say which places landed.
      const created = await api.post(`/trips/${tripId}/items/batch`, {
        items: chosen.map((candidate) => ({
          title: candidate.title,
          category: candidate.category || "site",
          notes: candidate.notes || null,
          tags: splitTags(candidate.tags),
          links: (candidate.links ?? []).map((l) => ({ url: l.url, label: l.label || null })),
          ...(candidate.lat != null && candidate.lng != null
            ? { location: { lat: candidate.lat, lng: candidate.lng, address: candidate.address || null } }
            : candidate.address
              ? { location: { lat: null, lng: null, address: candidate.address } }
              : {}),
        })),
      });

      await attachCovers(created, chosen);
      navigate(`/trips/${tripId}/locations`);
    } catch (err) {
      console.error("adding suggested locations failed:", err?.body?.error || err?.message || err);
      showError("suggest.addFailed");
      addBtn.disabled = false;
    }
  }

  // Covers, after the locations exist.
  //
  // Not part of the batch: a cover is fetched from somebody else's server and
  // stored as a blob, which is multipart-shaped work that does not belong in a
  // JSON transaction -- see items_batch.go. So each is attached afterwards
  // through the endpoint the image field already uses.
  //
  // Best effort, deliberately. A cover that will not fetch must not undo a
  // location the user has already been told about: the failure costs a
  // picture, which they can add by hand, and the alternative is a rollback of
  // work that succeeded.
  async function attachCovers(created, chosen) {
    for (let i = 0; i < created.length; i++) {
      const cover = chosen[i]?.cover;
      if (!cover?.url) continue;
      try {
        const asset = await api.post(`/trips/${tripId}/media/url`, {
          url: cover.url,
          source_url: cover.source_url || "",
          credit: cover.credit || "",
          license: cover.license || "",
        });
        await api.put(`/items/${created[i].id}/image`, { media_asset_id: asset.id });
      } catch (err) {
        console.error("attaching a suggested cover failed:", err?.body?.error || err?.message || err);
      }
    }
  }

  // The wire carries tags as one comma-separated string, because the whole
  // proposal pipeline is string-shaped (see assist.Location.Tags). The items
  // API takes a list.
  function splitTags(tags) {
    return String(tags ?? "")
      .split(",")
      .map((tag) => tag.trim())
      .filter(Boolean);
  }

  runBtn.addEventListener("click", run);
  addBtn.addEventListener("click", addSelected);
  promptEl.addEventListener("keydown", (e) => {
    if (e.key === "Enter") run();
  });
  container.querySelector('[data-action="suggest-cancel"]').addEventListener("click", () => {
    controller?.abort();
  });

  syncBar();
  promptEl.focus();
}
