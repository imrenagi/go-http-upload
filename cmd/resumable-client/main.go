package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
)

func main() {

	req, err := http.NewRequest("POST", "http://localhost:8080/api/v3/files", nil)
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating request")
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Upload-Length", "75867935")
	req.Header.Set("Tus-Resumable", "1.0.0")

	httpClient := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatal().Err(err).Msg("Error sending request")
	}
	defer resp.Body.Close()
	d, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal().Err(err).Msg("Error reading response")
	}
	log.Debug().Msg(string(d))
	log.Debug().Str("location", resp.Header.Get("Location")).
		Int("status", resp.StatusCode).
		Msg("Check file creation response")

	location := resp.Header.Get("Location")
	id := location[strings.LastIndex(location, "/")+1:]
	log.Debug().Str("id", id).Msg("Extracted file ID")

	// f, err := os.Open("BloomRPC-1.5.3.dmg")
	f, err := os.Open("file.pdf")
	if err != nil {
		log.Fatal().Err(err).Msg("Error opening file")
	}
	defer f.Close()

	ctx := context.Background()
	// Create a pipe to stream data
	pr, pw := io.Pipe()

	// Start goroutine to write file data and inject error
	go func() {
		defer pw.Close()
		buf := make([]byte, 1024)
		bytesWritten := 0
		for {
			n, err := f.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			bytesWritten += n

			// Inject error after writing some data
			if bytesWritten > 1024000 { // After 1MB
				pw.CloseWithError(io.ErrUnexpectedEOF)
				return
			}

			if _, err := pw.Write(buf[:n]); err != nil {
				pw.CloseWithError(err)
				return
			}
			log.Debug().
				Int("bytesWritten", bytesWritten).
				Msg("data written")
		}
	}()

	req, err = http.NewRequestWithContext(ctx, "PATCH", "http://localhost:8080/api/v3/files/"+id, pr)
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating request")
	}
	req.Header.Set("Content-Type", "application/offset+octet-stream")
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Offset", "0")

	log.Debug().Msg("Sending file data")

	resp, err = httpClient.Do(req)
	if err != nil {
		log.Fatal().Err(err).
			Msg("Error sending request")
	}
	defer resp.Body.Close()
	d, err = io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal().Err(err).Msg("Error reading response")
	}

	log.Debug().Msg(string(d))
	log.Debug().Int("status", resp.StatusCode).
		Str("Upload-Offset", resp.Header.Get("Upload-Offset")).
		Msg("Check file upload response")

}
