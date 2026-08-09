import { api } from "../api.js";
import { t, translatePage } from "../i18n.js";
import "../components/item-card.js";
import { renderItemForm } from "../components/item-form.js";
import { renderItemDetail } from "../components/item-detail.js";

const CATEGORIES = ["location", "stay", "transport"];

export async function renderItemsTab(container, tripId) {
  let activeFilter = "all";

  container.innerHTML = `
    <div class="items-tab">
      <div class="items-tab__header">
        <div class="items-filter">
          <button data-filter="all" class="active" data-i18n="items.filter.all"></button>
          ${CATEGORIES.map((c) => `<button data-filter="${c}">${t(`item.category.${c}`)}</button>`).join("")}
        </div>
        <button data-action="new-item" data-i18n="items.new"></button>
      </div>
      <div class="item-form-slot"></div>
      <p class="items-empty" data-i18n="items.empty" hidden></p>
      <div class="item-list"></div>
      <div class="item-detail-slot"></div>
    </div>
  `;
  translatePage(container);

  const list = container.querySelector(".item-list");
  const emptyState = container.querySelector(".items-empty");
  const formSlot = container.querySelector(".item-form-slot");
  const detailSlot = container.querySelector(".item-detail-slot");

  async function load() {
    const query = activeFilter === "all" ? "" : `?category=${activeFilter}`;
    const items = await api.get(`/trips/${tripId}/items${query}`);
    list.innerHTML = "";
    emptyState.hidden = items.length > 0;
    for (const item of items) {
      const card = document.createElement("item-card");
      card.setAttribute("item-id", item.id);
      card.setAttribute("title", item.title);
      card.setAttribute("category", item.category);
      if (item.type) card.setAttribute("type", item.type);
      list.appendChild(card);
    }
  }

  container.querySelectorAll("[data-filter]").forEach((btn) => {
    btn.addEventListener("click", () => {
      activeFilter = btn.getAttribute("data-filter");
      container.querySelectorAll("[data-filter]").forEach((b) => b.classList.toggle("active", b === btn));
      load();
    });
  });

  container.querySelector('[data-action="new-item"]').addEventListener("click", () => {
    renderItemForm(formSlot, null, {
      tripId,
      onSaved: async () => {
        formSlot.innerHTML = "";
        await load();
      },
      onCancel: () => {
        formSlot.innerHTML = "";
      },
    });
  });

  list.addEventListener("item-open", async (e) => {
    await renderItemDetail(detailSlot, e.detail.itemId, {
      onClose: () => {
        detailSlot.innerHTML = "";
      },
      onDeleted: async () => {
        detailSlot.innerHTML = "";
        await load();
      },
    });
  });

  await load();
}
