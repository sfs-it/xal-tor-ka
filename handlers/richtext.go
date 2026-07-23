// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 SFS.it di Zanutto Agostino

package handlers

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
)

// richHTML renders an admin-provided service description (Markdown) into HTML that is
// SAFE to embed in a listing card. Pipeline: Markdown → HTML (goldmark) → sanitize
// (bluemonday UGC policy: strips scripts/handlers/unknown tags, keeps basic formatting +
// links). The input is admin-authored but still sanitized (defence in depth). Empty → "".
func richHTML(md string) template.HTML {
	if md == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(md), &buf); err != nil {
		return ""
	}
	return template.HTML(listingSanitizer.SanitizeBytes(buf.Bytes()))
}

var listingSanitizer = bluemonday.UGCPolicy()

// --- Listing preview image (per-backend, stored under <data>/listing-img/<id>.<ext>) ---

const maxListingImg = 2 << 20 // 2 MiB

func (s *Server) listingImgDir() string {
	return filepath.Join(filepath.Dir(s.SetupPath), "listing-img")
}

// saveListingImage handles the optional preview-image upload of the service edit form.
// Returns (newFilename, remove, err): remove=true when the admin ticked «rimuovi»;
// newFilename set when a valid file was uploaded ("" = keep existing). Validates size and
// type (png/jpeg/webp/gif) — never trusts the client extension.
func (s *Server) saveListingImage(r *http.Request, id string) (string, bool, error) {
	if r.FormValue("img_remove") == "1" {
		return "", true, nil
	}
	file, _, err := r.FormFile("image")
	if err == http.ErrMissingFile {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxListingImg+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > maxListingImg {
		return "", false, errListingImg("immagine troppo grande (max 2 MB)")
	}
	ext := map[string]string{
		"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif",
	}[http.DetectContentType(data)]
	if ext == "" {
		return "", false, errListingImg("formato immagine non supportato (usa PNG/JPEG/WebP/GIF)")
	}
	dir := s.listingImgDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	// drop any previous file for this id (extension may differ), then write the new one
	if olds, _ := filepath.Glob(filepath.Join(dir, id+".*")); len(olds) > 0 {
		for _, o := range olds {
			_ = os.Remove(o)
		}
	}
	name := id + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", false, err
	}
	return name, false, nil
}

func (s *Server) removeListingImage(name string) {
	if name != "" {
		_ = os.Remove(filepath.Join(s.listingImgDir(), name))
	}
}

// handleListingImg serves a backend's preview image. Gated behind a session (same visibility
// as /listing). Serves the stored file for the backend id; 404 if absent.
func (s *Server) handleListingImg(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.session(r); !ok {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("id")
	for _, be := range s.Resolver.Backends() {
		if be.ID == id && be.Image != "" {
			w.Header().Set("Cache-Control", "private, max-age=300")
			http.ServeFile(w, r, filepath.Join(s.listingImgDir(), be.Image))
			return
		}
	}
	http.NotFound(w, r)
}

type errListingImg string

func (e errListingImg) Error() string { return string(e) }
