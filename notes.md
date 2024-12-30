# Notes


1. This code block is used to read the request body and write it to a file. The code reads the request body in chunks of 32KB and writes it to the file. The code also logs the number of bytes read and written. The code uses a buffer of 32KB to read the request body. The code reads the request body in a loop until the end of the request body is reached. The code writes the data to the file in chunks of 32KB. The code logs the number of bytes written to the file. The code also logs any errors that occur during reading or writing the request body.
```go
var n int64
buf := make([]byte, 32*1024) // 32KB buffer
for {
	log.Debug().Msg("read the request body")
	nr, er := r.Body.Read(buf)
	log.Debug().Int("read_size", nr).Msg("read the request body")
	if nr > 0 {
		nw, ew := f.Write(buf[0:nr])
		if nw < 0 || nr < nw {
			nw = 0
			if ew == nil {
				ew = errors.New("invalid write result")
			}
		}
		n += int64(nw)
		if ew != nil {
			log.Error().Err(ew).Msg("error writing the file")
			err = ew
			break
		}
		if nr != nw {
			log.Error().Err(io.ErrShortWrite).Msg("error writing the file")
			err = io.ErrShortWrite
			break
		}
	}

	if er != nil {
		log.Error().Err(er).Msg("error reading the request body")
		if er != io.EOF {
			err = er
		}
		break
	}
	log.Debug().Int64("written_size", n).Msg("write the data to the file xx")
}
```


```go
func (c *Controller) ResumeUpload() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // ... existing code ...

        f, err := os.OpenFile(fm.FilePath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
        if err != nil {
            log.Error().Err(err).Msg("error opening the file")
            writeError(w, http.StatusBadRequest, errors.New("error opening the file"))
            return
        }
        defer f.Close()

        // Store the current position before writing
        originalPos, err := f.Seek(0, io.SeekCurrent)
        if err != nil {
            log.Error().Err(err).Msg("error getting file position")
            writeError(w, http.StatusInternalServerError, errors.New("error preparing file"))
            return
        }

        var n int64
        if c.extensions.Enabled(ChecksumExtension) && checksum.Algorithm != "" {
            var hash hash.Hash
            switch checksum.Algorithm {
            case "md5":
                hash = md5.New()
            case "sha1":
                hash = sha1.New()
            default:
                writeError(w, http.StatusBadRequest, errors.New("unsupported checksum algorithm"))
                return
            }
            
            reader := io.TeeReader(r.Body, hash)
            n, err = io.Copy(f, reader)
            if err != nil {
                // Revert to original position on error
                f.Seek(originalPos, io.SeekStart)
                log.Error().Err(err).Msg("error writing file")
                writeError(w, http.StatusInternalServerError, errors.New("error writing file"))
                return
            }

            calculatedHash := hex.EncodeToString(hash.Sum(nil))
            if calculatedHash != checksum.Value {
                // Revert to original position if checksum fails
                f.Seek(originalPos, io.SeekStart)
                f.Truncate(originalPos) // Ensure file is truncated to original size
                log.Debug().Msg("Checksum mismatch")
                writeError(w, 460, errors.New("checksum mismatch"))
                return
            }
        } else {
            n, err = io.Copy(f, r.Body)
            if err != nil {
                // Revert to original position on error
                f.Seek(originalPos, io.SeekStart)
                f.Truncate(originalPos)
                log.Error().Err(err).Msg("error writing file")
                writeError(w, http.StatusInternalServerError, errors.New("error writing file"))
                return
            }
        }

        // If we get here, everything succeeded
        fm.UploadedSize += n
        c.store.Save(fm.ID, fm)

        // ... rest of response handling ...
    }
}
```

```go

func main() {
	f, err := os.OpenFile("test.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open file")
	}
	defer f.Close()

	originalPos, err := f.Seek(0, io.SeekEnd)
	log.Info().Int64("originalPos", originalPos).Msg("getting the original post")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to seek")
	}
	n, err := f.WriteString("test")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to write")
	}

	cur, _ := f.Seek(0, io.SeekCurrent)
	log.Info().Int64("cur", cur).Msg("getting the position after write")

	x := int64(-1 * n)
	num, err := f.Seek(x, io.SeekEnd)
	log.Info().Int64("num", num).Msg("getting the start position for truncate")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to seek")
	}
	err = f.Truncate(num)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to write")
	}
}
```