// Returns markup for a Lucide icon from the sprite at web/icons/lucide-sprite.svg.
// Referencing the sprite by file path (not a same-document #id) is required for
// icons used inside Shadow DOM components (trip-card, location-card): a <use>
// pointing at a <symbol> elsewhere in the same document doesn't pierce shadow
// boundaries, but a <use> pointing at an external file is a resource fetch and
// works identically inside or outside a shadow root.
export function icon(name, { className = "" } = {}) {
  return `<svg class="icon ${className}" aria-hidden="true"><use href="/icons/lucide-sprite.svg#lucide-${name}"></use></svg>`;
}
