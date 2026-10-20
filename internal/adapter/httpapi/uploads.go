package httpapi

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/upload"
)

type FileView struct {
	FileID       string  `json:"fileId"`
	Filename     string  `json:"filename"`
	ContentType  string  `json:"contentType"`
	SizeBytes    int64   `json:"sizeBytes"`
	OriginalURL  string  `json:"originalUrl"`
	ThumbnailURL *string `json:"thumbnailUrl"`
}

func fileView(file d.File) FileView {
	base := "/api/files/" + file.ID
	v := FileView{FileID: file.ID, Filename: file.Filename, ContentType: file.Original.ContentType, SizeBytes: file.Original.SizeBytes, OriginalURL: base + "/original"}
	if file.Thumbnail != nil {
		url := base + "/thumbnail"
		v.ThumbnailURL = &url
	}
	return v
}

func (h *Handler) registerUploads(mux *routes) {
	mux.HandleFunc("POST /api/upload", h.uploadFile)
	mux.HandleFunc("GET /api/files/{fileId}", func(w http.ResponseWriter, r *http.Request) {
		file, err := h.Files.ForActor(actor(r)).File(r.Context(), r.PathValue("fileId"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusOK, fileView(file))
	})
	mux.HandleFunc("GET /api/files/{fileId}/original", h.serveUploadedFile)
	mux.HandleFunc("GET /api/files/{fileId}/thumbnail", h.serveUploadedFile)
}

func (h *Handler) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, d.MaxFileBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, fault.New("REQUEST_TOO_LARGE", "上传文件上限 128 MiB"))
		} else {
			fail(w, fault.ErrInvalid)
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	for name, files := range r.MultipartForm.File {
		if name != "file" || len(files) != 1 {
			fail(w, fault.ErrInvalid)
			return
		}
	}
	for name, values := range r.MultipartForm.Value {
		if len(values) != 1 || (name != "thumbnail" && name != "thumbnailMaxWidth" && name != "thumbnailMaxHeight") {
			fail(w, fault.ErrInvalid)
			return
		}
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, fault.ErrInvalid)
		return
	}
	defer file.Close()
	var options *ports.ThumbnailOptions
	if flag := r.FormValue("thumbnail"); flag != "" {
		if flag != "true" && flag != "false" {
			fail(w, fault.ErrInvalid)
			return
		}
		if flag == "true" {
			options = &ports.ThumbnailOptions{}
		}
	}
	for name, value := range map[string]string{"thumbnailMaxWidth": r.FormValue("thumbnailMaxWidth"), "thumbnailMaxHeight": r.FormValue("thumbnailMaxHeight")} {
		if value == "" {
			continue
		}
		n, err := strconv.Atoi(value)
		if options == nil || err != nil || n < 1 || n > 4096 {
			fail(w, fault.ErrInvalid)
			return
		}
		if name == "thumbnailMaxWidth" {
			options.MaxWidth = n
		} else {
			options.MaxHeight = n
		}
	}
	raw, err := io.ReadAll(io.LimitReader(file, d.MaxFileBytes+1))
	if err != nil {
		fail(w, err)
		return
	}
	stored, err := h.Files.ForActor(actor(r)).Upload(r.Context(), app.Input{Filename: header.Filename, ContentType: header.Header.Get("Content-Type"), Data: raw, Thumbnail: options})
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusCreated, fileView(stored))
}

func (h *Handler) serveUploadedFile(w http.ResponseWriter, r *http.Request) {
	thumbnail := strings.HasSuffix(r.URL.Path, "/thumbnail")
	file, raw, err := h.Files.ForActor(actor(r)).Read(r.Context(), r.PathValue("fileId"), thumbnail)
	if err != nil {
		fail(w, err)
		return
	}
	kind, name := file.Original.ContentType, file.Filename
	disposition := "attachment"
	if thumbnail {
		kind, name, disposition = "image/webp", "thumbnail.webp", "inline"
	} else {
		// 仅被动栅格图片/视频格式允许内联展示；任意 HTML/SVG 与不透明文档
		// 一律按下载处理，不在应用源上执行。
		switch kind {
		case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", "video/mp4", "video/webm":
			disposition = "inline"
		}
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeContent(w, r, name, file.CreatedAt, bytes.NewReader(raw))
}
