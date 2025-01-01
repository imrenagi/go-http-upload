package v3

import (
	"time"
)

type FileMetadata struct {
	ID           string
	TotalSize    uint64
	UploadedSize int64
	Metadata     string
	ExpiresAt    time.Time
	Path         string
}
