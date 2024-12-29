package v1

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

func FormUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// log content type
		log.Debug().Str("content_type", r.Header.Get("Content-Type")).Msg("Request Content Type")

		// parse the form
		r.ParseMultipartForm(10 << 20)
		// get a handle to the file
		file, handler, err := r.FormFile("file")
		if err != nil {
			log.Error().Err(err).Msg("Error Retrieving the File")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Error Retrieving the File"))
			return
		}
		defer file.Close()

		// convert handler.size to KB
		f, err := os.CreateTemp("", "sample")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Error Retrieving the File"))
			return
		}
		defer f.Close()
		defer os.Remove(f.Name())

		n, err := io.Copy(f, file)
		if err != nil {
			log.Error().Err(err).Msg("Error Copying the File")
		}

		log.Info().Str("file_name", handler.Filename).
			Int64("file_size", handler.Size).
			Int64("written_size", n).
			Str("stored_file", f.Name()).
			Msg("File Uploaded")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("File Uploaded Successfully"))
	}
}

func BinaryUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		contentType := r.Header.Get("Content-Type")
		contentLength := r.Header.Get("Content-Length")
		fileName := r.Header.Get("X-Api-File-Name")
		log.Debug().
			Str("content_type", contentType).
			Str("content_length", contentLength).
			Str("file_name", fileName).
			Msg("received binary data")

		// Read the binary data from the request body
		file := r.Body
		defer file.Close()

		f, err := os.OpenFile(filepath.Join("/tmp", fileName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Error Retrieving the File"))
			return
		}
		defer f.Close()
		// defer os.Remove(f.Name())
		n, err := io.Copy(f, file)
		if err != nil {
			log.Error().Err(err).Msg("Error Copying the File")
		}

		log.Info().
			Int64("written_size", n).
			Str("stored_file", f.Name()).
			Msg("File Uploaded")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("File Uploaded Successfully"))
	}
}
