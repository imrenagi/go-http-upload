package v3

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

const (
	TusResumableHeader = "Tus-Resumable"
	TusExtensionHeader = "Tus-Extension"
	TusVersionHeader   = "Tus-Version"
	TusMaxSizeHeader   = "Tus-Max-Size"

	TusVersion              = "1.0.0"
	TusMaxSize              = int64(1073741824)
	UploadOffsetHeader      = "Upload-Offset"
	UploadLengthHeader      = "Upload-Length"
	UploadMetadataHeader    = "Upload-Metadata"
	UploadDeferLengthHeader = "Upload-Defer-Length"
	UploadExpiresHeader     = "Upload-Expires"
	ContentTypeHeader       = "Content-Type"

	UploadMaxDuration = 10 * time.Minute
)

var (
	SupportedTusExtensions = []string{
		"creation",
		"expiration",
	}
	SupportedTusVersion = []string{
		"1.0.0",
	}
)

func NewController() Controller {
	return Controller{
		store: NewStore(),
	}
}

type Controller struct {
	store *Store
}

func TusVersionCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(TusResumableHeader) != TusVersion {
			w.WriteHeader(http.StatusPreconditionFailed)
			w.Write([]byte("Tus Version mistmatch"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TusVersionInjections(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodOptions {
			w.Header().Set(TusResumableHeader, TusVersion)
		}
		next.ServeHTTP(w, r)
	})
}

func (c *Controller) GetConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", "no-store")
		w.Header().Set(TusResumableHeader, TusVersion)
		w.Header().Add(TusVersionHeader, strings.Join(SupportedTusVersion, ","))
		w.Header().Add(TusExtensionHeader, strings.Join(SupportedTusExtensions, ","))
		w.Header().Add(TusMaxSizeHeader, fmt.Sprint(TusMaxSize))
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("GetConfig"))
	}
}

func (c *Controller) GetOffset() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		fileID := vars["file_id"]
		log.Debug().Str("file_id", fileID).Msg("Check request path and query")
		fm, ok := c.store.Find(fileID)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("File not found"))
			return
		}

		if fm.ExpiresAt.Before(time.Now()) {
			w.WriteHeader(http.StatusGone)
			w.Write([]byte("File expired"))
			return
		}

		w.Header().Add(UploadOffsetHeader, fmt.Sprint(fm.UploadedSize))
		w.Header().Add(UploadLengthHeader, fmt.Sprint(fm.TotalSize))
		w.Header().Add("Cache-Control", "no-store")
		if fm.Metadata != "" {
			w.Header().Add(UploadMetadataHeader, fm.Metadata)
		}
		if !fm.ExpiresAt.IsZero() {
			w.Header().Add(UploadExpiresHeader, uploadExpiresAt(fm.ExpiresAt))
		}
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("GetOffset"))
	}
}

func (c *Controller) ResumeUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		fileID := vars["file_id"]
		log.Debug().Str("file_id", fileID).Msg("Check request path and query")

		uploadOffset := r.Header.Get(UploadOffsetHeader)
		offset, err := strconv.ParseInt(uploadOffset, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid Upload-Offset header"))
			return
		}
		if offset < 0 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid Upload-Offset header"))
			return
		}

		contentLength := r.Header.Get("Content-Length")
		length, err := strconv.ParseInt(contentLength, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid Content-Length header"))
			return
		}

		contentType := r.Header.Get(ContentTypeHeader)
		log.Debug().Str("upload_offset", uploadOffset).
			Str("content_type", contentType).
			Msg("Check request header")

		if contentType != "application/offset+octet-stream" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			w.Write([]byte("only application/offset+octet-stream is supported"))
			return
		}

		fm, ok := c.store.Find(fileID)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("File not found"))
			return
		}

		if fm.ExpiresAt.Before(time.Now()) {
			w.WriteHeader(http.StatusGone)
			w.Write([]byte("File expired"))
			return
		}

		if offset != fm.UploadedSize {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Upload-Offset header does not match the current offset"))
			return
		}

		file := r.Body
		defer file.Close()

		f, err := os.OpenFile(filepath.Join("/tmp", fm.ID.String()), os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Error Retrieving the File"))
			return
		}
		defer f.Close()

		_, err = f.Seek(offset, 0)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Error Seeking the File"))
			return
		}

		n, err := io.Copy(f, file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Error writing file"))
			return
		}

		log.Debug().
			Int64("written_size", n).
			Str("stored_file", f.Name()).
			Msg("File Uploaded")

		fm.UploadedSize += length
		c.store.Save(fm.ID.String(), fm)

		w.Header().Add(UploadOffsetHeader, fmt.Sprint(fm.UploadedSize))
		if !fm.ExpiresAt.IsZero() {
			w.Header().Add(UploadExpiresHeader, uploadExpiresAt(fm.ExpiresAt))
		}
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("ResumeUpload"))
	}
}

func (c *Controller) CreateUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uploadDeferLength := r.Header.Get(UploadDeferLengthHeader)
		if uploadDeferLength != "" && uploadDeferLength != "1" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid Upload-Defer-Length header"))
			return
		}

		isDeferLength := uploadDeferLength == "1"
		if isDeferLength {
			w.WriteHeader(http.StatusNotImplemented)
			w.Write([]byte("Upload-Defer-Length is not implemented"))
			return
		}

		totalLength := r.Header.Get(UploadLengthHeader)
		totalSize, err := strconv.ParseInt(totalLength, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid Upload-Length header"))
			return
		}
		if totalSize < 0 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid Upload-Length header"))
			return
		}

		if totalSize > TusMaxSize {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			w.Write([]byte("Upload-Length exceeds the maximum size"))
		}

		uploadMetadata := r.Header.Get(UploadMetadataHeader)
		log.Debug().Str("upload_metadata", uploadMetadata).Msg("Check request header")

		fm := FileMetadata{
			ID:        uuid.New(),
			TotalSize: totalSize,
			Metadata:  uploadMetadata,
			ExpiresAt: time.Now().Add(UploadMaxDuration),
		}
		c.store.Save(fm.ID.String(), fm)

		w.Header().Add("Location", fmt.Sprintf("/files/%s", fm.ID))
		if !fm.ExpiresAt.IsZero() {
			w.Header().Add(UploadExpiresHeader, uploadExpiresAt(fm.ExpiresAt))
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("CreateUpload"))
	}
}

func uploadExpiresAt(t time.Time) string {	
	return t.Format("Mon, 02 Jan 2006 15:04:05 GMT")
}
