package metrochat

import "embed"

//go:embed all:webapp
var WebappFS embed.FS

//go:embed all:ooldchat-web/assets
var LandingAssetsFS embed.FS
