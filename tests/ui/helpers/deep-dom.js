// Browser-side DOM helpers, as source strings.
//
// These run inside page.evaluate, so they can't be imported there — they're
// injected by concatenating DEEP_DOM_SOURCE into the evaluated function. Keeping
// them in one place matters because the shadow-piercing walk is the part every
// caller gets wrong: Caravel renders trip cards, location cards, the map and the
// menus into shadow roots, so a plain document.querySelectorAll misses exactly
// the elements most likely to be broken. Stage 07 found wrong heading levels
// only after adding the shadow walk.

export const DEEP_DOM_SOURCE = `
// Every element in the document in *document order*, descending into every open
// shadow root at the point where its host sits. Order is what makes a heading
// outline checkable, so this is a deliberate pre-order walk rather than a
// collect-then-concat.
function deepWalk(root, out) {
  out = out || [];
  const children = root.children || [];
  for (const el of children) {
    out.push(el);
    if (el.shadowRoot) deepWalk(el.shadowRoot, out);
    deepWalk(el, out);
  }
  return out;
}

function deepQueryAll(selector, root) {
  return deepWalk(root || document.documentElement).filter((el) => el.matches && el.matches(selector));
}

// Where an element lives, for failure messages: a shadow-root hit reported as a
// bare tag name is nearly impossible to find by hand.
function describeElement(el) {
  const parts = [];
  let node = el;
  while (node && node !== document.documentElement) {
    let step = node.localName || "?";
    if (node.id) step += "#" + node.id;
    else if (node.classList && node.classList.length) step += "." + [...node.classList].slice(0, 2).join(".");
    parts.unshift(step);
    const parent = node.parentNode;
    if (parent && parent.host) {
      parts.unshift("[shadow]");
      node = parent.host;
    } else {
      node = parent;
    }
  }
  return parts.join(" > ");
}
`;

// Resolves an element's accessible name the way a screen reader would, in
// precedence order. Deliberately a subset of the full accname algorithm — enough
// to answer "would this control be announced as anything at all?", which is the
// question Stage 07 was asking when it swept 157 controls.
export const ACCESSIBLE_NAME_SOURCE = `
function accessibleName(el) {
  const trim = (s) => (s || "").replace(/\\s+/g, " ").trim();

  if (el.getAttribute("aria-hidden") === "true") return { name: "", hidden: true };

  const ariaLabel = trim(el.getAttribute("aria-label"));
  if (ariaLabel) return { name: ariaLabel, from: "aria-label" };

  const labelledBy = el.getAttribute("aria-labelledby");
  if (labelledBy) {
    // IDREFs resolve within the element's own tree — the shadow root if it has
    // one, otherwise the document. Looking only at document breaks for the
    // components that render into shadow DOM.
    const scope = el.getRootNode();
    const text = labelledBy
      .split(/\\s+/)
      .map((id) => {
        const target = scope.getElementById ? scope.getElementById(id) : document.getElementById(id);
        return target ? trim(target.textContent) : "";
      })
      .filter(Boolean)
      .join(" ");
    if (text) return { name: text, from: "aria-labelledby" };
  }

  if (el.id) {
    const scope = el.getRootNode();
    const forLabel = (scope.querySelector || document.querySelector).call(
      scope,
      'label[for="' + CSS.escape(el.id) + '"]'
    );
    if (forLabel && trim(forLabel.textContent)) return { name: trim(forLabel.textContent), from: "label[for]" };
  }

  const wrapping = el.closest && el.closest("label");
  if (wrapping && trim(wrapping.textContent)) return { name: trim(wrapping.textContent), from: "wrapping label" };

  // Buttons are named by their content; inputs are not.
  if (el.localName === "button" || el.type === "submit" || el.type === "button") {
    const own = trim(el.textContent);
    if (own) return { name: own, from: "text content" };
    // An icon-only button often carries its label on an inner <use href="#id">
    // or a nested title element.
    const svgTitle = el.querySelector && el.querySelector("title");
    if (svgTitle && trim(svgTitle.textContent)) return { name: trim(svgTitle.textContent), from: "svg title" };
  }

  const placeholder = trim(el.getAttribute("placeholder"));
  if (placeholder) return { name: placeholder, from: "placeholder" };

  const title = trim(el.getAttribute("title"));
  if (title) return { name: title, from: "title" };

  const value = el.localName === "input" && (el.type === "submit" || el.type === "button") ? trim(el.value) : "";
  if (value) return { name: value, from: "value" };

  return { name: "", from: null };
}
`;
