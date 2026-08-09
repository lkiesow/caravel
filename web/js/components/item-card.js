const CATEGORY_COLORS = {
  location: "#16a34a",
  stay: "#7c3aed",
  transport: "#2563eb",
};

const styles = `
  :host {
    display: block;
    cursor: pointer;
  }
  :host(:focus-visible) {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
    border-radius: 0.5rem;
  }
  .card {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    border: 1px solid var(--color-border);
    border-radius: 0.5rem;
    padding: 0.75rem 1rem;
    background: var(--color-surface);
  }
  .card:hover {
    border-color: var(--color-accent);
  }
  .dot {
    width: 0.6rem;
    height: 0.6rem;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .text {
    flex: 1;
    min-width: 0;
  }
  h4 {
    margin: 0;
    font-size: 1rem;
  }
  .type {
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
`;

class ItemCard extends HTMLElement {
  static get observedAttributes() {
    return ["item-id", "title", "type", "category"];
  }

  connectedCallback() {
    if (!this.shadowRoot) this.attachShadow({ mode: "open" });
    if (!this.hasAttribute("tabindex")) this.setAttribute("tabindex", "0");
    this.setAttribute("role", "button");
    this.render();
    this.addEventListener("click", () => this.open());
    this.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        this.open();
      }
    });
  }

  open() {
    this.dispatchEvent(
      new CustomEvent("item-open", {
        bubbles: true,
        composed: true,
        detail: { itemId: this.getAttribute("item-id") },
      })
    );
  }

  attributeChangedCallback() {
    if (this.shadowRoot) this.render();
  }

  render() {
    const title = this.getAttribute("title") || "";
    const type = this.getAttribute("type") || "";
    const category = this.getAttribute("category") || "location";
    const color = CATEGORY_COLORS[category] || "#71717a";

    this.shadowRoot.innerHTML = `
      <style>${styles}</style>
      <div class="card">
        <span class="dot" style="background:${color}"></span>
        <div class="text">
          <h4>${escapeHtml(title)}</h4>
          ${type ? `<div class="type">${escapeHtml(type)}</div>` : ""}
        </div>
      </div>
    `;
  }
}

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

customElements.define("item-card", ItemCard);
