package httpapi

import (
	"net/http"
)

func (a *API) handleCoinToolPage(w http.ResponseWriter, r *http.Request) {
	serveEmbeddedFile(w, r, webappFS(), "coin-tool.html")
}
