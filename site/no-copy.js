// Blocks right-click, text selection, and copy/cut on this site's static
// and rendered pages. Deliberately does not touch inputs/textareas/
// contenteditable elements — the /apply form still needs a selectable,
// pasteable username field. Links are exempt too: applicants legitimately
// need to copy resource URLs and the docs/API links out of the spec pages,
// and blocking that adds friction without discouraging copying the actual
// spec text. This is friction, not real protection: page source, browser
// dev tools, and reader mode all still expose the text.
(function () {
  function isExempt(el) {
    return !!el && (
      el.tagName === "INPUT" ||
      el.tagName === "TEXTAREA" ||
      el.isContentEditable ||
      (el.closest && el.closest("a"))
    );
  }

  ["contextmenu", "copy", "cut", "selectstart"].forEach(function (type) {
    document.addEventListener(type, function (e) {
      if (!isExempt(e.target)) e.preventDefault();
    });
  });
})();
