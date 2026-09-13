// Package profile is a person's picture: taken in through the API, kept in
// the same bucket as attachments, and served back through the API so the
// session decides who sees it.
package profile

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/auth"
)

const (
	// AvatarMaxBytes is the largest picture taken; a face does not need more.
	AvatarMaxBytes int64 = 2 << 20
	// AvatarMaxSide refuses a picture whose sides say it would decode into a
	// wall of memory, before any of it is decoded.
	AvatarMaxSide = 2048
)

var (
	// ErrNotAnImage is returned for anything that is not a PNG or a JPEG.
	ErrNotAnImage = errors.New("send a PNG or a JPEG picture")
	// ErrTooLarge is returned for a picture over the size or the side limit.
	ErrTooLarge = errors.New("the picture is too large: up to 2 MB and 2048 pixels a side")
	// ErrNoAvatar is returned when a person has no picture.
	ErrNoAvatar = errors.New("no picture")
)

// Service keeps pictures. Nil store means the deployment has no bucket, and
// setting a picture says so.
type Service struct {
	store    attachment.Store
	accounts *auth.Service
	now      func() time.Time
}

func NewService(store attachment.Store, accounts *auth.Service) *Service {
	return &Service{store: store, accounts: accounts, now: time.Now}
}

func key(userID uuid.UUID) string { return "user/" + userID.String() + "/avatar" }

// URL is the path a picture is served from, with a version so a browser that
// cached the old one asks for the new one.
func URL(userID uuid.UUID, version int64) string {
	return "/api/v1/users/" + userID.String() + "/avatar?v=" + strconv.FormatInt(version, 10)
}

// Set takes a picture in: the bytes are read up to the limit, the format is
// what the bytes say and not what the header claimed, and the sides are
// checked from the header before any pixel is decoded.
func (s *Service) Set(ctx context.Context, userID uuid.UUID, body io.Reader) (string, error) {
	if s.store == nil {
		return "", attachment.ErrUnavailable
	}
	if _, off := s.store.(attachment.Unavailable); off {
		return "", attachment.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(body, AvatarMaxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > AvatarMaxBytes {
		return "", ErrTooLarge
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return "", ErrNotAnImage
	}
	if config.Width > AvatarMaxSide || config.Height > AvatarMaxSide || config.Width == 0 || config.Height == 0 {
		return "", ErrTooLarge
	}
	contentType := "image/" + format
	if err := s.store.Put(ctx, key(userID), bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		return "", fmt.Errorf("store the picture: %w", err)
	}
	url := URL(userID, s.now().Unix())
	if err := s.accounts.SetAvatarURL(ctx, userID, &url); err != nil {
		return "", err
	}
	return url, nil
}

// Remove takes the picture away; the initials come back.
func (s *Service) Remove(ctx context.Context, userID uuid.UUID) error {
	if s.store != nil {
		if err := s.store.Delete(ctx, key(userID)); err != nil && !errors.Is(err, attachment.ErrNoObject) {
			return err
		}
	}
	return s.accounts.SetAvatarURL(ctx, userID, nil)
}

// Open reads a picture back with the type its bytes say it is.
func (s *Service) Open(ctx context.Context, userID uuid.UUID) (io.ReadCloser, string, error) {
	if s.store == nil {
		return nil, "", ErrNoAvatar
	}
	body, err := s.store.Get(ctx, key(userID))
	if err != nil {
		return nil, "", ErrNoAvatar
	}
	buffered := bufio.NewReader(body)
	head, _ := buffered.Peek(512)
	contentType := http.DetectContentType(head)
	return struct {
		io.Reader
		io.Closer
	}{buffered, body}, contentType, nil
}
