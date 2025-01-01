package main

import (
	"context"

	"cloud.google.com/go/storage"
	"github.com/rs/zerolog/log"
)

func main() {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create storage client")
	}
	defer client.Close()

	bucket := "imrenagi-upload-test"

	src1 := client.Bucket(bucket).Object("abdba280-4dc3-40df-a9dc-2dbc0fb47f75-0")
	src2 := client.Bucket(bucket).Object("abdba280-4dc3-40df-a9dc-2dbc0fb47f75-1")
	src3 := client.Bucket(bucket).Object("abdba280-4dc3-40df-a9dc-2dbc0fb47f75-2")
	src4 := client.Bucket(bucket).Object("abdba280-4dc3-40df-a9dc-2dbc0fb47f75-3")
	dst := client.Bucket(bucket).Object("abdba280-4dc3-40df-a9dc-2dbc0fb47f75")

	// ComposerFrom takes varargs, so you can put as many objects here
	// as you want.
	_, err = dst.ComposerFrom(src1, src2, src3, src4).Run(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to compose objects")
	}

	src1.Delete(ctx)
	src2.Delete(ctx)
	src3.Delete(ctx)
	src4.Delete(ctx)
}
