import { formatDateRange } from "../format.js";

const CATEGORY_COLORS = {
  site: "#16a34a",
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
  .thumb {
    width: 2.5rem;
    height: 2.5rem;
    border-radius: 0.375rem;
    object-fit: cover;
    flex-shrink: 0;
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
  h2 {
    margin: 0;
    font-size: 1rem;
  }
  .type {
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
  /* The itinerary days this location is on. Directly under the title because
     "when" is the thing being scanned for on a planning screen -- the type and
     the tags say what it is, which the title usually already did. */
  .dates {
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
    margin-top: 0.25rem;
  }
  /* Quiet on purpose: a card is a title with a picture, and the tags are there
     to be recognised at a glance, not read. Same muted palette as .type. */
  .tag {
    font-size: 0.7rem;
    line-height: 1;
    padding: 0.2rem 0.4rem;
    border-radius: 0.75rem;
    background: var(--color-surface);
    color: var(--color-text-muted);
    border: 1px solid var(--color-border);
    white-space: nowrap;
    max-width: 10rem;
    overflow: hidden;
    text-overflow: ellipsis;
  }
`;

class ItemCard extends HTMLElement {
  static get observedAttributes() {
    return ["item-id", "title", "type", "category", "image-url", "tags", "dates"];
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
    const category = this.getAttribute("category") || "site";
    const color = CATEGORY_COLORS[category] || "#71717a";
    const imageUrl = this.getAttribute("image-url");
    // JSON rather than a separator, because a tag may contain anything --
    // including whatever separator would have been picked.
    let tags = [];
    try {
      tags = JSON.parse(this.getAttribute("tags") || "[]");
    } catch {
      tags = [];
    }
    // At most three, then a count. A card is a fixed-height row in a list, and
    // one location with a dozen tags must not make its neighbours look
    // different; the whole set is on the location page.
    const shown = tags.slice(0, 3);
    const overflow = tags.length - shown.length;

    // Same JSON-in-an-attribute treatment as the tags, and the same reason for
    // showing only the first: a location split across three separate stretches
    // of the trip would otherwise turn one card into a paragraph. The whole set
    // is on the location page.
    let dates = [];
    try {
      dates = JSON.parse(this.getAttribute("dates") || "[]");
    } catch {
      dates = [];
    }
    const firstRange = dates.length ? formatDateRange(dates[0].start_date, dates[0].end_date) : null;
    const moreRanges = dates.length - 1;

    this.shadowRoot.innerHTML = `
      <style>${styles}</style>
      <div class="card">
        ${imageUrl ? `<img class="thumb" src="${escapeAttr(imageUrl)}" alt="" />` : ""}
        <span class="dot" style="background:${color}"></span>
        <div class="text">
          <h2>${escapeHtml(title)}</h2>
          ${
            firstRange
              ? `<div class="dates">${escapeHtml(firstRange)}${moreRanges > 0 ? ` +${moreRanges}` : ""}</div>`
              : ""
          }
          ${type ? `<div class="type">${escapeHtml(type)}</div>` : ""}
          ${
            tags.length
              ? `<div class="tags">${shown.map((tag) => `<span class="tag">${escapeHtml(tag)}</span>`).join("")}${
                  overflow ? `<span class="tag">+${overflow}</span>` : ""
                }</div>`
              : ""
          }
        </div>
      </div>
    `;
  }
}

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}

customElements.define("item-card", ItemCard);
