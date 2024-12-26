package v3

import (
	"time"

	"github.com/google/uuid"
)

type FileMetadata struct {
	ID           uuid.UUID
	TotalSize    int64
	UploadedSize int64
	Metadata     string
	ExpiresAt    time.Time
}
