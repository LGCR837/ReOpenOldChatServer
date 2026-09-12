package httpapi

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "image/gif"
)

const (
	musicCoverProxyMaxEdge      = 480
	musicCoverProxyJpegQuality  = 72
	musicCoverProxyMaxInputSize = 4 << 20
)

func (a *API) musicCoverProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "/v1/uploads/media/") {
		return raw
	}
	name := strings.TrimSpace(strings.TrimPrefix(raw, "/v1/uploads/media/"))
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return raw
	}
	return "/v1/music/cover/" + name
}

func (a *API) handleMusicCoverProxy(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/music/cover/"))
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	path := filepath.Join(a.cfg.UploadDir, "media", name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if info.Size() > musicCoverProxyMaxInputSize {
		http.ServeFile(w, r, path)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer f.Close()

	reader := io.LimitReader(f, musicCoverProxyMaxInputSize+1)
	raw, err := io.ReadAll(reader)
	if err != nil || int64(len(raw)) <= 0 || int64(len(raw)) > musicCoverProxyMaxInputSize {
		http.ServeFile(w, r, path)
		return
	}

	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		http.ServeFile(w, r, path)
		return
	}

	bounds := img.Bounds()
	w0 := bounds.Dx()
	h0 := bounds.Dy()
	if w0 <= 0 || h0 <= 0 {
		http.ServeFile(w, r, path)
		return
	}

	resized := img
	if w0 > musicCoverProxyMaxEdge || h0 > musicCoverProxyMaxEdge {
		nw, nh := fitInside(w0, h0, musicCoverProxyMaxEdge, musicCoverProxyMaxEdge)
		if nw > 0 && nh > 0 {
			resized = resizeNearest(img, nw, nh)
		}
	}

	var out bytes.Buffer
	encodeErr := jpeg.Encode(&out, resized, &jpeg.Options{Quality: musicCoverProxyJpegQuality})
	if encodeErr != nil {
		if strings.EqualFold(format, "png") {
			out.Reset()
			if err := png.Encode(&out, resized); err != nil {
				http.ServeFile(w, r, path)
				return
			}
			w.Header().Set("Content-Type", "image/png")
		} else {
			http.ServeFile(w, r, path)
			return
		}
	} else {
		w.Header().Set("Content-Type", "image/jpeg")
	}

	w.Header().Set("Cache-Control", "public, max-age=604800")
	_, _ = w.Write(out.Bytes())
}

func fitInside(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 || maxW <= 0 || maxH <= 0 {
		return 0, 0
	}
	scaleW := float64(maxW) / float64(w)
	scaleH := float64(maxH) / float64(h)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}
	if scale >= 1 {
		return w, h
	}
	nw := int(float64(w) * scale)
	nh := int(float64(h) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	return nw, nh
}

func resizeNearest(src image.Image, nw, nh int) image.Image {
	if src == nil || nw <= 0 || nh <= 0 {
		return src
	}
	sb := src.Bounds()
	sw := sb.Dx()
	sh := sb.Dy()
	if sw <= 0 || sh <= 0 {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := sb.Min.Y + (y*sh)/nh
		for x := 0; x < nw; x++ {
			sx := sb.Min.X + (x*sw)/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
