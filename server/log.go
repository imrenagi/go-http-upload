package server

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func InitializeLogger(lvl string) func() {
	level, err := zerolog.ParseLevel(lvl)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to parse log level")
	}
	zerolog.SetGlobalLevel(level)

	// var stdOut io.Writer = os.Stdout
	stdOut := zerolog.ConsoleWriter{Out: os.Stdout}

	writers := []io.Writer{stdOut}
	zerolog.TimeFieldFormat = time.RFC3339Nano

	multi := zerolog.MultiLevelWriter(writers...)
	log.Logger = zerolog.New(multi).With().Timestamp().Logger()

	return func() {}
}

func LogInterceptor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		log := log.With().Str("request_id", uuid.New().String()).Logger()

		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote", r.RemoteAddr).
			Msg("request started")

		next.ServeHTTP(w, r.WithContext(log.WithContext(r.Context())))
	})
}
