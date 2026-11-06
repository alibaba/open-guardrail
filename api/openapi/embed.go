// Package openapi embeds the public service contract.
package openapi

import _ "embed"

//go:embed openapi.yaml
var Document []byte
