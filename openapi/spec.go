package openapi

import _ "embed"

// Spec is the versioned public API contract served by NexusAPI.
//
//go:embed openapi.yaml
var Spec []byte
