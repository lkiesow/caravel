import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";

// Renders a read-only document list (filename, size, note, download link,
// delete) plus an "Add document" trigger that opens a native <dialog> for
// uploading - `<dialog>` covers showModal()/backdrop/focus-trapping/Escape
// natively, so no custom modal component is needed. The dialog supports
// picking multiple files at once and renders one note input per selected
// file, uploading each with its own note. `path` is either
// `/trips/{id}/documents` or `/items/{id}/documents` - both share the same
// list/upload/delete shape.
export async function renderDocumentList(container, path) {
  let docs = await api.get(path);

  function render() {
    container.innerHTML = `
      <div class="document-list">
        <button type="button" class="document-list__add" data-i18n="documents.add"></button>
        <p class="documents-empty" data-i18n="documents.empty" hidden></p>
        <ul class="documents"></ul>
        <dialog class="document-dialog">
          <form class="document-dialog__form" novalidate>
            <h3 data-i18n="documents.dialogTitle"></h3>
            <label class="document-dialog__picker">
              <span data-i18n="documents.chooseFiles"></span>
              <input type="file" name="files" multiple data-i18n-aria-label="common.uploadFile" />
            </label>
            <div class="document-dialog__files"></div>
            <p class="document-dialog__error" hidden></p>
            <div class="document-dialog__actions">
              <button type="submit" data-i18n="documents.upload"></button>
              <button type="button" data-action="cancel" data-i18n="common.cancel"></button>
            </div>
          </form>
        </dialog>
      </div>
    `;
    translatePage(container);

    const list = container.querySelector(".documents");
    const emptyState = container.querySelector(".documents-empty");
    emptyState.hidden = docs.length > 0;

    for (const doc of docs) {
      const li = document.createElement("li");
      li.innerHTML = `
        <a href="${doc.download_url}" target="_blank" rel="noopener">${escapeHtml(doc.filename)}</a>
        <span class="document-size">${formatSize(doc.size_bytes)}</span>
        ${doc.note ? `<span class="document-note">${escapeHtml(doc.note)}</span>` : ""}
        <button data-action="delete" data-id="${doc.id}" aria-label="${t("common.remove")}">&times;</button>
      `;
      li.querySelector('[data-action="delete"]').addEventListener("click", async () => {
        if (!window.confirm(t("documents.deleteConfirm"))) return;
        await api.delete(`/documents/${doc.id}`);
        docs = docs.filter((d) => d.id !== doc.id);
        render();
      });
      list.appendChild(li);
    }

    const dialog = container.querySelector(".document-dialog");
    const form = container.querySelector(".document-dialog__form");
    const fileInput = form.files;
    const filesContainer = container.querySelector(".document-dialog__files");
    const errorEl = container.querySelector(".document-dialog__error");

    container.querySelector(".document-list__add").addEventListener("click", () => {
      form.reset();
      filesContainer.innerHTML = "";
      errorEl.hidden = true;
      dialog.showModal();
    });

    container.querySelector('[data-action="cancel"]').addEventListener("click", () => dialog.close());

    fileInput.addEventListener("change", () => {
      filesContainer.innerHTML = Array.from(fileInput.files)
        .map(
          (file, i) => `
        <div class="document-dialog__file-row">
          <span class="document-dialog__filename">${escapeHtml(file.name)}</span>
          <input type="text" data-note-index="${i}" data-i18n-placeholder="documents.notePlaceholder" />
        </div>
      `
        )
        .join("");
      translatePage(filesContainer);
    });

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const files = Array.from(fileInput.files);
      if (!files.length) return;
      errorEl.hidden = true;

      const notes = files.map((_, i) => filesContainer.querySelector(`[data-note-index="${i}"]`).value || "");

      try {
        for (let i = 0; i < files.length; i++) {
          const formData = new FormData();
          formData.append("file", files[i]);
          if (notes[i]) formData.append("note", notes[i]);
          const res = await fetch(`/api${path}`, { method: "POST", body: formData, credentials: "same-origin" });
          const doc = await res.json();
          if (!res.ok) throw new Error(doc.error || t("common.error"));
          docs.push(doc);
        }
        dialog.close();
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
