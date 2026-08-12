import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import { icon } from "../icon.js";

// Renders a read-only document list (filename, size, note, download link,
// delete) plus an inline single-file add row (file picker + optional
// note + Upload button) - the same file+note+button shape as the Links/
// Dates forms in the location editor, rather than the multi-file dialog
// this used to be. Selecting several files is still possible, just one
// upload at a time. `path` is either `/trips/{id}/documents` or
// `/items/{id}/documents` - both share the same list/upload/delete shape.
//
// Staging mode: pass `path: null` plus a `staged` array to collect picks
// in memory instead of uploading them. Nothing is fetched and nothing is
// POSTed; each pick is pushed as { file, note } and the list renders from
// the File objects (name + size). The location create page uses this so a
// brand-new location can carry documents, then uploads them itself once
// the item ID exists - a multipart upload can't be part of the JSON create
// request, so post-create is the only option regardless (see todo.md).
export async function renderDocumentList(container, path, { staged } = {}) {
  const isStaging = !path;
  let docs = isStaging ? [] : await api.get(path);

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
  function render() {
    container.innerHTML = `
      <div class="document-list">
        <ul class="documents"></ul>
        <p class="documents-empty" data-i18n="documents.empty" hidden></p>
        <p class="document-form__error" hidden></p>
        <form class="document-form">
          <label class="image-field__upload">
            <span data-i18n="documents.chooseFile"></span>
            <input type="file" name="file" hidden required data-i18n-aria-label="common.uploadFile" />
          </label>
          <input type="text" name="note" data-i18n-placeholder="documents.notePlaceholder" />
          <button type="submit" class="btn btn-primary btn-collapse">${isStaging ? `${icon("plus")} <span data-i18n="documents.stage"></span>` : `${icon("upload")} <span data-i18n="documents.upload"></span>`}</button>
        </form>
      </div>
    `;
    translatePage(container);

    const list = container.querySelector(".documents");
    const emptyState = container.querySelector(".documents-empty");
    const rows = isStaging ? staged : docs;
    emptyState.hidden = rows.length > 0;

    rows.forEach((row, i) => {
      const li = document.createElement("li");
      // A staged row has no id, no URL and nothing uploaded yet, so it's
      // plain text with no download link, and removing it needs no
      // confirmation - it only drops a local pick.
      li.innerHTML = isStaging
        ? `
        <span>${escapeHtml(row.file.name)}</span>
        <span class="document-size">${formatSize(row.file.size)}</span>
        ${row.note ? `<span class="document-note">${escapeHtml(row.note)}</span>` : ""}
        <button class="icon-remove" data-action="delete" aria-label="${t("common.remove")}">${icon("x")}</button>
      `
        : `
        <a href="${row.download_url}" target="_blank" rel="noopener">${escapeHtml(row.filename)}</a>
        <span class="document-size">${formatSize(row.size_bytes)}</span>
        ${row.note ? `<span class="document-note">${escapeHtml(row.note)}</span>` : ""}
        <button class="icon-remove" data-action="delete" data-id="${row.id}" aria-label="${t("common.remove")}">${icon("x")}</button>
      `;
      li.querySelector('[data-action="delete"]').addEventListener("click", async () => {
        if (isStaging) {
          staged.splice(i, 1);
          render();
          return;
        }
        if (!window.confirm(t("documents.deleteConfirm"))) return;
        await api.delete(`/documents/${row.id}`);
        docs = docs.filter((d) => d.id !== row.id);
        render();
      });
      list.appendChild(li);
    });

    const form = container.querySelector(".document-form");
    const fileInput = form.file;
    const fileLabelText = form.querySelector(".image-field__upload span");
    const defaultFileLabel = fileLabelText.textContent;
    const errorEl = container.querySelector(".document-form__error");

    // Echoes the picked filename in place of the generic "Choose a file"
    // label, so there's feedback before hitting Upload - same idea as
    // image-field.js's preview, just text instead of an image.
    fileInput.addEventListener("change", () => {
      fileLabelText.textContent = fileInput.files[0]?.name || defaultFileLabel;
    });

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const file = fileInput.files[0];
      if (!file) return;
      errorEl.hidden = true;

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
        const doc = await res.json();
        if (!res.ok) throw new Error(doc.error || t("common.error"));
        docs.push(doc);
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
