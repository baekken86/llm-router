package llmrouter

import "embed"

//go:embed web/dist
var WebDistFS embed.FS

//go:embed data/models.json
var ModelsJSON []byte
