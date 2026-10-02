package docs

import "embed"

//go:embed openapi.yaml swagger-ui/*
var Files embed.FS
