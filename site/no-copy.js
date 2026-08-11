// Blocks right-click, text selection, and copy/cut on this site's static
// and rendered pages. Deliberately does not touch inputs/textareas/
// contenteditable elements — the /apply form still needs a selectable,
// pasteable username field. This is friction, not real protection: page
// source, browser dev tools, and reader mode all still expose the text.
(function () {
  function isEditable(el) {
    return !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
  }

  ["contextmenu", "copy", "cut", "selectstart"].forEach(function (type) {
    document.addEventListener(type, function (e) {
      if (!isEditable(e.target)) e.preventDefault();
    });
  });
})();
