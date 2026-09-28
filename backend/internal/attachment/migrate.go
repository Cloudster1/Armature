package attachment

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// MigrationReport says what a copy between stores did.
type MigrationReport struct {
	Copied int
	Bytes  int64
	Failed []string
}

// sniffLen is how much of a file the content type is read from, the most
// http.DetectContentType looks at.
const sniffLen = 512

// Migrate copies every object a directory holds into another store. It is
// how a deployment moves from a volume to a bucket: run it, point the api at
// the bucket, and only then let the volume go. Copying again is harmless, an
// object is simply written once more, so an interrupted run is resumed by
// running it again. The volume knows no content types, so each is read off
// the bytes.
func Migrate(ctx context.Context, from *FSStore, to Store, log *slog.Logger) (MigrationReport, error) {
	var report MigrationReport
	err := from.Walk(ctx, func(object Object) error {
		if err := copyObject(ctx, from, to, object); err != nil {
			log.Error("attachment not copied", "key", object.Key, "error", err)
			report.Failed = append(report.Failed, object.Key)
			return nil
		}
		report.Copied++
		report.Bytes += object.Size
		log.Info("attachment copied", "key", object.Key, "bytes", object.Size)
		return nil
	})
	if err != nil {
		return report, err
	}
	if len(report.Failed) > 0 {
		return report, fmt.Errorf("%d of %d objects were not copied; run it again once the cause is fixed", len(report.Failed), report.Copied+len(report.Failed))
	}
	return report, nil
}

func copyObject(ctx context.Context, from *FSStore, to Store, object Object) error {
	body, err := from.Get(ctx, object.Key)
	if err != nil {
		return err
	}
	defer body.Close()
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(body, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return err
	}
	head = head[:n]
	contentType := http.DetectContentType(head)
	return to.Put(ctx, object.Key, io.MultiReader(bytes.NewReader(head), body), object.Size, contentType)
}
