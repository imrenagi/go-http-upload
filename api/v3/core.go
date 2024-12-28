package v3

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	TusResumableHeader         = "Tus-Resumable"
	TusExtensionHeader         = "Tus-Extension"
	TusVersionHeader           = "Tus-Version"
	TusMaxSizeHeader           = "Tus-Max-Size"
	TusChecksumAlgorithmHeader = "Tus-Checksum-Algorithm"

	TusVersion              = "1.0.0"
	UploadOffsetHeader      = "Upload-Offset"
	UploadLengthHeader      = "Upload-Length"
	UploadMetadataHeader    = "Upload-Metadata"
	UploadDeferLengthHeader = "Upload-Defer-Length"
	UploadExpiresHeader     = "Upload-Expires"
	UploadChecksumHeader    = "Upload-Checksum"
	ContentTypeHeader       = "Content-Type"

	UploadMaxDuration = 10 * time.Minute
)

type Extension string

const (
	CreationExtension      Extension = "creation"
	ExpirationExtension    Extension = "expiration"
	ChecksumExtension      Extension = "checksum"
	TerminationExtension   Extension = "termination"
	ConcatenationExtension Extension = "concatenation"
)

type Extensions []Extension

func (e Extensions) Enabled(ext Extension) bool {
	for _, v := range e {
		if v == ext {
			return true
		}
	}
	return false
}

func (e Extensions) String() string {
	var s []string
	for _, v := range e {
		s = append(s, string(v))
	}
	return strings.Join(s, ",")
}

var (
	defaultMaxSize             = uint64(0)
	defaultSupportedExtensions = Extensions{
		CreationExtension,
		ExpirationExtension,
		ChecksumExtension,
	}
	SupportedTusVersion = []string{
		"0.2.0",
		"1.0.0",
	}
	SupportedChecksumAlgorithms = []string{
		"md5",
	}
)

type Options struct {
	Extensions Extensions
	MaxSize    uint64
}

type Option func(*Options)

func WithExtensions(extensions Extensions) Option {
	return func(o *Options) {
		o.Extensions = extensions
	}
}

func WithMaxSize(size uint64) Option {
	return func(o *Options) {
		o.MaxSize = size
	}
}

func NewController(s Storage, opts ...Option) Controller {
	o := Options{
		Extensions: defaultSupportedExtensions,
		MaxSize:    defaultMaxSize,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return Controller{
		store:      s,
		extensions: o.Extensions,
		maxSize:    o.MaxSize,
	}
}

type Storage interface {
	Find(id string) (FileMetadata, bool)
	Save(id string, metadata FileMetadata)
}

type Controller struct {
	store      Storage
	extensions Extensions
	maxSize    uint64
}

func TusResumableHeaderCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get(TusResumableHeader) == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Tus-Resumable header is missing"))
			return
		}

		tusVersion := r.Header.Get(TusResumableHeader)
		supported := false
		for _, version := range SupportedTusVersion {
			if tusVersion == version {
				supported = true
				break
			}
		}
		if !supported {
			w.WriteHeader(http.StatusPreconditionFailed)
			w.Write([]byte("Tus version not supported"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TusResumableHeaderInjections(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodOptions {
			w.Header().Set(TusResumableHeader, TusVersion)
		}
		next.ServeHTTP(w, r)
	})
}

func (c *Controller) GetConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add(TusVersionHeader, strings.Join(SupportedTusVersion, ","))
		if len(c.extensions) > 0 {
			w.Header().Add(TusExtensionHeader, c.extensions.String())
		}
		if c.maxSize != 0 {
			w.Header().Add(TusMaxSizeHeader, fmt.Sprint(c.maxSize))
		}
		if c.extensions.Enabled(ChecksumExtension) {
			w.Header().Add(TusChecksumAlgorithmHeader, strings.Join(SupportedChecksumAlgorithms, ","))
		}
		w.WriteHeader(http.StatusNoContent)
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

		if !fm.ExpiresAt.IsZero() && fm.ExpiresAt.Before(time.Now()) {
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
	}
}

func newChecksum(value string) (checksum, error) {
	if value == "" {
		return checksum{}, nil
	}
	d := strings.Split(value, " ")
	if len(d) != 2 {
		return checksum{}, fmt.Errorf("invalid checksum format")
	}
	if d[0] != "md5" {
		return checksum{}, fmt.Errorf("unsupported checksum algorithm")
	}
	return checksum{
		Algorithm: d[0],
		Value:     d[1],
	}, nil
}

type checksum struct {
	Algorithm string
	Value     string
}

func (c checksum) equal(file io.Reader) (bool, error) {
	hash, err := c.calculateChecksum(file)
	if err != nil {
		return false, err
	}
	return hash == c.Value, nil
}

func (c checksum) calculateChecksum(file io.Reader) (string, error) {
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (c *Controller) ResumeUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		fileID := vars["file_id"]
		log.Debug().Str("file_id", fileID).Msg("Check request path and query")

		var checksum checksum
		if c.extensions.Enabled(ChecksumExtension) {
			var err error
			checksum, err = newChecksum(r.Header.Get(UploadChecksumHeader))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(err.Error()))
				return
			}
		}

		uploadOffset := r.Header.Get(UploadOffsetHeader)
		offset, err := strconv.ParseInt(uploadOffset, 10, 64)
		if err != nil {
			log.Debug().Err(err).
				Str("upload_offset", uploadOffset).
				Msg("Invalid Upload-Offset header: not a number")
			writeError(w, http.StatusBadRequest, errors.New("invalid Upload-Offset header: not a number"))
			return
		}
		if offset < 0 {
			log.Debug().Str("upload_offset", uploadOffset).Msg("Invalid Upload-Offset header: negative value")
			writeError(w, http.StatusBadRequest, errors.New("invalid Upload-Offset header: negative value"))
			return
		}

		contentType := r.Header.Get(ContentTypeHeader)
		log.Debug().Str("upload_offset", uploadOffset).
			Str("content_type", contentType).
			Msg("Check request header")

		if contentType != "application/offset+octet-stream" {
			log.Debug().Str("content_type", contentType).Msg("Invalid Content-Type")
			writeError(w, http.StatusUnsupportedMediaType, errors.New("invalid Content-Type header: expected application/offset+octet-stream"))
			return
		}

		fm, ok := c.store.Find(fileID)
		if !ok {
			log.Debug().Str("file_id", fileID).Msg("file not found")
			writeError(w, http.StatusNotFound, errors.New("file not found"))
			return
		}

		if c.extensions.Enabled(ExpirationExtension) && fm.ExpiresAt.Before(time.Now()) {
			w.WriteHeader(http.StatusGone)
			w.Write([]byte("File expired"))
			return
		}

		if offset != fm.UploadedSize {
			log.Debug().Msg("upload-Offset header does not match the current offset")
			writeError(w, http.StatusConflict, errors.New("upload-Offset header does not match the current offset"))
			return
		}

		// Create a copy of the request body using TeeReader
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			log.Error().Err(err).Msg("Error copying the request body")
			writeError(w, http.StatusInternalServerError, errors.New("error copying the request body"))
			return
		}
		rd1 := io.NopCloser(bytes.NewBuffer(buf))
		rd2 := io.NopCloser(bytes.NewBuffer(buf))
		defer r.Body.Close()
		defer rd1.Close()
		defer rd2.Close()

		if c.extensions.Enabled(ChecksumExtension) && checksum.Algorithm != "" {
			ok, err := checksum.equal(rd1)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Error calculating checksum"))
				return
			}
			if !ok {
				w.WriteHeader(http.StatusBadRequest) // checksum mismatch
				w.Write([]byte("Checksum mismatch"))
				return
			}
		}

		f, err := os.OpenFile(filepath.Join("/tmp", fm.ID), os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Error().Err(err).Msg("error opening the file")
			writeError(w, http.StatusBadRequest, errors.New("error opening the file"))
			return
		}
		defer f.Close()

		_, err = f.Seek(offset, 0)
		if err != nil {
			log.Error().Err(err).Msg("error seeking the File")
			writeError(w, http.StatusInternalServerError, errors.New("error seeking the file"))
			return
		}

		n, err := io.Copy(f, rd2)
		if err != nil {
			log.Error().Err(err).Msg("error writing the file")
			writeError(w, http.StatusInternalServerError, errors.New("error writing the file"))			
			return
		}

		log.Debug().
			Int64("written_size", n).
			Str("stored_file", f.Name()).
			Msg("File Uploaded")

		fm.UploadedSize += n
		c.store.Save(fm.ID, fm)

		w.Header().Add(UploadOffsetHeader, fmt.Sprint(fm.UploadedSize))
		if !fm.ExpiresAt.IsZero() {
			w.Header().Add(UploadExpiresHeader, uploadExpiresAt(fm.ExpiresAt))
		}
		w.WriteHeader(http.StatusNoContent)
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
		totalSize, err := strconv.ParseUint(totalLength, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid Upload-Length header"))
			return
		}

		if c.maxSize > 0 && totalSize > c.maxSize {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			w.Write([]byte("Upload-Length exceeds the maximum size"))
		}

		uploadMetadata := r.Header.Get(UploadMetadataHeader)
		log.Debug().Str("upload_metadata", uploadMetadata).Msg("Check request header")

		fm := FileMetadata{
			ID:        uuid.New().String(),
			TotalSize: totalSize,
			Metadata:  uploadMetadata,
			ExpiresAt: time.Now().Add(UploadMaxDuration),
		}
		c.store.Save(fm.ID, fm)

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

type cError struct {
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)

	b, _ := json.Marshal(cError{Message: err.Error()})
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}
