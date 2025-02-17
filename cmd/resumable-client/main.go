package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {

	stdOut := zerolog.ConsoleWriter{Out: os.Stdout}
	writers := []io.Writer{stdOut}
	zerolog.TimeFieldFormat = time.RFC3339Nano
	multi := zerolog.MultiLevelWriter(writers...)
	log.Logger = zerolog.New(multi).With().Timestamp().Logger()

	f, err := os.Open("testfile")
	if err != nil {
		log.Fatal().Err(err).Msg("Error opening file")
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		log.Fatal().Err(err).Msg("Error getting file info")
	}
	fileSize := fi.Size()
	log.Debug().Int64("size", fileSize).Msg("File size in bytes")

	req, err := http.NewRequest("POST", "http://localhost:8080/api/v3/files", nil)
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating request")
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Upload-Length", fmt.Sprint(fileSize))
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

	for {

		req, err := http.NewRequest("HEAD", "http://localhost:8080/api/v3/files/"+id, nil)
		if err != nil {
			log.Fatal().Err(err).Msg("Error creating request")
		}
		req.Header.Set("Tus-Resumable", "1.0.0")

		resp, err := httpClient.Do(req)
		if err != nil {
			log.Fatal().Err(err).Msg("Error sending request")
		}

		d, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatal().Err(err).Msg("Error reading response")
		}
		resp.Body.Close()

		log.Debug().Msg(string(d))

		uploadOffset := resp.Header.Get("Upload-Offset")
		offset, err := strconv.ParseInt(uploadOffset, 10, 64)
		if err != nil {
			log.Fatal().Err(err).Msg("Error parsing upload offset")
		}
		log.Debug().Str("Upload-Offset", uploadOffset).Msg("Check file upload offset ---")

		if offset >= fileSize {
			log.Debug().
				Str("Upload-Offset", uploadOffset).
				Str("fileSize", fmt.Sprint(fileSize)).
				Msg("File upload complete")
			break
		}

		f, err := os.Open("testfile")
		if err != nil {
			log.Fatal().Err(err).Msg("Error opening file")
		}
		defer f.Close()

		start, err := f.Seek(offset, io.SeekStart)
		if err != nil {
			log.Fatal().Err(err).Msg("Error seeking to offset")
		}
		log.Debug().Int64("start", start).Msg("Seek to offset")

		ctx := context.Background()
		// ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
		// defer cancel()
		req, err = http.NewRequestWithContext(ctx, "PATCH", "http://localhost:8080/api/v3/files/"+id, f)
		if err != nil {
			log.Fatal().Err(err).Msg("Error creating request")
		}
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Tus-Resumable", "1.0.0")
		req.Header.Set("Upload-Offset", fmt.Sprint(offset))

		log.Debug().Msg("Sending file data")

		resp, err = httpClient.Do(req)
		if err != nil {
			log.Warn().Err(err).Msg("Error sending request")
		}
		if resp == nil {
			log.Debug().Msg("patch response is nil")
			continue
		}

		d, err = io.ReadAll(resp.Body)
		if err != nil {
			log.Warn().Err(err).Msg("Error reading response")
		}
		resp.Body.Close()

		log.Debug().Msg(string(d))
		log.Debug().Int("status", resp.StatusCode).
			Str("Upload-Offset", resp.Header.Get("Upload-Offset")).
			Msg("Check file upload response")

	}

}
