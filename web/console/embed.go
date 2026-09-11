// Package console embeds the production Console assets into the liteaig binary.
package console

import "embed"

//go:embed dist
var Assets embed.FS
