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
  /* Everything under the title: when the location is, what it is, and its
     tags. Stacked by default, because at 324px there is no room to do
     anything else and each part is short enough to read as its own line.

     Above 640px they run together as one row with separators, which is what
     the space is for: a card there is nearly full width, so three stacked
     lines of a few words each left most of it empty while making the card
     three times taller than it needed to be. The separators are drawn only in
     that layout, and only between parts that are actually present -- see the
     render below, which joins the parts it has rather than emitting empty
     slots. */
  .meta {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.125rem;
  }
  .meta__sep {
    display: none;
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
  @media (min-width: 641px) {
    .meta {
      flex-direction: row;
      align-items: center;
      flex-wrap: wrap;
      gap: 0.375rem;
    }
    .meta__sep {
      display: inline;
    }
  }
  /* The itinerary days this location is on. First in the meta row because
     "when" is the thing being scanned for on a planning screen -- the type and
     the tags say what it is, which the title usually already did. */
  .dates {
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
  /* Wrapping stays on even though the row is meant to be one line: the fit
     pass below decides which chips survive by asking the browser which ones
     wrapped, so it needs them free to wrap in order to measure that. What the
     reader sees is a single line, because everything past the first row gets
     the hidden attribute. */
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }
  /* Quiet on purpose: a card is a title with a picture, and the tags are there
     to be recognised at a glance, not read. Same muted palette as the dates. */
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
  .tag[hidden] {
    display: none;
  }
`;

class ItemCard extends HTMLElement {
  static get observedAttributes() {
    return ["item-id", "title", "category", "image-url", "tags", "dates"];
  }

  connectedCallback() {
    if (!this.shadowRoot) this.attachShadow({ mode: "open" });
    if (!this.hasAttribute("tabindex")) this.setAttribute("tabindex", "0");
    this.setAttribute("role", "button");
    this.render();
    // Re-solve the tag row whenever the card changes width: rotating the phone,
    // dragging a desktop window, or crossing the 641px breakpoint where the
    // meta row stops stacking. Hiding a chip changes the card height but never
    // its width, so this cannot feed itself.
    if (typeof ResizeObserver === "function") {
      this.fitObserver = new ResizeObserver(() => this.scheduleFit());
      this.fitObserver.observe(this);
    }
    this.addEventListener("click", () => this.open());
    this.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        this.open();
      }
    });
  }

  disconnectedCallback() {
    if (this.fitObserver) {
      this.fitObserver.disconnect();
      this.fitObserver = null;
    }
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
    // Every tag is emitted, plus a hidden overflow badge; which of them the
    // reader actually sees is decided after layout by fitTags(). A card is one
    // row in a list and one location with a dozen tags must not make its
    // neighbours look different -- but how many fit is a question about pixels,
    // not about a count, so it cannot be answered here. The whole set is on
    // the location page either way.

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

    // Joined rather than emitted as fixed slots, so a separator only ever
    // appears *between* two parts that exist -- a location with no dates must
    // not lead with a stray dot. aria-hidden on the separator because it is a
    // visual join: a screen reader already gets these as separate phrases, and
    // reading "middle dot" between them is noise.
    const metaParts = [
      firstRange
        ? `<span class="dates">${escapeHtml(firstRange)}${moreRanges > 0 ? ` +${moreRanges}` : ""}</span>`
        : "",
      tags.length
        ? `<span class="tags">${tags
            .map((tag) => `<span class="tag">${escapeHtml(tag)}</span>`)
            .join("")}<span class="tag tag--more" hidden></span></span>`
        : "",
    ].filter(Boolean);
    const meta = metaParts.length
      ? `<div class="meta">${metaParts.join('<span class="meta__sep" aria-hidden="true">·</span>')}</div>`
      : "";

    this.shadowRoot.innerHTML = `
      <style>${styles}</style>
      <div class="card">
        ${imageUrl ? `<img class="thumb" src="${escapeAttr(imageUrl)}" alt="" />` : ""}
        <span class="dot" style="background:${color}"></span>
        <div class="text">
          <h2>${escapeHtml(title)}</h2>
          ${meta}
        </div>
      </div>
    `;
    this.scheduleFit();
  }

  /* Trim the tag row to a single line, once the browser has laid it out.

     The old rule was a fixed slice(0, 3), which got it wrong in both
     directions: on a wide card a fourth short tag fitted easily but was
     replaced by a badge that took *more* room than the tag, and at 324px three
     tags already filled the line, so the badge whose job is to keep the card
     one row tall was the thing that added a second row. Whether a tag fits
     depends on the card width and on how long the tag is, and neither is
     visible from a count.

     So: let them wrap, ask which ones did, and hide those. */
  fitTags() {
    const wrap = this.shadowRoot && this.shadowRoot.querySelector(".tags");
    if (!wrap) return;
    const chips = Array.from(wrap.querySelectorAll(".tag:not(.tag--more)"));
    const badge = wrap.querySelector(".tag--more");
    if (!chips.length || !badge) return;

    // Start from everything-visible, or a previous pass's verdict becomes this
    // pass's input and the row can only ever shrink -- a card widened back up
    // would keep the tags it dropped while narrow.
    for (const chip of chips) chip.hidden = false;
    badge.hidden = true;

    // All reads, then all writes. Interleaving them would force a reflow per
    // chip, on every card in the list.
    const tops = chips.map((chip) => chip.getBoundingClientRect().top);
    const firstRow = Math.min(...tops);
    // A tolerance rather than equality: these are subpixel numbers, and chips
    // on one line can differ in the last fraction.
    let fits = tops.filter((top) => top < firstRow + 1).length;
    if (fits === chips.length) return; // nothing wrapped, so nothing to say

    // One surviving chip pays for the badge, which is never wider than a chip.
    // Keep at least one real tag: a lone "+4" tells the reader nothing.
    fits = Math.max(1, fits - 1);
    for (let i = fits; i < chips.length; i++) chips[i].hidden = true;
    badge.textContent = `+${chips.length - fits}`;
    badge.hidden = false;
  }

  /* Measuring text before the brand font has loaded measures the fallback, and
     the fallback is narrower -- so an early pass keeps a chip that does not
     actually fit. Wait for the font, then for a frame, so the chips we ask
     about have been laid out with it. */
  scheduleFit() {
    if (this.fitPending) return;
    this.fitPending = true;
    const run = () =>
      requestAnimationFrame(() => {
        this.fitPending = false;
        if (this.isConnected) this.fitTags();
      });
    if (document.fonts && document.fonts.status !== "loaded") {
      document.fonts.ready.then(run, run);
    } else {
      run();
    }
  }
}

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function escapeAttr(s) {
  return escapeHtml(s);
}

customElements.define("item-card", ItemCard);
