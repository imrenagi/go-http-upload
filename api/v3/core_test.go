package v3_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	. "github.com/imrenagi/go-http-upload/api/v3"
	"github.com/stretchr/testify/assert"
)

func newFakeStore(m map[string]FileMetadata) *fakeStore {
	return &fakeStore{
		files: m,
	}
}

type fakeStore struct {
	files map[string]FileMetadata
}

func (s *fakeStore) Find(id string) (FileMetadata, bool) {
	metadata, exists := s.files[id]
	return metadata, exists
}

func (s *fakeStore) Save(id string, metadata FileMetadata) {
	s.files[id] = metadata
}

func TestGetOffset(t *testing.T) {
	t.Run("The Server MUST always include the Upload-Offset header in the response for a HEAD request. The Server SHOULD acknowledge successful HEAD requests with a 200 OK or 204 No Content status.",
		func(t *testing.T) {
			m := map[string]FileMetadata{
				"a": {
					ID:           "a",
					UploadedSize: 0,
				},
			}
			ctrl := NewController(newFakeStore(m))

			req := httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
			w := httptest.NewRecorder()

			router := mux.NewRouter()
			router.HandleFunc("/api/v1/files/{file_id}", ctrl.GetOffset())
			router.ServeHTTP(w, req)

			assert.Contains(t, []int{http.StatusOK, http.StatusNoContent}, w.Code, "Expected status code %v, got %v", http.StatusOK, w.Code)
			assert.Equal(t, "0", w.Header().Get(UploadOffsetHeader), "Expected Upload-Offset header to be 0, got %v", w.Header().Get(UploadOffsetHeader))

			//The Server MUST prevent the client and/or proxies from caching the response by adding the Cache-Control: no-store header to the response.
			assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "Expected Cache-Control header to be no-store, got %v", w.Header().Get("Cache-Control"))
		})

	t.Run("If the size of the upload is known, the Server MUST include the Upload-Length header in the response.", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 19,
				TotalSize:    100,
			},
		}
		ctrl := NewController(newFakeStore(m))

		req := httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.GetOffset())
		router.ServeHTTP(w, req)

		assert.Equal(t, "100", w.Header().Get(UploadLengthHeader))
		assert.Equal(t, "19", w.Header().Get(UploadOffsetHeader))
	})

	t.Run("If the resource is not found, the Server SHOULD return either the 404 Not Found status without the Upload-Offset header.", func(t *testing.T) {
		m := map[string]FileMetadata{}
		ctrl := NewController(newFakeStore(m))

		req := httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.GetOffset())
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Empty(t, w.Header().Get(UploadOffsetHeader))
	})

}

func TestTusResumableHeader(t *testing.T) {
	t.Run("Return 400 if The Tus-Resumable header is not included in HEAD request", func(t *testing.T) {
		m := map[string]FileMetadata{}
		ctrl := NewController(newFakeStore(m))

		req := httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.Use(TusResumableHeaderCheck)
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.GetOffset())
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		// the Server MUST NOT process the request.
		assert.Empty(t, w.Header().Get(UploadOffsetHeader))
		assert.Empty(t, w.Header().Get(UploadLengthHeader))
	})

	t.Run("Return 412 if The Tus-Resumable header is not supported by the server. server must not process the request", func(t *testing.T) {
		m := map[string]FileMetadata{}
		ctrl := NewController(newFakeStore(m))

		req := httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
		req.Header.Set(TusResumableHeader, "1.0.1")
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.Use(TusResumableHeaderCheck)
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.GetOffset())
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusPreconditionFailed, w.Code)
		// the Server MUST NOT process the request.
		assert.Empty(t, w.Header().Get(UploadOffsetHeader))
		assert.Empty(t, w.Header().Get(UploadLengthHeader))
	})

	t.Run("Multipe value of The Tus-Resumable header can be supported by the server", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 19,
				TotalSize:    100,
			},
		}
		ctrl := NewController(newFakeStore(m))
		router := mux.NewRouter()
		router.Use(TusResumableHeaderCheck)
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.GetOffset())

		req := httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
		req.Header.Set(TusResumableHeader, "0.2.0")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Contains(t, []int{http.StatusOK, http.StatusNoContent}, w.Code, "Expected status code %v, got %v", http.StatusOK, w.Code)

		req = httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
		req.Header.Set(TusResumableHeader, "1.0.0")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Contains(t, []int{http.StatusOK, http.StatusNoContent}, w.Code, "Expected status code %v, got %v", http.StatusOK, w.Code)
	})

	t.Run("The Tus-Resumable header MUST be included in every response in HEAD requests. ", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 19,
				TotalSize:    100,
			},
		}
		ctrl := NewController(newFakeStore(m))
		router := mux.NewRouter()
		router.Use(TusResumableHeaderInjections)
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.GetOffset())

		req := httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, "1.0.0", w.Header().Get(TusResumableHeader))
		assert.Contains(t, []int{http.StatusOK, http.StatusNoContent}, w.Code, "Expected status code %v, got %v", http.StatusOK, w.Code)
	})
}

func TestGetConfig(t *testing.T) {
	t.Run("A successful response indicated by the 204 No Content or 200 OK status MUST contain the Tus-Version header", func(t *testing.T) {
		m := map[string]FileMetadata{}
		ctrl := NewController(newFakeStore(m))

		req := httptest.NewRequest(http.MethodOptions, "/api/v1/files", nil)
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files", ctrl.GetConfig())
		router.ServeHTTP(w, req)

		assert.Contains(t, []int{http.StatusOK, http.StatusNoContent}, w.Code, "Expected status code %v, got %v", http.StatusOK, w.Code)
		assert.Equal(t, "0.2.0,1.0.0", w.Header().Get(TusVersionHeader))
		assert.Empty(t, w.Header().Get(TusResumableHeader))
	})

	t.Run("It MAY include the Tus-Extension and Tus-Max-Size headers.", func(t *testing.T) {
		m := map[string]FileMetadata{}
		ctrl := NewController(newFakeStore(m),
			WithExtensions(Extensions{CreationExtension,
				ExpirationExtension,
				ChecksumExtension}),
			WithMaxSize(1073741824))

		req := httptest.NewRequest(http.MethodOptions, "/api/v1/files", nil)
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files", ctrl.GetConfig())
		router.ServeHTTP(w, req)

		assert.Equal(t, "creation,expiration,checksum", w.Header().Get(TusExtensionHeader))
		assert.Equal(t, "1073741824", w.Header().Get(TusMaxSizeHeader))
		assert.Equal(t, "md5", w.Header().Get(TusChecksumAlgorithmHeader))
	})

	t.Run("The extension header must be omitted if the server does not support any extensions", func(t *testing.T) {
		m := map[string]FileMetadata{}
		ctrl := NewController(newFakeStore(m),
			WithExtensions(Extensions{}),
		)

		req := httptest.NewRequest(http.MethodOptions, "/api/v1/files", nil)
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files", ctrl.GetConfig())
		router.ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get(TusExtensionHeader))
		assert.Empty(t, w.Header().Get(TusMaxSizeHeader))
		assert.Empty(t, w.Header().Get(TusChecksumAlgorithmHeader))

	})
}

func TestResumeUpload(t *testing.T) {

	t.Run("Upload-Offset must be included in the request", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 0,
				TotalSize:    10,
			},
		}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{}))

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/files/a", nil)
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.ResumeUpload()).Methods(http.MethodPatch)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, `{"message":"invalid Upload-Offset header: not a number"}`, w.Body.String())
	})

	t.Run("Upload-Offset must be included in the request with value gte 0", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 0,
				TotalSize:    10,
			},
		}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{}))

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/files/a", nil)
		req.Header.Set("Upload-Offset", "-1")
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.ResumeUpload()).Methods(http.MethodPatch)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, `{"message":"invalid Upload-Offset header: negative value"}`, w.Body.String())
	})

	t.Run("When PATCH requests doesnt use Content-Type: application/offset+octet-stream, server SHOULD return a 415 Unsupported Media Type status", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 0,
				TotalSize:    10,
			},
		}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{}))

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/files/a", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Upload-Offset", "0")
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.ResumeUpload()).Methods(http.MethodPatch)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
		assert.Equal(t, `{"message":"invalid Content-Type header: expected application/offset+octet-stream"}`, w.Body.String())
	})

	t.Run("If the server receives a PATCH request against a non-existent resource it SHOULD return a 404 Not Found status.", func(t *testing.T) {
		m := map[string]FileMetadata{}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{}))

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/files/a", nil)
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Upload-Offset", "0")
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.ResumeUpload()).Methods(http.MethodPatch)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, `{"message":"file not found"}`, w.Body.String())
	})

	t.Run(" If the offsets do not match, the Server MUST respond with the 409 Conflict status without modifying the upload resource.", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 0,
				TotalSize:    10,
			},
		}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{}))

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/files/a", nil)
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Upload-Offset", "10")
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.ResumeUpload()).Methods(http.MethodPatch)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, `{"message":"upload-Offset header does not match the current offset"}`, w.Body.String())
	})

	t.Run("The Server MUST acknowledge successful PATCH requests with the 204 No Content status. It MUST include the Upload-Offset header containing the new offset", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 0,
				TotalSize:    5,
			},
		}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{}))

		buf := bytes.NewBufferString("ccc")
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/files/a", buf)
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Upload-Offset", "0")
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.ResumeUpload()).Methods(http.MethodPatch)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "3", w.Header().Get(UploadOffsetHeader))
	})
}

func TestExpiration(t *testing.T) {
	t.Run("The expiration header may be included in the HEAD response when the upload is going to expire.", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 0,
				TotalSize:    5,
				ExpiresAt:    time.Now().Add(1 * time.Hour),
			},
		}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{ExpirationExtension}))

		req := httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.GetOffset())
		router.ServeHTTP(w, req)

		format := "Mon, 02 Jan 2006 15:04:05 GMT"
		ts := w.Header().Get(UploadExpiresHeader)
		tt, err := time.Parse(format, ts)
		assert.NoError(t, err)

		assert.Equal(t, m["a"].ExpiresAt.Format(format), tt.Format(format))
	})

	t.Run("the Server SHOULD respond with 410 Gone status if the Server is keeping track of expired uploads", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 0,
				TotalSize:    5,
				ExpiresAt:    time.Now().Add(-1 * time.Hour),
			},
		}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{ExpirationExtension}))

		req := httptest.NewRequest(http.MethodHead, "/api/v1/files/a", nil)
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.GetOffset())
		router.ServeHTTP(w, req)

		format := "Mon, 02 Jan 2006 15:04:05 GMT"
		ts := w.Header().Get(UploadExpiresHeader)
		tt, err := time.Parse(format, ts)
		assert.NoError(t, err)
		assert.Equal(t, m["a"].ExpiresAt.Format(format), tt.Format(format))
		assert.Equal(t, http.StatusGone, w.Code)
	})

	t.Run("This header MUST be included in every PATCH response if the upload is going to expire.", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 0,
				TotalSize:    5,
				ExpiresAt:    time.Now().Add(1 * time.Hour),
			},
		}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{ExpirationExtension}))

		buf := bytes.NewBufferString("ccc")
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/files/a", buf)
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Upload-Offset", "0")
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.ResumeUpload()).Methods(http.MethodPatch)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "3", w.Header().Get(UploadOffsetHeader))

		format := "Mon, 02 Jan 2006 15:04:05 GMT"
		ts := w.Header().Get(UploadExpiresHeader)
		tt, err := time.Parse(format, ts)
		assert.NoError(t, err)
		assert.Equal(t, m["a"].ExpiresAt.Format(format), tt.Format(format))
	})

	t.Run("If a Client does attempt to resume an upload which has since been removed by the Server, the Server SHOULD respond with 410 Gone status", func(t *testing.T) {
		m := map[string]FileMetadata{
			"a": {
				ID:           "a",
				UploadedSize: 0,
				TotalSize:    5,
				ExpiresAt:    time.Now().Add(-1 * time.Hour),
			},
		}
		ctrl := NewController(newFakeStore(m), WithExtensions(Extensions{ExpirationExtension}))

		buf := bytes.NewBufferString("ccc")
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/files/a", buf)
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Upload-Offset", "0")
		w := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/api/v1/files/{file_id}", ctrl.ResumeUpload()).Methods(http.MethodPatch)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusGone, w.Code)
		assert.Empty(t, w.Header().Get(UploadOffsetHeader))
		assert.Empty(t, w.Header().Get(UploadExpiresHeader))
		assert.Equal(t, `{"message":"file expired"}`, w.Body.String())

	})
}

func TestChecksum(t *testing.T) {
	
}