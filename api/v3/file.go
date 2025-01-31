package v3

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

func NewFile() File {
	id := uuid.New().String()
	f := File{
		ID:            id,
		IsDeferLength: true,
		Path:          "/tmp/file-upload-" + id,
	}
	return f
}

type File struct {
	ID            string
	Name          string
	TotalSize     uint64
	UploadedSize  uint64
	ContentType   string
	Checksum      string
	ExpiresAt     time.Time
	Path          string
	IsDeferLength bool
}

func (f *File) ParseMetadata(m string) error {
	md := make(map[string]string)
	kvs := strings.Split(m, ",")
	for _, kv := range kvs {
		if kv == "" {
			continue
		}
		parts := strings.Fields(kv)
		if len(parts) != 2 {
			return errors.New("invalid metadata")
		}
		decoded, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return err
		}
		md[parts[0]] = string(decoded)
	}
	contentType, ok := md["content-type"]
	if !ok {
		return errors.New("missing content-type")
	}
	checksum, ok := md["checksum"]
	if !ok {
		return errors.New("missing checksum")
	}
	name, ok := md["filename"]
	if !ok {
		return errors.New("missing filename")
	}
	f.Name = name
	f.ContentType = contentType
	f.Checksum = checksum
	return nil
}
