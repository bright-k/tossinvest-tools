// Package tossinvest embeds the immutable official contract snapshots.
package tossinvest

import _ "embed"

//go:embed openapi.json
var OpenAPI []byte

//go:embed asyncapi.json
var AsyncAPI []byte

//go:embed manifest.json
var Manifest []byte
