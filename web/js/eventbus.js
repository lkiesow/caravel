// Single shared event bus for cross-component notifications (e.g. "item
// saved" refreshing the map/itinerary/list) instead of a state-management
// layer. See plan section 4.2.
export const eventBus = new EventTarget();
