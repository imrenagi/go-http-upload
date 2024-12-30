package main

import (
	"context"

	"github.com/imrenagi/go-http-upload/server"
	"github.com/rs/zerolog/log"
)

func main() {
	ctx := context.Background()
	// Initialize the logger
	_ = server.InitializeLogger("debug")

	server := server.New(server.Opts{})
	if err := server.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to run the server")
	}
}
