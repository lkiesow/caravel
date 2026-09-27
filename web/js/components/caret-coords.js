// Where the caret is inside a textarea, in pixels.
//
// A textarea will not say: there is no API for it, and Selection/Range see a
// textarea as one opaque node. The standard workaround is to render the text
// *before* the caret into a hidden div styled to wrap exactly as the field
// does, end it with a marker span, and ask the span where it landed.
//
// Only the caller of this file knows what to do with the answer; the numbers
// come back relative to the textarea's own border box, so a caller positioning
// something inside the field's offset parent adds el.offsetTop/offsetLeft.
//
// The measurement is redone on every call and never cached. The app serves a
// subsetted Montserrat, so a value taken before the webfont lands would be
// wrong for the rest of the session -- and the fields this is used on grow as
// they are typed into, which moves the answer anyway.

// Everything that affects where a glyph ends up. Not `width` or `box-sizing`:
// getComputedStyle reports the *content* width whatever the box-sizing is, so
// copying both would make a border-box field's mirror narrower than the field
// by its padding and wrap in the wrong places. offsetWidth is used instead,
// which is unambiguous.
const COPIED = [
  "border-top-width",
  "border-right-width",
  "border-bottom-width",
  "border-left-width",
  "padding-top",
  "padding-right",
  "padding-bottom",
  "padding-left",
  "font-family",
  "font-size",
  "font-weight",
  "font-style",
  "font-variant",
  "letter-spacing",
  "word-spacing",
  "line-height",
  "text-transform",
  "text-indent",
  "text-align",
  "direction",
  "tab-size",
];

export function caretCoords(el) {
  const cs = getComputedStyle(el);
  const mirror = document.createElement("div");
  for (const prop of COPIED) mirror.style.setProperty(prop, cs.getPropertyValue(prop));
  mirror.style.boxSizing = "border-box";
  mirror.style.width = `${el.offsetWidth}px`;
  // A textarea both wraps and preserves whitespace. The mirror has to do both
  // or the line count diverges the moment somebody types two spaces.
  mirror.style.whiteSpace = "pre-wrap";
  mirror.style.overflowWrap = "break-word";
  mirror.style.wordBreak = cs.wordBreak;
  mirror.style.position = "absolute";
  mirror.style.top = "0";
  mirror.style.left = "0";
  mirror.style.height = "auto";
  mirror.style.visibility = "hidden";
  mirror.style.pointerEvents = "none";

  const caret = el.selectionStart;
  mirror.textContent = el.value.slice(0, caret);
  const marker = document.createElement("span");
  // A zero-width space, so a caret sitting right after a newline still gets a
  // box with a position rather than an empty inline with none.
  marker.textContent = "​";
  mirror.appendChild(marker);

  el.parentNode.appendChild(mirror);
  // offsetTop is measured from the offsetParent's padding edge -- the mirror
  // is the offsetParent, being the only positioned ancestor -- so the borders
  // go back on to make this a border-box offset, matching the field's. Same
  // arithmetic the two autoGrow functions do for the same reason.
  const top = marker.offsetTop + parseFloat(cs.borderTopWidth) - el.scrollTop;
  const left = marker.offsetLeft + parseFloat(cs.borderLeftWidth) - el.scrollLeft;
  // `line-height: normal` parses as NaN. 1.2 is the usual normal.
  const lineHeight = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.2;
  mirror.remove();

  return { top, left, lineHeight };
}
