import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { formatBytes } from "../format.js";
import { icon } from "../icon.js";
import { confirmDialog, promptDialog } from "./dialog.js";
import { renderMenu } from "./menu.js";
import { renderLoading } from "./loading.js";

// Renders a file list as cards (type tile, name, meta line, size, and a
// per-row overflow menu holding Edit note / Delete) plus a drop zone that
// accepts several files at once, dropped or browsed, and uploads them one at a
// time. `path` is either `/trips/{id}/files` or `/items/{id}/files` - both
// share the same list/upload/delete shape.
//
// The trip-level list mixes trip files with files attached to a location, so a
// row carrying `item_title` shows it (".file-card__source"): one flat list
// sorted by upload date, where only the location-attached rows are labelled. The API
// leaves item_title null on the item-level list, so the same template renders
// there unlabelled without needing a mode - on a location's own page every file
// belongs to that location and saying so on each row would be noise.
//
// Read-only mode: pass `readOnly: true` for a list with no add row and no
// per-row menu - the location *view* page, where editing lives behind its Edit
// button. That caller already has the rows (it fetches them to decide whether
// to render its card at all), so it passes them as `rows` and the component
// skips the request rather than asking for the same list twice.
//
// Staging mode: pass `path: null` plus a `staged` array to collect picks
// in memory instead of uploading them. Nothing is fetched and nothing is
// POSTed; each pick is pushed as { file, note } and the list renders from
// the File objects (name + size). The location create page uses this so a
// brand-new location can carry files, then uploads them itself once
// the item ID exists - a multipart upload can't be part of the JSON create
// request, so post-create is the only option regardless (see todo.md).
// Visibility (Stage 14 Milestone 7): pass `shared: true` when the trip has
// somebody else on it. That turns on the per-file personal/trip choice — a
// selector on the drop zone for new uploads and a radio group in each row's own
// menu afterwards. Left off, nothing about visibility is rendered at all: on a
// solo trip the distinction cannot mean anything, and offering it would be
// asking a question with only one possible answer.
//
// The value still travels on every upload either way; the server defaults it,
// and a trip that later gains a member finds its existing files already
// trip-visible.
export async function renderFileList(container, path, { staged, rows: given, readOnly = false, shared = false } = {}) {
  const isStaging = !path;
  // The visibility a new upload gets, remembered across a batch and across the
  // re-render that follows one — picking "personal" and then dropping three
  // files should not silently revert between them.
  let uploadVisibility = "trip";
  // Errors survive the re-render that shows them: render() rebuilds the whole
  // subtree, so the paragraph a handler wrote into is gone by the time the user
  // would read it.
  let errors = [];
  // Nothing is fetched in staging mode, and nothing when the caller already
  // has the rows (the location view page fetches them itself to decide whether
  // to render its card at all - refetching here would be a second request for
  // the same list).
  if (!isStaging && !given) renderLoading(container);
  let files = isStaging ? [] : given ?? (await api.get(path));

  // Full rebuild on every call, list and add-form together - not a
  // partial re-render that reuses a persistent form across calls. The
  // latter is exactly what caused a real duplicate-submit bug elsewhere
  // in this app (see location-editor-page.js's renderLinksList()/
  // bindLinkForm() split): a form.addEventListener() call inside a
  // function that's itself re-invoked by its own submit handler stacks
  // one more listener on the same node every time. A full rebuild
  // sidesteps that class of bug entirely, since the form node is never
  // reused across calls - every render() gets a fresh one with exactly
  // one listener.
  //
  // The file input is deliberately not `required`. It's hidden (the visible
  // control is the label around it), and a hidden invalid control can't be
  // focused to show a validation bubble - so the browser blocked the submit
  // event outright and the click did nothing at all, leaving only "The
  // invalid form control with name='file' is not focusable" in the console.
  // The submit handler below reports an empty pick instead.
  function render() {
    container.innerHTML = `
      <div class="file-list">
        <p class="file-list__summary" hidden></p>
        <ul class="files"></ul>
        <p class="file-list-empty" data-i18n="files.empty" hidden></p>
        ${
          readOnly
            ? ""
            : `
        <p class="file-list__error" role="alert" hidden></p>
        <label class="file-drop">
          <input type="file" name="file" multiple hidden data-i18n-aria-label="common.uploadFile" />
          <span class="file-drop__icon">${icon("upload")}</span>
          <span class="file-drop__text">
            <span class="file-drop__title file-drop__title--wide" data-i18n="files.dropTitle"></span>
            <span class="file-drop__title file-drop__title--narrow" data-i18n="files.dropTitleNarrow"></span>
            <span class="file-drop__hint file-drop__hint--wide" data-i18n="files.dropHint"></span>
            <span class="file-drop__hint file-drop__hint--narrow" data-i18n="files.dropHintNarrow"></span>
          </span>
          <span class="btn btn-secondary file-drop__browse" aria-hidden="true" data-i18n="files.browse"></span>
        </label>
        ${
          shared
            ? `<div class="file-visibility">
          <span class="file-visibility__label" id="file-visibility-label" data-i18n="files.visibility.label"></span>
          <div class="setting-choices" role="radiogroup" aria-labelledby="file-visibility-label">
            <label class="setting-choice">
              <input type="radio" name="uploadVisibility" value="trip" />
              <span data-i18n="files.visibility.trip"></span>
            </label>
            <label class="setting-choice">
              <input type="radio" name="uploadVisibility" value="personal" />
              <span data-i18n="files.visibility.personal"></span>
            </label>
          </div>
        </div>`
            : ""
        }
        `
        }
      </div>
    `;
    translatePage(container);

    const list = container.querySelector(".files");
    const emptyState = container.querySelector(".file-list-empty");
    const rows = isStaging ? staged : files;
    emptyState.hidden = rows.length > 0;

    // Count and total size, above the list. The heading itself belongs to the
    // caller (a tab, or an .editor-card), so this says "3 files - 459 KB"
    // rather than repeating the word the heading already carries. Plural via
    // t()'s count argument, so German gets its own two forms too.
    const summary = container.querySelector(".file-list__summary");
    summary.hidden = rows.length === 0;
    if (rows.length) {
      const total = rows.reduce((sum, row) => sum + (isStaging ? row.file.size : row.size_bytes), 0);
      summary.textContent = t("files.summary", { total: formatBytes(total) }, rows.length);
    }

    rows.forEach((row, i) => {
      // One view model for both modes: a staged pick is a File object
      // (name/size/type and nothing else), an uploaded one is an API row, and
      // every difference between them belongs here rather than in two copies
      // of the card template.
      const view = isStaging
        ? { filename: row.file.name, size: row.file.size, contentType: row.file.type, note: row.note, itemTitle: null, href: null, visibility: row.visibility || "trip", isMine: true }
        : { filename: row.filename, size: row.size_bytes, contentType: row.content_type, note: row.note, itemTitle: row.item_title, href: row.download_url, visibility: row.visibility, isMine: row.is_mine };

      // The note wins the title when there is one, and the filename drops to
      // the meta line. A note is the only readable name a file uploaded as
      // "5d2ffd5f-b621-41d9-9b10-173d5a72f860.png" will ever have, and it used
      // to render as a small italic afterthought behind exactly that string.
      const title = view.note ? escapeHtml(view.note) : filenameHtml(view.filename);

      const sep = (variant = "") => `<span class="file-card__sep${variant ? ` file-card__sep--${variant}` : ""}" aria-hidden="true">·</span>`;

      // The size appears twice in the markup and exactly once on screen: the
      // standalone column is hidden under 640px, this copy shown, and vice
      // versa above it. Two nodes rather than one because CSS can move a box
      // within its own flex parent but cannot move it into a different one,
      // and on a phone there is no room for a size column beside the name.
      // The inline size carries its own separator (hidden with it above 640px,
      // where the standalone size column takes over) - otherwise desktop showed
      // a stray leading dot in front of the filename.
      const metaHead =
        `<span class="file-card__size file-card__size--inline">${formatBytes(view.size)}</span>` + (view.note ? `${sep("size")}${filenameHtml(view.filename)}` : "");

      // Then the desktop-only "No note" hint, and the location this file is
      // attached to (trip-level lists only). Order matters for the punctuation:
      // every separator here has to have something visible in front of it at
      // its own breakpoint, or it strands a dot at the start of the line.
      //   mobile:  [size · filename] / [location on its own line]
      //   desktop: [filename or "No note" · location]  (size is the column)
      // So "No note" needs no separator of its own - it is only ever first -
      // while the location's is desktop-only, hidden where the location wraps.
      // A personal file says so, with a lock. Only rendered when the trip is
      // shared: on a solo trip every file is trip-visible and a badge saying so
      // on every row would be noise.
      const personal = shared && view.visibility === "personal";
      const metaTail =
        (view.note ? "" : `<span class="file-card__nonote">${escapeHtml(t("files.noNote"))}</span>`) +
        (view.itemTitle ? `${sep("source")}<span class="file-card__source">${escapeHtml(view.itemTitle)}</span>` : "") +
        (personal ? `${sep("visibility")}<span class="file-card__personal">${icon("lock", { className: "file-card__lock" })}${escapeHtml(t("files.personal"))}</span>` : "");

      const body = `
        <span class="file-card__tile">${icon(tileIconName(view.contentType))}</span>
        <span class="file-card__text">
          <span class="file-card__name">${title}</span>
          <span class="file-card__meta">${metaHead}${metaTail}</span>
        </span>
      `;

      const li = document.createElement("li");
      li.className = "file-card";
      // The whole card is the download link, which is also what clears the
      // 44px tap target without a min-height propping it up - the old row was
      // a bare filename link measuring 22px. A staged pick has nothing to
      // download yet, so there its body is a plain span.
      li.innerHTML = `
        ${view.href ? `<a class="file-card__body" href="${view.href}" target="_blank" rel="noopener">${body}</a>` : `<span class="file-card__body">${body}</span>`}
        <span class="file-card__size">${formatBytes(view.size)}</span>
        ${readOnly ? "" : `<span class="file-actions"></span>`}
      `;

      // One overflow menu per row rather than a bare delete icon: a note can
      // now be changed after upload (PATCH /api/files/{id}), so the row has
      // two actions, and two icons competing beside a filename is exactly the
      // pile-up this row already has too much of. renderMenu's action-item
      // mode exists for this - these are things the menu does, not a
      // selection it holds.
      if (readOnly) {
        list.appendChild(li);
        return;
      }
      renderMenu(li.querySelector(".file-actions"), {
        // Vertical, not the horizontal ellipsis the tab bar's "More" uses: this
        // one belongs to the row it sits in, and the vertical form is what a
        // per-row overflow reads as everywhere else.
        iconName: "ellipsis-vertical",
        chevron: false,
        triggerClass: "file-actions__trigger",
        ariaLabel: "files.actions",
        // Radio items and action items in one menu, which renderMenu supports:
        // visibility is a state the file is in, the other two are things the
        // menu does. The radio group appears only for the uploader's own files
        // on a shared trip — an editor may rename or delete a shared file, but
        // who reads someone else's document is not theirs to decide, and the
        // server refuses it too.
        activeValue: view.visibility,
        items: [
          ...(shared && view.isMine
            ? [
                { value: "trip", label: t("files.visibility.trip"), iconName: "users" },
                { value: "personal", label: t("files.visibility.personal"), iconName: "lock" },
              ]
            : []),
          { value: "note", label: t("files.editNote"), iconName: "pencil", action: true },
          { value: "delete", label: t(isStaging ? "common.remove" : "common.delete"), iconName: "trash-2", action: true, danger: true },
        ],
        onSelect: async (action) => {
          if (action === "trip" || action === "personal") {
            if (action === view.visibility) return;
            if (isStaging) {
              staged[i].visibility = action;
            } else {
              const updated = await api.put(`/files/${row.id}/visibility`, { visibility: action });
              files = files.map((f) => (f.id === updated.id ? updated : f));
            }
            render();
            return;
          }
          if (action === "note") {
            const note = await promptDialog({
              messageKey: "files.notePrompt",
              value: row.note || "",
              placeholderKey: "files.notePlaceholder",
            });
            // null means cancelled; "" means "clear it", which is a real
            // answer and has to reach the server.
            if (note === null) return;
            if (isStaging) {
              staged[i].note = note || null;
            } else {
              const updated = await api.patch(`/files/${row.id}`, { note });
              files = files.map((f) => (f.id === updated.id ? updated : f));
            }
            render();
            return;
          }

          if (isStaging) {
            staged.splice(i, 1);
            render();
            return;
          }
          if (!(await confirmDialog({ messageKey: "files.deleteConfirm" }))) return;
          await api.delete(`/files/${row.id}`);
          files = files.filter((f) => f.id !== row.id);
          render();
        },
      });
      list.appendChild(li);
    });

    // Read-only mode stops here: no add row, so nothing below this line has a
    // node to bind to.
    if (readOnly) return;

    const drop = container.querySelector(".file-drop");
    const fileInput = drop.querySelector('input[type="file"]');
    const errorEl = container.querySelector(".file-list__error");

    // The selector reflects the remembered choice and writes back to it. Not a
    // form field read at submit time: the drop zone has no submit, and a drop
    // gesture never touches this control.
    if (shared) {
      const current = container.querySelector(`[name="uploadVisibility"][value="${uploadVisibility}"]`);
      if (current) current.checked = true;
      container.querySelectorAll('[name="uploadVisibility"]').forEach((input) => {
        input.addEventListener("change", () => {
          if (input.checked) uploadVisibility = input.value;
        });
      });
    }

    if (errors.length) {
      // One line per file that failed, so a batch reports which of its members
      // was the problem instead of failing anonymously.
      errorEl.textContent = errors.join(" ");
      errorEl.hidden = false;
      errors = [];
    }

    // Picking is the trigger now - there is no Upload button left to press, so
    // the old "Choose a file first." case can't happen.
    fileInput.addEventListener("change", () => add(fileInput.files));

    // The drag half. dragover has to preventDefault on every event or the
    // browser navigates to the dropped file instead of handing it over, and
    // dragleave fires when the pointer crosses onto a *child* of the zone too,
    // hence the contains() guard - without it the highlight flickers off as
    // soon as the pointer reaches the icon or the label text.
    drop.addEventListener("dragover", (e) => {
      e.preventDefault();
      drop.classList.add("file-drop--over");
    });
    drop.addEventListener("dragleave", (e) => {
      if (!drop.contains(e.relatedTarget)) drop.classList.remove("file-drop--over");
    });
    drop.addEventListener("drop", (e) => {
      e.preventDefault();
      drop.classList.remove("file-drop--over");
      add(e.dataTransfer?.files);
    });

    // Everything a pick can be: one file or several, dropped or browsed,
    // staged locally or uploaded one at a time. Sequential rather than
    // parallel so a failure belongs to a named file, and so a dozen dropped
    // files don't open a dozen simultaneous requests.
    async function add(fileList) {
      const picked = [...(fileList || [])];
      if (!picked.length) return;
      errorEl.hidden = true;

      drop.setAttribute("aria-busy", "true");
      drop.classList.add("file-drop--busy");

      for (const file of picked) {
        // Checked here as well as by the server, which answers an oversized
        // upload with a 413 whose body is about multipart parsing rather than
        // about this file - see maxFileUploadBytes in internal/httpapi/files.go.
        if (file.size > MAX_UPLOAD_BYTES) {
          errors.push(t("files.tooLarge", { name: file.name, limit: formatBytes(MAX_UPLOAD_BYTES) }));
          continue;
        }
        // unshift, not push: the list is newest-first (the API sorts by
        // uploaded_at DESC), so appending put a new file at the *bottom* until
        // the next load moved it to the top. Found by the spec below, which
        // asserted the order before and after a reload.
        if (isStaging) {
          staged.unshift({ file, note: null, visibility: uploadVisibility });
          continue;
        }
        try {
          const formData = new FormData();
          formData.append("file", file);
          formData.append("visibility", uploadVisibility);
          const res = await fetch(`/api${path}`, { method: "POST", body: formData, credentials: "same-origin" });
          const created = await res.json();
          if (!res.ok) throw new Error(created.error || t("common.error"));
          files.unshift(created);
        } catch (err) {
          errors.push(`${file.name}: ${err.message || t("common.error")}`);
        }
      }

      // One re-render for the whole batch, which also replaces the file input -
      // so picking the same file twice in a row still fires `change` the second
      // time, and any errors collected above are printed by the next render().
      render();
    }
  }

  render();
}

// The server's own limit (maxFileUploadBytes in internal/httpapi/files.go).
// Duplicated deliberately: the alternative is asking the API for it, and a
// number that has to match on both sides is better as a named constant with a
// pointer at the other one than as an extra round trip.
const MAX_UPLOAD_BYTES = 50 * 1024 * 1024;

// Which tile a file gets, from the content type the server sniffed on upload
// (never the client-declared one - see internal/httpapi/files.go). Three
// buckets, because that is as much as a 36px tile can say: a picture, a
// document, or something else.
function tileIconName(contentType) {
  const type = (contentType || "").split(";")[0].trim();
  if (type.startsWith("image/")) return "image";
  if (type.startsWith("text/") || type === "application/pdf") return "file-text";
  return "file";
}

// A filename split into stem and extension, so the stem can ellipsize while
// the extension stays visible: "5d2ffd5f-b621-4... .png" rather than
// "5d2ffd5f-b621-41d9-9b10-173..." with the one part that says what the file
// *is* cut off. CSS-only (the stem gets overflow: hidden, the extension
// flex: none), so there is no measuring and no resize listener.
function filenameHtml(filename) {
  const dot = filename.lastIndexOf(".");
  const hasExt = dot > 0 && dot < filename.length - 1;
  const stem = hasExt ? filename.slice(0, dot) : filename;
  const ext = hasExt ? filename.slice(dot) : "";
  return `<span class="file-card__filename"><span class="file-card__stem">${escapeHtml(stem)}</span>${ext ? `<span class="file-card__ext">${escapeHtml(ext)}</span>` : ""}</span>`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
