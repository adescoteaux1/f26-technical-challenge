// Package site embeds the static documentation pages served alongside the
// Control Tower API: a landing page linking to both challenges, and the shared
// stylesheet. The challenge spec page itself is rendered from CHALLENGE.md
// at request time (see internal/api/pages.go), not embedded here, so
// editing that file takes effect on the next request with no rebuild.
package site

import (
	"embed"
	_ "embed"
)

// Assets holds the design exports referenced by the challenge specs. Served
// under /site/assets/ so the same relative markdown path resolves both on the
// rendered page and when the .md is read directly on GitHub.
//
//go:embed assets/*.png
var Assets embed.FS

//go:embed index.html
var IndexHTML string

//go:embed style.css
var StyleCSS string

//go:embed apply.html
var ApplyHTML string

//go:embed no-copy.js
var NoCopyJS string
