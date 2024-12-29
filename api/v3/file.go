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

func (f FileMetadata) FilePath() string {
	if f.Path == "" {
		return "/tmp/" + f.ID
	}
	return f.Path
}