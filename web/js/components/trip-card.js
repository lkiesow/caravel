import { formatDateRange } from "../format.js";

const styles = `
  :host {
    display: block;
    height: 100%;
    cursor: pointer;
  }
  :host(:focus-visible) {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
    border-radius: 0.5rem;
  }
  /* .trip-grid's grid items stretch to the row height by default (grid's
     align-items: stretch), but that stretch only reaches :host - nothing
     here propagated it into the shadow tree, so two cards in the same row
     ended up different heights whenever one had a .dates line and the
     other didn't. height:100% + this flex column carries the stretched
     height all the way to .body, which is the piece that actually needs
     to absorb the difference. */
  .card {
    height: 100%;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--color-border);
    border-radius: 0.5rem;
    overflow: hidden;
    background: var(--color-surface);
  }
  .card:hover {
    border-color: var(--color-accent);
  }
  .thumb {
    display: block;
    width: 100%;
    height: 8rem;
    object-fit: cover;
  }
  .body {
    flex: 1;
    padding: 1rem;
  }
  /* Explicit size because this is an h2 for document-structure reasons (the
     page's h1 is "Your trips"), not because a card title should be rendered
     at h2 scale. 1.17rem is what the UA gave the h3 this used to be, so the
     card looks exactly as before. */
  h2 {
    margin: 0 0 0.25rem;
    font-size: 1.17rem;
  }
  .dates {
    color: var(--color-text-muted);
    font-size: 0.875rem;
  }
`;

class TripCard extends HTMLElement {
  static get observedAttributes() {
    return ["trip-id", "title", "start-date", "end-date", "image-url"];
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
      new CustomEvent("trip-open", {
        bubbles: true,
        composed: true,
        detail: { tripId: this.getAttribute("trip-id") },
      })
    );
  }

  attributeChangedCallback() {
    if (this.shadowRoot) this.render();
  }

  render() {
    const title = this.getAttribute("title") || "";
    const imageUrl = this.getAttribute("image-url");
    // Same formatter as the trip header the card links to, so the list and
    // the page it opens don't disagree about how a date looks. It also
    // handles a trip with only one bound set, which the previous inline
    // interpolation rendered as a dangling "2026-08-20 – ".
    const dateRange = formatDateRange(this.getAttribute("start-date"), this.getAttribute("end-date"));

    this.shadowRoot.innerHTML = `
      <style>${styles}</style>
      <div class="card">
        ${imageUrl ? `<img class="thumb" src="${escapeAttr(imageUrl)}" alt="" />` : ""}
        <div class="body">
          <h2>${escapeHtml(title)}</h2>
          ${dateRange ? `<div class="dates">${escapeHtml(dateRange)}</div>` : ""}
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

customElements.define("trip-card", TripCard);
