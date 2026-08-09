import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";

// Renders an upload form + list for the documents at `path` (either
// `/trips/{id}/documents` or `/items/{id}/documents` — both share the same
// list/upload/delete shape, see plan Section 3.3).
export async function renderDocumentList(container, path) {
  let docs = await api.get(path);

  function render() {
    container.innerHTML = `
      <div class="document-list">
        <form class="document-upload">
          <input type="file" name="file" required data-i18n-aria-label="common.uploadFile" />
          <button type="submit" data-i18n="documents.upload"></button>
        </form>
        <p class="documents-empty" data-i18n="documents.empty" hidden></p>
        <ul class="documents"></ul>
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

    container.querySelector(".document-upload").addEventListener("submit", async (e) => {
      e.preventDefault();
      const input = e.target.file;
      const file = input.files[0];
      if (!file) return;
      const formData = new FormData();
      formData.append("file", file);
      const res = await fetch(`/api${path}`, { method: "POST", body: formData, credentials: "same-origin" });
      const doc = await res.json();
      if (!res.ok) {
        window.alert(doc.error || t("common.error"));
        return;
      }
      docs.push(doc);
      render();
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
