package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	v1 "github.com/imrenagi/go-http-upload/api/v1"
	v3 "github.com/imrenagi/go-http-upload/api/v3"
	v4 "github.com/imrenagi/go-http-upload/api/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

var meter = otel.Meter("github.com/imrenagi/go-http-upload/server")

type Opts struct {
}

func New(opts Opts) Server {
	s := Server{
		opts: opts,
	}
	return s
}

type Server struct {
	opts Opts
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
		// ReadTimeout is the maximum duration for reading the entire request, including the body.
		// This prevents slowloris attacks.
		// This is useful for handling request from slow client so that it won't hold the connection for too long.
		ReadTimeout: 30 * time.Second,
		// WriteTimeout is the maximum duration before timing out writes of the response.
		// This is useful for handling slow client which read the response slowly.
		WriteTimeout: 10 * time.Second,
		// ReadHeaderTimeout is necessary here to prevent slowloris attacks.
		// https://www.cloudflare.com/learning/ddos/ddos-attack-tools/slowloris/
		ReadHeaderTimeout: 5 * time.Second,
		// IdleTimeout is the maximum amount of time to wait for the next request when keep-alives are enabled.
		IdleTimeout: 5 * time.Second,
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

	v4Controller := v4.NewController(v4.NewStore())
	apiV4Router := apiRouter.PathPrefix("/v4").Subrouter()
	apiV4Router.Use(v4.TusResumableHeaderCheck, v4.TusResumableHeaderInjections)
	apiV4Router.Handle("/files", otelhttp.WithRouteTag("/api/v4/files", http.HandlerFunc(v4Controller.GetConfig()))).Methods(http.MethodOptions)
	apiV4Router.Handle("/files", otelhttp.WithRouteTag("/api/v4/files", http.HandlerFunc(v4Controller.CreateUpload()))).Methods(http.MethodPost)
	apiV4Router.Handle("/files/{file_id}", otelhttp.WithRouteTag("/api/v4/files/{file_id}", http.HandlerFunc(v4Controller.GetOffset()))).Methods(http.MethodHead)
	apiV4Router.Handle("/files/{file_id}", otelhttp.WithRouteTag("/api/v4/files/{file_id}", http.HandlerFunc(v4Controller.ResumeUpload()))).Methods(http.MethodPatch)

	return otelhttp.NewHandler(mux, "/")
}
