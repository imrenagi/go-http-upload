package v3

import (
	"time"
)

type FileMetadata struct {
	ID           string
	TotalSize    int64
	UploadedSize int64
	Metadata     string
	ExpiresAt    time.Time
}
