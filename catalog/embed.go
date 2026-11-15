package catalogdata

import "embed"

// FS embeds the curated, SHA-256-indexed 1-click marketplace catalog.
//
//go:embed index.json *.json companions/*.json
var FS embed.FS
