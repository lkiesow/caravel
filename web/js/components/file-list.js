import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";
import { confirmDialog } from "./dialog.js";
import { renderLoading } from "./loading.js";

// Renders a read-only file list (filename, size, source, note, download
// link, delete) plus an inline single-file add row (file picker + optional
// note + Upload button) - the same file+note+button shape as the Links/
// Dates forms in the location editor, rather than the multi-file dialog
// this used to be. Selecting several files is still possible, just one
// upload at a time. `path` is either `/trips/{id}/files` or
// `/items/{id}/files` - both share the same list/upload/delete shape.
//
// The trip-level list mixes trip files with files attached to a location, so a
// row carrying `item_title` shows it (".file-source"): one flat list sorted
// by upload date, where only the location-attached rows are labelled. The API
// leaves item_title null on the item-level list, so the same template renders
// there unlabelled without needing a mode - on a location's own page every file
// belongs to that location and saying so on each row would be noise.
//
// Staging mode: pass `path: null` plus a `staged` array to collect picks
// in memory instead of uploading them. Nothing is fetched and nothing is
// POSTed; each pick is pushed as { file, note } and the list renders from
// the File objects (name + size). The location create page uses this so a
// brand-new location can carry files, then uploads them itself once
// the item ID exists - a multipart upload can't be part of the JSON create
// request, so post-create is the only option regardless (see todo.md).
export async function renderFileList(container, path, { staged } = {}) {
  const isStaging = !path;
  // Nothing is fetched in staging mode, so there is nothing to wait for.
  if (!isStaging) renderLoading(container);
  let files = isStaging ? [] : await api.get(path);

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
        <ul class="files"></ul>
        <p class="file-list-empty" data-i18n="files.empty" hidden></p>
        <p class="file-form__error" role="alert" hidden></p>
        <form class="file-form">
          <label class="image-field__upload">
            <span data-i18n="files.chooseFile"></span>
            <input type="file" name="file" hidden data-i18n-aria-label="common.uploadFile" />
          </label>
          <input type="text" name="note" data-i18n-placeholder="files.notePlaceholder" />
          <button type="submit" class="btn btn-primary btn-row">${isStaging ? `${icon("plus")} <span data-i18n="files.stage"></span>` : `${icon("upload")} <span data-i18n="files.upload"></span>`}</button>
        </form>
      </div>
    `;
    translatePage(container);

    const list = container.querySelector(".files");
    const emptyState = container.querySelector(".file-list-empty");
    const rows = isStaging ? staged : files;
    emptyState.hidden = rows.length > 0;

    rows.forEach((row, i) => {
      const li = document.createElement("li");
      // A staged row has no id, no URL and nothing uploaded yet, so it's
      // plain text with no download link, and removing it needs no
      // confirmation - it only drops a local pick.
      li.innerHTML = isStaging
        ? `
        <span>${escapeHtml(row.file.name)}</span>
        <span class="file-size">${formatSize(row.file.size)}</span>
        ${row.note ? `<span class="file-note">${escapeHtml(row.note)}</span>` : ""}
        <button class="icon-remove" data-action="delete" aria-label="${t("common.remove")}">${icon("x")}</button>
      `
        : `
        <a href="${row.download_url}" target="_blank" rel="noopener">${escapeHtml(row.filename)}</a>
        <span class="file-size">${formatSize(row.size_bytes)}</span>
        ${row.item_title ? `<span class="file-source">${escapeHtml(row.item_title)}</span>` : ""}
        ${row.note ? `<span class="file-note">${escapeHtml(row.note)}</span>` : ""}
        <button class="icon-remove" data-action="delete" data-id="${row.id}" aria-label="${t("common.remove")}">${icon("x")}</button>
      `;
      li.querySelector('[data-action="delete"]').addEventListener("click", async () => {
        if (isStaging) {
          staged.splice(i, 1);
          render();
          return;
        }
        if (!(await confirmDialog({ messageKey: "files.deleteConfirm" }))) return;
        await api.delete(`/files/${row.id}`);
        files = files.filter((d) => d.id !== row.id);
        render();
      });
      list.appendChild(li);
    });

    const form = container.querySelector(".file-form");
    const fileInput = form.file;
    const fileLabelText = form.querySelector(".image-field__upload span");
    const defaultFileLabel = fileLabelText.textContent;
    const errorEl = container.querySelector(".file-form__error");

    // Echoes the picked filename in place of the generic "Choose a file"
    // label, so there's feedback before hitting Upload - same idea as
    // image-field.js's preview, just text instead of an image.
    fileInput.addEventListener("change", () => {
      fileLabelText.textContent = fileInput.files[0]?.name || defaultFileLabel;
    });

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      errorEl.hidden = true;

      const file = fileInput.files[0];
      if (!file) {
        errorEl.textContent = t("files.noFile");
        errorEl.hidden = false;
        return;
      }

      if (isStaging) {
        staged.push({ file, note: form.note.value || null });
        render();
        return;
      }

      try {
        const formData = new FormData();
        formData.append("file", file);
        if (form.note.value) formData.append("note", form.note.value);
        const res = await fetch(`/api${path}`, { method: "POST", body: formData, credentials: "same-origin" });
        const created = await res.json();
        if (!res.ok) throw new Error(created.error || t("common.error"));
        files.push(created);
        render();
      } catch (err) {
        errorEl.textContent = err.message || t("common.error");
        errorEl.hidden = false;
      }
    });
  }

  render();
}

function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}
