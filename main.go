package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	v1 "github.com/imrenagi/go-http-upload/api/v1"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

var meter = otel.Meter("github.com/imrenagi/go-http-uploader")

type ServerOpts struct {
}

func NewServer(opts ServerOpts) Server {
	s := Server{
		opts: opts,
	}
	return s
}

type Server struct {
	opts ServerOpts
}

// Run runs the gRPC-Gateway, dialing the provided address.
func (s *Server) Run(ctx context.Context) error {
	log.Info().Msg("starting server")

	serviceName := "go-http-uploader"

	prometheusExporter := NewPrometheusExporter(ctx)
	meterShutdownFn := InitMeterProvider(ctx, serviceName, prometheusExporter)

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: s.newHTTPHandler(),
	}

	go func() {
		log.Info().Msgf("Starting http server on :8080")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msgf("listen:%+s\n", err)
		}
	}()

	<-ctx.Done()

	gracefulShutdownPeriod := 30 * time.Second
	log.Warn().Msg("shutting down http server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownPeriod)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("failed to shutdown http server gracefully")
	}
	log.Warn().Msg("http server gracefully stopped")

	if err := meterShutdownFn(ctx); err != nil {
		log.Error().Err(err).Msg("failed to shutdown meter provider")
	}
	return nil
}

func (s *Server) newHTTPHandler() http.Handler {
	mux := mux.NewRouter()
	mux.Use(
		otelhttp.NewMiddleware("uploader"),
		LogInterceptor)
	mux.Handle("/metrics", promhttp.Handler())
	apiRouter := mux.PathPrefix("/api").Subrouter()
	apiRouter.Handle("/v1/form", otelhttp.WithRouteTag("/api/v1/form", http.HandlerFunc(v1.FormUpload())))
	apiRouter.Handle("/v1/binary", otelhttp.WithRouteTag("/api/v1/binary", http.HandlerFunc(v1.BinaryUpload())))
	mux.Handle("/binary-upload", otelhttp.WithRouteTag("/binary-upload", http.HandlerFunc(v1.Web()))).Methods(http.MethodGet)

	handler := otelhttp.NewHandler(mux, "/")
	return handler
}

func main() {
	ctx := context.Background()
	// Initialize the logger
	_ = InitializeLogger("debug")

	server := NewServer(ServerOpts{})
	if err := server.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to run the server")
	}
}
