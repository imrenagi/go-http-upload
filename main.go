package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	v1 "github.com/imrenagi/go-http-upload/api/v1"
	v3 "github.com/imrenagi/go-http-upload/api/v3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

var meter = otel.Meter("github.com/imrenagi/go-http-upload")

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
		// ReadTimeout is necessary here to prevent slowloris attacks.
		// https://www.cloudflare.com/learning/ddos/ddos-attack-tools/slowloris/
		// This is also useful when clients is already canceling the request, but the server is still holding the connection.
		ReadTimeout:       3 * time.Second,
		ReadHeaderTimeout: 3 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       5 * time.Second,
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

	apiV1Router := apiRouter.PathPrefix("/v1").Subrouter()
	apiV1Router.Handle("/form", otelhttp.WithRouteTag("/api/v1/form", http.HandlerFunc(v1.FormUpload())))
	apiV1Router.Handle("/binary", otelhttp.WithRouteTag("/api/v1/binary", http.HandlerFunc(v1.BinaryUpload())))
	mux.Handle("/v1", otelhttp.WithRouteTag("/v1", http.HandlerFunc(v1.Web()))).Methods(http.MethodGet)

	v3Controller := v3.NewController(v3.NewStore())
	apiV3Router := apiRouter.PathPrefix("/v3").Subrouter()
	apiV3Router.Use(v3.TusResumableHeaderCheck, v3.TusResumableHeaderInjections)
	apiV3Router.Handle("/files", otelhttp.WithRouteTag("/api/v3/files", http.HandlerFunc(v3Controller.GetConfig()))).Methods(http.MethodOptions)
	apiV3Router.Handle("/files", otelhttp.WithRouteTag("/api/v3/files", http.HandlerFunc(v3Controller.CreateUpload()))).Methods(http.MethodPost)
	apiV3Router.Handle("/files/{file_id}", otelhttp.WithRouteTag("/api/v3/files/{file_id}", http.HandlerFunc(v3Controller.GetOffset()))).Methods(http.MethodHead)
	apiV3Router.Handle("/files/{file_id}", otelhttp.WithRouteTag("/api/v3/files/{file_id}", http.HandlerFunc(v3Controller.ResumeUpload()))).Methods(http.MethodPatch)

	return otelhttp.NewHandler(mux, "/")
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
