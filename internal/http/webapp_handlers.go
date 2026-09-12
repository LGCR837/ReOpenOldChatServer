package httpapi

import (
	"bytes"
	"io/fs"
	"net/http"
	"time"

	mc "metrochat"
)

// webappFS returns the embedded webapp/ filesystem as an fs.FS.
func webappFS() fs.FS {
	sub, err := fs.Sub(mc.WebappFS, "webapp")
	if err != nil {
		return nil
	}
	return sub
}

// landingAssetsFS returns the embedded ooldchat-web/assets/ filesystem as an fs.FS.
func landingAssetsFS() fs.FS {
	sub, err := fs.Sub(mc.LandingAssetsFS, "ooldchat-web/assets")
	if err != nil {
		return nil
	}
	return sub
}

// serveEmbeddedFile reads a file from the embedded FS and writes it to the response.
func serveEmbeddedFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (a *API) handleWebApp(w http.ResponseWriter, r *http.Request) {
	serveEmbeddedFile(w, r, webappFS(), "index.html")
}

func (a *API) handleWebAppLogin(w http.ResponseWriter, r *http.Request) {
	serveEmbeddedFile(w, r, webappFS(), "login.html")
}

func (a *API) handleHomePage(w http.ResponseWriter, r *http.Request) {
	serveEmbeddedFile(w, r, webappFS(), "landing.html")
}
