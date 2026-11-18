package edge

import _ "embed"

//go:embed assets/grex.js
var embeddedGrexJS []byte

//go:embed assets/tokens.css
var embeddedGrexTokensCSS []byte

//go:embed assets/grex.elements.json
var embeddedGrexElementsJSON []byte

// RenderGrexJS returns the frozen Grex JS v1.0.0 module bytes for the Grisp Welcome Page.
func RenderGrexJS() []byte {
	out := make([]byte, len(embeddedGrexJS))
	copy(out, embeddedGrexJS)
	return out
}

// RenderGrexTokensCSS returns the Grex JS v1.0.0 base design token stylesheet.
func RenderGrexTokensCSS() []byte {
	out := make([]byte, len(embeddedGrexTokensCSS))
	copy(out, embeddedGrexTokensCSS)
	return out
}

// RenderGrexElementsJSON returns the frozen Grex JS v1.0.0 20-element catalog JSON.
func RenderGrexElementsJSON() []byte {
	out := make([]byte, len(embeddedGrexElementsJSON))
	copy(out, embeddedGrexElementsJSON)
	return out
}
