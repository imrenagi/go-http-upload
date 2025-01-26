package v3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
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
		"sha1",
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

	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("error creating storage client")
	}

	bkt := client.Bucket("go-http-upload-gcs-test")

	return Controller{
		store:      s,
		extensions: o.Extensions,
		maxSize:    o.MaxSize,
		storage:    client,
		bucket:     bkt,
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
	storage    *storage.Client
	bucket     *storage.BucketHandle
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

		w.Header().Add(UploadOffsetHeader, fmt.Sprint(fm.UploadedSize))
		w.Header().Add(UploadLengthHeader, fmt.Sprint(fm.TotalSize))
		w.Header().Add("Cache-Control", "no-store")
		if fm.Metadata != "" {
			w.Header().Add(UploadMetadataHeader, fm.Metadata)
		}
		if !fm.ExpiresAt.IsZero() {
			w.Header().Add(UploadExpiresHeader, uploadExpiresAt(fm.ExpiresAt))
		}

		if !fm.ExpiresAt.IsZero() && fm.ExpiresAt.Before(time.Now()) {
			log.Debug().Str("file_id", fileID).Msg("file expired")
			writeError(w, http.StatusGone, errors.New("file expired"))
			return
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
	if d[0] != "md5" && d[0] != "sha1" {
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

func (c *Controller) ResumeUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<20) //64MB
		doneCh := make(chan struct{})
		defer close(doneCh)

		go func() {
			select {
			case <-doneCh:
				log.Info().Msg("Upload completed")
				return
			case <-r.Context().Done():
				log.Warn().Err(r.Context().Err()).Msg("Upload canceled")
				return
			}
		}()

		// r.Body = http.MaxBytesReader(w, r.Body, 10<<20) //10MB
		vars := mux.Vars(r)
		fileID := vars["file_id"]

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
			log.Debug().Str("file_id", fileID).Msg("file expired")
			writeError(w, http.StatusGone, errors.New("file expired"))
			return
		}

		log.Debug().Int64("offset_request", offset).
			Int64("uploaded_size", fm.UploadedSize).
			Msg("Check size")

		if offset != fm.UploadedSize {
			log.Warn().Msg("upload-Offset header does not match the current offset")
			writeError(w, http.StatusConflict, errors.New("upload-Offset header does not match the current offset"))
			return
		}

		objName := fmt.Sprintf("%s-%d", fileID, offset)
		obj := c.bucket.Object(objName)
		objW := obj.NewWriter(r.Context())

		// objW.CRC32C = crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
		// objW.SendCRC32C = true
		defer objW.Close()

		n, err := io.Copy(objW, r.Body)
		if err != nil {

			fm.UploadedSize += n
			c.store.Save(fm.ID, fm)

			log.Info().
				Int64("written_size", n).
				Msg("partial message is written")

			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				log.Warn().Err(err).Msg("network timeout while writing file")
				writeError(w, http.StatusRequestTimeout, fmt.Errorf("network timeout: %w", err))
				return
			}

			log.Error().Err(err).Msg("error writing the file")
			writeError(w, http.StatusInternalServerError, fmt.Errorf("error writing the file: %w", err))
			return
		}

		fm.UploadedSize += n
		c.store.Save(fm.ID, fm)

		objPath := fmt.Sprintf("gs://%s/%s", c.bucket.BucketName(), objName)

		log.Debug().
			Int64("written_size", n).
			Str("stored_file", objPath).
			Msg("File Uploaded")

		log.Debug().Msg("prepare the response header")
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

		// TODO doesn't this upload length optional?
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
