package v3_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
