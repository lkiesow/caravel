import { api } from "../api.js";
import { createGuard, guardClick } from "../busy.js";
import { t, translatePage, getLocale } from "../i18n.js";
import { navigate } from "../router.js";
import { renderItemForm } from "../components/location-form.js";
import { renderImageField } from "../components/image-field.js";
import { renderFileList } from "../components/file-list.js";
import { icon } from "../icon.js";
import { confirmDialog } from "../components/dialog.js";
import { renderLoading } from "../components/loading.js";
import { renderNotFoundPage } from "./not-found-page.js";
import { canEdit, isShared } from "../trip-role.js";
import "../components/leaflet-map.js";
import { hasCapability } from "../session.js";
import { renderAssistPanel } from "../components/assist-panel.js";

// Both modes render the same cards, in the same order - Basic info, Cover
// photo, Location, Links, Dates, Files - matching the read view's
// section order (location-view-page.js), and both commit through the same
// single Save button at the bottom.
//
// Everything typed on the page is held in `draft` until then, and goes out
// as ONE request: the item's own fields plus nested location/links/dates,
// which handleCreateItem/handleUpdateItem write in a transaction (Stage 09
// Milestone 1). Before that the page had five independent save paths, and
// the visually primary Save only carried Basic info - so coordinates typed
// into the card below it were silently discarded. One request also means
// there is nothing to guard against navigating away from: either Save was
// pressed and everything landed, or it wasn't and nothing did.
//
// The cover photo and the files cannot ride in a JSON body, so create mode
// sends a multipart one instead (Stage 23 Milestones 3-4): the item as JSON
// in an "item" part, the staged cover and the staged files alongside it, and
// the server commits all of it in one transaction or none of it. In edit mode
// they still write immediately against the existing item - image-field.js and
// file-list.js each take a path and own their own request - because there is
// already something to attach them to.
//
// What that replaced is worth remembering, because the failure was quiet: the
// create returned an ID, flushUploads() then wrote the cover, and a failure
// there left the location saved without it. The page did not adopt the item
// it had just created, so it was still in create mode, and pressing Save
// again made a *second* location.
export async function renderLocationEditorPage(container, { tripId, itemId }) {
  let item = null;
  renderLoading(container);

  // A viewer reaching this route — by typed URL, a bookmark, or a back button
  // after being demoted — gets sent somewhere useful rather than shown a form
  // whose every save would 403. This is the only place in the app that
  // redirects on a role, because it is the only route that exists solely to
  // write.
  let trip;
  try {
    trip = await api.get(`/trips/${tripId}`);
  } catch {
    renderNotFoundPage(container, { href: "/trips", labelKey: "common.home" });
    return;
  }
  if (!canEdit(trip)) {
    // The location they were trying to edit if there is one, the trip if not.
    navigate(itemId ? `/trips/${tripId}/locations/${itemId}` : `/trips/${tripId}`);
    return;
  }

  if (itemId) {
    try {
      item = await api.get(`/items/${itemId}`);
    } catch {
      renderNotFoundPage(container, { href: `/trips/${tripId}`, labelKey: "common.back" });
      return;
    }
  }

  // The page's working copy, in both modes. links/dates are edited here and
  // sent as whole lists (the API replaces the set), so the add/remove
  // handlers just push and splice - no per-row request, and no difference
  // between creating and editing. image/files are the upload staging
  // slots, used in create mode only.
  // Set by render(); save() and the per-card Enter handlers reach it through
  // this binding rather than being passed it, since they're all closures over
  // the same single render.
  let itemForm;
  // The cover-photo field's handle, so the assistant's cover suggestion can
  // write through the component's own API rather than reaching into its DOM.
  let imageField = null;
  // Assigned by renderLocationForm(). The assistant writes coordinates through
  // it so the map and the "show on map" hint update exactly as they do when a
  // pin is dragged, rather than the fields being set behind their backs.
  let setCoordinates = null;
  let assistPanel = null;

  const draft = {
    image: null,
    files: [],
    links: (item?.links ?? []).map((l) => ({ url: l.url, label: l.label ?? null })),
    dates: (item?.dates ?? []).map((d) => ({ start_date: d.start_date, end_date: d.end_date, label: d.label ?? null })),
  };

  // One guard for the page's one write, rather than one per control. Save is
  // reachable from three places - the button, Enter in the Basic info card,
  // Enter in the Location card - and three separate flags would leave two of
  // those doors open on a slow connection, which is exactly how the same
  // location got created twice. So the *wrapped* function is what every entry
  // point gets, and the button is what visibly goes busy. Resolved through the
  // container rather than captured, so it survives a re-render.
  const saveGuard = createGuard({ elements: () => container.querySelector('[data-action="save"]') });
  const save = saveGuard.wrap(commitSave);

  function render() {
    container.innerHTML = `
      <div class="page location-editor">
        <a href="${item ? `/trips/${tripId}/locations/${item.id}` : `/trips/${tripId}`}" data-link class="back-link">${icon("arrow-left")} <span data-i18n="common.back"></span></a>
        <div class="page__header">
          <h1></h1>
        </div>

        <div class="editor-card">
          <h2 data-i18n="location.editor.basicInfo"></h2>
          <div class="assist-slot" hidden></div>
          <div class="item-form-slot"></div>
          <div data-assist-field="sources"></div>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.image"></h2>
          <div class="image-field-slot"></div>
          <div data-assist-field="cover"></div>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.location"></h2>
          <form class="location-form">
            <label>
              <span data-i18n="item.detail.lat"></span>
              <input type="number" step="any" name="lat" />
            </label>
            <label>
              <span data-i18n="item.detail.lng"></span>
              <input type="number" step="any" name="lng" />
            </label>
            <div data-assist-field="coordinates"></div>
            <label>
              <span data-i18n="item.detail.address"></span>
              <input type="text" name="address" />
            </label>
            <div data-assist-field="address"></div>
            <div class="location-reverse" hidden>
              <button type="button" class="btn btn-secondary btn-row" data-action="lookup-address">${icon("map-pin")} <span data-i18n="location.form.lookupAddress"></span></button>
              <p class="location-reverse__status" role="status" hidden></p>
              <div class="location-reverse__offer" hidden>
                <p class="location-reverse__value"></p>
                <button type="button" class="btn btn-secondary btn-row" data-action="accept-address">${icon("check")} <span data-i18n="location.form.lookupAccept"></span></button>
              </div>
            </div>
            <label class="location-form__checkbox">
              <input type="checkbox" name="showOnMap" checked />
              <span data-i18n="location.form.showOnMap"></span>
            </label>
            <p class="location-form__hint" data-i18n="location.form.showOnMapHint" hidden></p>
            <div class="location-search" hidden>
              <div class="location-search__row">
                <input type="search" name="placeQuery" autocomplete="off" data-i18n-placeholder="location.form.searchPlaceholder" data-i18n-aria-label="location.form.searchPlaceholder" />
                <button type="button" class="btn btn-secondary btn-row" data-action="search-place">${icon("search")} <span data-i18n="location.form.searchButton"></span></button>
              </div>
              <p class="location-search__status" role="status" hidden></p>
              <ul class="location-search__results"></ul>
            </div>
            <p class="location-form__pick-hint" data-i18n="location.form.pickHint"></p>
            <leaflet-map pick locate class="location-form__map"${
              item?.location?.lat != null && item?.location?.lng != null
                ? ` lat="${escapeAttr(item.location.lat)}" lng="${escapeAttr(item.location.lng)}"`
                : ""
            }></leaflet-map>
          </form>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.links"></h2>
          <ul class="link-list"></ul>
          <div data-assist-field="links"></div>
          <form class="link-form">
            <input type="url" name="url" data-i18n-placeholder="item.detail.linkUrl" required />
            <input type="text" name="label" data-i18n-placeholder="item.detail.linkLabel" />
            <button type="submit" class="btn btn-secondary btn-row">${icon("plus")} <span data-i18n="item.detail.addLink"></span></button>
          </form>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.dates"></h2>
          <ul class="date-list"></ul>
          <form class="date-form">
            <input type="date" name="startDate" required data-i18n-aria-label="item.detail.startDate" />
            <input type="date" name="endDate" data-i18n-placeholder="item.detail.endDate" data-i18n-aria-label="item.detail.endDate" />
            <input type="text" name="label" data-i18n-placeholder="item.detail.dateLabel" />
            <button type="submit" class="btn btn-secondary btn-row">${icon("plus")} <span data-i18n="item.detail.addDate"></span></button>
          </form>
        </div>

        <div class="editor-card">
          <h2 data-i18n="item.detail.files"></h2>
          <div class="file-list-slot"></div>
        </div>

        <div class="editor-actions">
          <button class="btn btn-primary" data-action="save">${icon("check")} <span data-i18n="${item ? "common.save" : "location.editor.createButton"}"></span></button>
          <button class="btn btn-secondary" data-action="cancel">${icon("x")} <span data-i18n="common.cancel"></span></button>
        </div>

        ${
          item
            ? `
          <div class="editor-card">
            <h2 data-i18n="item.deleteHeading"></h2>
            <p class="editor-card__hint" data-i18n="item.deleteDescription"></p>
            <button class="btn btn-danger" data-action="delete">${icon("trash-2")} <span data-i18n="common.delete"></span></button>
          </div>
        `
            : ""
        }
      </div>
    `;
    translatePage(container);
    setHeading();

    itemForm = renderItemForm(container.querySelector(".item-form-slot"), item, { onSubmit: save });

    imageField = renderImageField(container.querySelector(".image-field-slot"), {
      tripId,
      imageUrl: item?.image_url,
      attachPath: item ? `/items/${item.id}/image` : undefined,
      // The title the user has already typed is usually the whole search, so
      // the picker opens with it filled in. Read at press time rather than
      // captured: on a new location it is typed after this card is rendered.
      searchSeed: () => container.querySelector('.item-form-slot [name="title"]')?.value ?? "",
      onChanged: (updated) => {
        if (item) {
          item.image_id = updated.image_id;
          item.image_url = updated.image_url;
        }
      },
      onStaged: (image) => {
        draft.image = image;
      },
    });

    renderLocationForm();
    renderAssistSlot();
    renderLinksList();
    bindLinkForm();
    renderDatesList();
    bindDateForm();
    renderFileList(container.querySelector(".file-list-slot"), item ? `/items/${item.id}/files` : null, {
      staged: draft.files,
      shared: isShared(trip),
    });

    container.querySelector('[data-action="save"]').addEventListener("click", save);
    container.querySelector('[data-action="cancel"]').addEventListener("click", cancel);

    // Guarded around the confirm too, not just the DELETE: the dialog is an
    // await like any other, and a second click while it is open would stack a
    // second copy of it.
    const deleteBtn = container.querySelector('[data-action="delete"]');
    if (deleteBtn) {
      guardClick(deleteBtn, async () => {
        if (!(await confirmDialog({ messageKey: "item.deleteConfirm" }))) return;
        await api.delete(`/items/${item.id}`);
        navigate(`/trips/${tripId}`);
      });
    }
  }

  // The assistant proposes; nothing here writes. Every accepted suggestion
  // lands in the same draft a keystroke would, so Save remains the only thing
  // that commits and the worst outcome of a bad suggestion is pressing Cancel.
  function renderAssistSlot() {
    // A run in flight would otherwise keep streaming into nodes this render
    // has just replaced.
    assistPanel?.destroy();
    assistPanel = renderAssistPanel(container.querySelector(".assist-slot"), {
      tripId,
      // Suggestions are placed into the [data-assist-field] slots scattered
      // through the page, so the panel needs the whole editor to find them --
      // they sit in the Basic info card, the Location card and the Links card.
      root: container,
      // Read live rather than captured: the point of enriching is to see what
      // is in front of the user, including anything typed and not yet saved.
      readCurrent: () => {
        const values = itemForm.readValues();
        return {
          title: values.title ?? "",
          // Empty while the select is still showing its default on a new
          // location: see isCategoryChosen. Sending "site" unchosen would make
          // every category suggestion claim to overwrite something.
          category: itemForm.isCategoryChosen() ? (values.category ?? "") : "",
          type: values.type ?? "",
          notes: values.notes ?? "",
          address: container.querySelector('.location-form [name="address"]').value,
          links: draft.links.map((l) => ({ url: l.url, label: l.label ?? "" })),
        };
      },
      applyField: (name, value) => {
        if (name === "address") {
          container.querySelector('.location-form [name="address"]').value = value;
          return;
        }
        itemForm.setValues({ [name]: value });
      },
      applyLink: (link) => {
        draft.links.push({ url: link.url, label: link.label || null });
        renderLinksList();
      },
      applyCoordinates: ({ lat, lng }) => setCoordinates?.({ lat, lng }),
      // The cover goes through the image field's own API, so accepting it is
      // the same operation as pasting a URL into that card -- including the
      // staging path on a location that does not exist yet. The provenance
      // travels with it: a freely licensed photograph stored without its
      // credit cannot be credited afterwards.
      applyCover: (cover) =>
        imageField?.setFromURL(cover.url, {
          source_url: cover.source_url,
          credit: cover.credit,
          license: cover.license,
        }),
    });
  }

  // The page's one and only write of the item itself: basic info plus the
  // nested location, links and dates, committed together server-side.
  //
  // Never called directly: `save` above is this behind the page's guard, and
  // that is what the button and both Enter handlers are given.
  async function commitSave() {
    itemForm.clearError();

    // show_on_map is a field of the item itself, not of its nested location,
    // even though its checkbox sits in the Location card - see readValues().
    const body = {
      ...itemForm.readValues(),
      show_on_map: container.querySelector('.location-form [name="showOnMap"]').checked,
      links: draft.links,
      dates: draft.dates,
    };

    // Absent means "leave it alone", so only send the key when there is
    // something to say: the typed coordinates, or explicit nulls to clear a
    // location the item already had. An untouched card on a location that
    // never had one sends nothing rather than creating an empty row.
    const location = readLocationForm();
    if (location) body.location = location;
    else if (item?.location) body.location = { lat: null, lng: null, address: null };

    // Create sends the whole location at once, edit sends the item alone.
    //
    // The asymmetry is not an oversight: on an existing location the cover and
    // the files are written the moment they are picked, through their own
    // endpoints, because there is already an item to attach them to. Only a
    // location that does not exist yet has to carry them along.
    let saved;
    try {
      saved = item
        ? await api.patch(`/items/${item.id}`, body)
        : await api.postForm(`/trips/${tripId}/items`, buildCreateForm(body));
    } catch (err) {
      // Nothing was created, so there is nothing to clean up and nothing to
      // adopt: the draft is still on the page and Save can simply be pressed
      // again. This used to be the sharp edge -- the item was created, the
      // cover failed, and because the page never learned it was now editing
      // rather than creating, the next Save made a second location.
      itemForm.showError(err.body?.error);
      return;
    }

    navigate(`/trips/${tripId}/locations/${saved.id}`);
  }

  // Everything a new location is made of, in one multipart body: the item as
  // JSON in an "item" part, the staged cover as either a file or a URL with
  // its provenance, and the staged files.
  //
  // The notes and visibilities are *positional* -- the nth file_note belongs
  // to the nth file -- which is what the server reads and the only ordering
  // multipart offers. So an empty string is appended rather than skipped when
  // a file has no note, or every later file would take the wrong one.
  function buildCreateForm(body) {
    const form = new FormData();
    form.append("item", JSON.stringify(body));

    if (draft.image?.kind === "file") {
      form.append("image", draft.image.file);
    } else if (draft.image?.kind === "url") {
      form.append("image_url", draft.image.url);
      // The provenance rides along, or a cover the assistant found is stored
      // with no record of whose photograph it is -- and unlike the image
      // itself, that cannot be recovered afterwards.
      const provenance = draft.image.provenance ?? {};
      if (provenance.source_url) form.append("source_url", provenance.source_url);
      if (provenance.credit) form.append("credit", provenance.credit);
      if (provenance.license) form.append("license", provenance.license);
    }

    for (const file of draft.files) {
      form.append("file", file.file);
      form.append("file_note", file.note ?? "");
      form.append("file_visibility", file.visibility ?? "");
    }
    return form;
  }

  function cancel() {
    if (draft.image?.kind === "file" && draft.image.previewUrl) URL.revokeObjectURL(draft.image.previewUrl);
    navigate(item ? `/trips/${tripId}/locations/${item.id}` : `/trips/${tripId}`);
  }

  // "Edit {title}" needs the item's title interpolated into the string,
  // but translatePage() only ever calls t(key) with no params (see
  // i18n.js) - so this bypasses data-i18n on the <h1> entirely and sets
  // its text directly. Called once after the initial render, and again
  // after Basic Info is saved so the heading picks up a changed title
  // without needing a full page reload.
  function setHeading() {
    container.querySelector(".page__header h1").textContent = item
      ? t("location.editor.editTitle", { title: item.title })
      : t("location.editor.newTitle");
  }

  // null when nothing was filled in, so an untouched Location card says
  // nothing about the item's location rather than clearing it.
  function readLocationForm() {
    const form = container.querySelector(".location-form");
    if (!form.lat.value && !form.lng.value && !form.address.value) return null;
    return {
      lat: form.lat.value ? Number(form.lat.value) : null,
      lng: form.lng.value ? Number(form.lng.value) : null,
      address: form.address.value || null,
    };
  }

  function renderLocationForm() {
    const form = container.querySelector(".location-form");
    if (item?.location) {
      form.lat.value = item.location.lat ?? "";
      form.lng.value = item.location.lng ?? "";
      form.address.value = item.location.address ?? "";
    }
    // Checked by default for a new location, matching the API's own default.
    if (item) form.showOnMap.checked = item.show_on_map;

    // "Show on map" only does anything once there are coordinates to show -
    // GET /trips/{id}/map filters on lat AND lng being present as well as on
    // this flag - and before Milestone 3 the checkbox sat in the Basic info
    // card, several cards above the fields it depends on. It's next to them
    // now, and says so when they're empty rather than silently doing nothing.
    // The box stays enabled either way: unchecking it isn't what's missing,
    // and the user's intent should survive until they fill the coordinates in.
    const hint = container.querySelector(".location-form__hint");
    const syncHint = () => {
      const hasCoordinates = Boolean(form.lat.value && form.lng.value);
      hint.hidden = hasCoordinates || !form.showOnMap.checked;
    };
    form.showOnMap.addEventListener("change", syncHint);

    // The picker and the number fields are two views of one value, and the
    // fields are the authoritative one: readLocationForm() still reads them
    // and nothing else, so the map cannot contribute to a save except by
    // writing here first.
    //
    // The initial coordinates are rendered straight onto the element in
    // render() rather than pushed from here, so an existing location's map
    // opens on its point instead of on the world view and then jumping.
    const picker = container.querySelector(".location-form__map");

    // Fields -> map. A blank field removes the attribute rather than setting
    // an empty one, because "no coordinate" and "the coordinate 0" have to
    // stay distinguishable (leaflet-map.js's readCoordinate makes the same
    // distinction on the other side).
    const syncMapFromFields = () => {
      for (const name of ["lat", "lng"]) {
        const value = form[name].value.trim();
        if (value === "") picker.removeAttribute(name);
        else picker.setAttribute(name, value);
      }
    };

    // Everything that reacts to the coordinates changing, in one place.
    //
    // There are five ways they change -- typing, a map click, the locate
    // button, choosing an address-search result, and resolving a pasted map
    // link -- and four of those write `form.lat.value` directly, which fires no
    // `input` event. So a listener is not enough and every writer has to say
    // so. Making that one call rather than three or four is what stops the next
    // writer from forgetting one of them, which is exactly what happened when
    // the reverse-geocoding button arrived: it watched the map events and the
    // input event, and stayed disabled after a search result filled the fields.
    const coordinateListeners = [];
    const onCoordinatesChanged = (fn) => coordinateListeners.push(fn);
    const coordinatesChanged = () => {
      syncMapFromFields();
      syncHint();
      for (const listener of coordinateListeners) listener();
    };

    // Map -> fields. No loop: setting the attributes above only moves the
    // marker, and location-picked is only ever emitted by a click or a drag.
    const takeCoordinates = ({ lat, lng }) => {
      form.lat.value = lat;
      form.lng.value = lng;
      coordinatesChanged();
    };
    setCoordinates = takeCoordinates;
    picker.addEventListener("location-picked", (e) => takeCoordinates(e.detail));
    // The locate control shows where the device is on any map; here that is
    // also the answer to "where is this place", which is the single most
    // useful case - standing somewhere and recording it. The trip map gets
    // the same button and ignores this event.
    picker.addEventListener("position-found", (e) => takeCoordinates(e.detail));

    for (const name of ["lat", "lng"]) {
      form[name].addEventListener("input", coordinatesChanged);
    }
    syncHint();
    bindPlaceSearch(form, coordinatesChanged);
    bindAddressLookup(form, onCoordinatesChanged);
    // The card has no button of its own any more - these values are read
    // back by save(). Enter in a coordinate field saves the page, via the
    // same submit-plus-keydown pair the Basic info card uses and for the same
    // reason (see location-form.js): the submit listener catches the native
    // reload, the keydown is what actually fires for a multi-field form with
    // no submit button.
    const saveFromForm = (e) => {
      e.preventDefault();
      save();
    };
    form.addEventListener("submit", saveFromForm);
    form.addEventListener("keydown", (e) => {
      if (e.key === "Enter") saveFromForm(e);
    });
  }

  // Address search: find a place by name instead of knowing where to click.
  //
  // Hidden entirely unless the server reports the capability - the endpoint
  // is disabled by setting CARAVEL_GEOCODER_URL empty, and a control that
  // can only answer "not enabled on this server" is worse than no control.
  //
  // It searches on Enter or on the button, never per keystroke: every query
  // costs an external service a request, and OSM's usage policy is the reason
  // this goes through our own endpoint at all (internal/httpapi/geocode.go).
  function bindPlaceSearch(form, coordinatesChanged) {
    if (!hasCapability("geocoding")) return;

    const panel = container.querySelector(".location-search");
    const input = form.placeQuery;
    const button = container.querySelector('[data-action="search-place"]');
    const status = container.querySelector(".location-search__status");
    const results = container.querySelector(".location-search__results");
    panel.hidden = false;

    const setStatus = (key, params) => {
      status.textContent = key ? t(key, params) : "";
      status.hidden = !key;
    };

    async function search() {
      const query = input.value.trim();
      results.innerHTML = "";
      // A pasted Google Maps link is not a search term, and sending it to
      // Nominatim as one finds nothing. The same field and the same button
      // handle both: what you have in your clipboard is somebody else's idea
      // of how to name a place, and asking the user to notice which kind it is
      // would be the app's problem becoming theirs.
      if (isMapLink(query)) return resolveMapLink(query);
      if (query.length < 2) {
        setStatus("location.form.searchTooShort");
        return;
      }
      setStatus("location.form.searching");
      button.disabled = true;
      let found;
      try {
        // The locale rides along on all three of these: it is the language to
        // name places in, and the app's language is a browser setting the
        // server cannot infer. Same reasoning as the assistant's own locale
        // field -- somebody running a German UI on an English system is the
        // ordinary case.
        found = await api.get(`/geocode?q=${encodeURIComponent(query)}&locale=${encodeURIComponent(getLocale())}`);
      } catch (err) {
        // The Go error is for the console; the user gets one sentence saying
        // the search is unavailable, not a status code.
        console.error(err);
        setStatus("location.form.searchFailed");
        return;
      } finally {
        button.disabled = false;
      }

      if (!found.length) {
        setStatus("location.form.searchNoResults");
        return;
      }
      setStatus(null);
      for (const place of found) {
        const li = document.createElement("li");
        const choose = document.createElement("button");
        choose.type = "button";
        choose.className = "location-search__result";
        choose.textContent = place.display_name;
        choose.addEventListener("click", () => {
          form.lat.value = place.lat;
          form.lng.value = place.lng;
          // Only fills an *empty* address: a result's display_name is a whole
          // formatted address, and overwriting something the user typed by
          // hand would lose their wording for the sake of tidiness.
          if (!form.address.value.trim()) form.address.value = place.display_name;
          coordinatesChanged();
          results.innerHTML = "";
          setStatus(null);
        });
        li.appendChild(choose);
        results.appendChild(li);
      }
    }

    // Recognising the link is deliberately loose: any http(s) URL on a Google
    // or goo.gl host. The server decides what it will actually follow (see
    // isMapLinkHost in internal/geocode), and being wrong here costs one 400
    // and a sentence, whereas being too strict means a link that works in the
    // browser and not in this field.
    function isMapLink(value) {
      let parsed;
      try {
        parsed = new URL(value);
      } catch {
        return false;
      }
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return false;
      const host = parsed.hostname.toLowerCase().replace(/\.$/, "");
      return host === "goo.gl" || host.endsWith(".goo.gl") || /(^|\.)google\.[a-z.]{2,7}$/.test(host);
    }

    // A link resolves to exactly one place, so it fills the fields rather than
    // offering a list -- there is nothing to choose between. The name the URL
    // carries is offered for the address the same way a search result's is:
    // only into an *empty* field, because it is a guess about what to call the
    // place rather than an address the user asked for.
    async function resolveMapLink(link) {
      setStatus("location.form.resolvingLink");
      button.disabled = true;
      let place;
      try {
        place = await api.get(`/geocode/link?url=${encodeURIComponent(link)}&locale=${encodeURIComponent(getLocale())}`);
      } catch (err) {
        console.error(err);
        // 404 means the link was followed and names no single place -- a
        // search results page. That is worth saying, because the user can go
        // back to Maps and pick the pin.
        setStatus(err?.status === 404 ? "location.form.linkNoPlace" : "location.form.linkFailed");
        return;
      } finally {
        button.disabled = false;
      }

      form.lat.value = place.lat;
      form.lng.value = place.lng;
      coordinatesChanged();

      // The name goes in the *title*, not the address -- it is the name of the
      // place ("Brandenburg Gate"), which is what a title is for, and putting
      // it where an address goes was simply wrong. The address is deliberately
      // left alone: the link does not carry one, and the only way to a real
      // address is the Look up address button a few fields down, which is one
      // press and now correctly enabled by the coordinates this just set.
      //
      // Nothing more is available from the link, and this was measured rather
      // than assumed: the expanded page is 219KB of JavaScript whose og: tags
      // read "Google Maps" and "Find local businesses", the street address
      // appears nowhere in the HTML, and the place id in the URL only becomes
      // an address through Google's paid API.
      //
      // Only into an empty title, like every other suggestion in this editor:
      // a name from a link does not get to overwrite what somebody typed.
      const named = place.display_name && !itemForm.readValues().title.trim();
      if (named) itemForm.setValues({ title: place.display_name });

      // The title lives in the card *above* this one, so on a phone it is off
      // screen -- which is why the message says what happened rather than
      // leaving the user to scroll up and find out.
      if (named) setStatus("location.form.linkResolvedTitled", { name: place.display_name });
      else setStatus("location.form.linkResolved");
      input.value = "";
    }

    button.addEventListener("click", search);
    // The card's own keydown handler treats Enter as "save the page"; in this
    // field Enter means "search", so it is stopped before it gets there.
    input.addEventListener("keydown", (e) => {
      if (e.key !== "Enter") return;
      e.preventDefault();
      e.stopPropagation();
      search();
    });
  }

  // Reverse geocoding: the coordinates you have, turned into an address you
  // may accept (Stage 22 Milestone 5).
  //
  // Three decisions, all of them deliberate:
  //
  // - **A button, not automatic.** A click on the map or a marker drag does not
  //   fire a lookup. Every query costs a volunteer-run service a request, which
  //   is the reason the place search above searches on Enter rather than per
  //   keystroke; a lookup per map click would break that rule quietly, and
  //   placing a pin takes several clicks to get right.
  // - **Offered, never applied.** The answer appears with an Accept button and
  //   the address field is not touched until it is pressed. An address is often
  //   hand-written and better than what a geocoder returns -- "Foss Hotel, room
  //   4" beats "Þórunnartún 1, 105 Reykjavík" for the person reading it later
  //   -- and overwriting that to be tidy is a loss. The place search already
  //   makes the same call, filling only an *empty* address field.
  // - **Hidden unless the server can do it.** Its own capability rather than
  //   `geocoding`, because the reverse endpoint is derived from the configured
  //   search URL and the derivation can fail (see geocode.ReverseURL): an
  //   instance can have working address search and no reverse lookup.
  function bindAddressLookup(form, onCoordinatesChanged) {
    if (!hasCapability("reverse_geocoding")) return;

    const panel = container.querySelector(".location-reverse");
    const button = container.querySelector('[data-action="lookup-address"]');
    const status = container.querySelector(".location-reverse__status");
    const offer = container.querySelector(".location-reverse__offer");
    const value = container.querySelector(".location-reverse__value");
    const accept = container.querySelector('[data-action="accept-address"]');
    panel.hidden = false;

    const setStatus = (key) => {
      status.textContent = key ? t(key) : "";
      status.hidden = !key;
    };
    const clearOffer = () => {
      offer.hidden = true;
      value.textContent = "";
    };

    // Nothing to look up without a point. Disabled rather than hidden, so the
    // control is visible as something that will work once there are
    // coordinates -- the same reasoning the "show on map" hint beside it uses.
    const syncEnabled = () => {
      button.disabled = !(form.lat.value.trim() && form.lng.value.trim());
    };
    // One subscription rather than a listener per way the coordinates can
    // change. The first version of this watched the input event and the two map
    // events, and so stayed disabled after an address-search result or a
    // resolved map link filled the fields -- both of which write them directly.
    onCoordinatesChanged(() => {
      syncEnabled();
      // A stale offer is worse than none: the address belonged to the old
      // point, and accepting it after moving the pin would file the wrong one.
      clearOffer();
      setStatus(null);
    });
    syncEnabled();

    // Guarded through busy.js rather than a local flag, like every other write
    // in this editor -- this one is a read, but it is a read that costs
    // somebody else a request, so a double press is worth dropping.
    const guarded = createGuard({ elements: button });
    button.addEventListener(
      "click",
      guarded.wrap(async () => {
        clearOffer();
        setStatus("location.form.lookingUp");
        const query = new URLSearchParams({
          lat: form.lat.value.trim(),
          lng: form.lng.value.trim(),
          locale: getLocale(),
        });
        let found;
        try {
          found = await api.get(`/geocode/reverse?${query}`);
        } catch (err) {
          // 404 is "there is no address there", which is an answer rather than
          // a failure and reads differently. Anything else is the service
          // being unreachable.
          console.error(err);
          setStatus(err?.status === 404 ? "location.form.lookupNothing" : "location.form.lookupFailed");
          return;
        }
        if (!found?.display_name) {
          setStatus("location.form.lookupNothing");
          return;
        }
        setStatus(null);
        value.textContent = found.display_name;
        offer.hidden = false;
        accept.focus();
      })
    );

    accept.addEventListener("click", () => {
      // Overwrites whatever is in the field, unlike the place search: pressing
      // Accept *is* the decision to use this address, and having asked for it
      // and been given it, having it silently not applied would be the
      // surprise.
      form.address.value = value.textContent;
      clearOffer();
      form.address.focus();
    });
  }

  // renderLinksList()/bindLinkForm() are split (rather than one combined
  // function) so the submit listener can be attached exactly once, from
  // render() below, instead of being re-attached on every add/delete - a
  // form.addEventListener() call inside a function that both handles
  // submit *and* gets re-invoked by its own handler stacks one more
  // listener on the same persistent <form> node every time, doubling on
  // each submit (1 -> 2 -> 4 -> ...). renderLinksList() itself stays safe
  // to call repeatedly: it only touches the <ul>, and the per-item delete
  // buttons it wires are freshly created nodes every time.
  function renderLinksList() {
    const list = container.querySelector(".link-list");
    list.innerHTML = draft.links.length
      ? draft.links
          .map(
            (l, i) =>
              `<li><a href="${escapeAttr(l.url)}" target="_blank" rel="noopener">${escapeHtml(l.label || l.url)}</a> <button class="icon-remove" data-action="delete-link" data-index="${i}" aria-label="${t("common.remove")}">${icon("x")}</button></li>`
          )
          .join("")
      : `<li class="empty">${t("item.detail.linksEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-link"]').forEach((btn) => {
      btn.addEventListener("click", () => {
        draft.links.splice(Number(btn.getAttribute("data-index")), 1);
        renderLinksList();
      });
    });
  }

  function bindLinkForm() {
    const form = container.querySelector(".link-form");
    form.addEventListener("submit", (e) => {
      e.preventDefault();
      draft.links.push({ url: form.url.value, label: form.label.value || null });
      form.reset();
      renderLinksList();
    });
  }

  // Same split as renderLinksList()/bindLinkForm() above, same reason.
  function renderDatesList() {
    const list = container.querySelector(".date-list");
    list.innerHTML = draft.dates.length
      ? draft.dates
          .map((d, i) => {
            const range = d.end_date ? `${escapeHtml(d.start_date || "")} – ${escapeHtml(d.end_date)}` : escapeHtml(d.start_date || "");
            return `<li>${range}${d.label ? " — " + escapeHtml(d.label) : ""} <button class="icon-remove" data-action="delete-date" data-index="${i}" aria-label="${t("common.remove")}">${icon("x")}</button></li>`;
          })
          .join("")
      : `<li class="empty">${t("item.detail.datesEmpty")}</li>`;

    list.querySelectorAll('[data-action="delete-date"]').forEach((btn) => {
      btn.addEventListener("click", () => {
        draft.dates.splice(Number(btn.getAttribute("data-index")), 1);
        renderDatesList();
      });
    });
  }

  function bindDateForm() {
    const form = container.querySelector(".date-form");
    form.addEventListener("submit", (e) => {
      e.preventDefault();
      draft.dates.push({ start_date: form.startDate.value, end_date: form.endDate.value || null, label: form.label.value || null });
      form.reset();
      renderDatesList();
    });
  }

  render();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}
