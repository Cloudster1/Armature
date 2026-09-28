package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FSStore keeps objects as files under one directory, for a deployment with
// a volume and no object store. Keys become paths; the store refuses one
// that would leave the directory.
type FSStore struct {
	root string
}

// NewFS opens the directory, making it if it is missing.
func NewFS(dir string) (*FSStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("the attachment directory is blank")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("attachment directory: %w", err)
	}
	return &FSStore{root: root}, nil
}

// Root is the directory the files live in.
func (f *FSStore) Root() string { return f.root }

// tempPrefix marks a file still being written; Walk skips them and a crash
// leaves one behind rather than a half object under its real name.
const tempPrefix = ".tmp-"

func (f *FSStore) pathOf(key string) (string, error) {
	if key == "" || strings.Contains(key, "\x00") {
		return "", fmt.Errorf("%q is not an object key", key)
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q is not an object key", key)
	}
	full := filepath.Join(f.root, clean)
	if full != f.root && !strings.HasPrefix(full, f.root+string(filepath.Separator)) {
		return "", fmt.Errorf("%q is not an object key", key)
	}
	if strings.HasPrefix(filepath.Base(full), tempPrefix) {
		return "", fmt.Errorf("%q is not an object key", key)
	}
	return full, nil
}

// Put writes to a temporary file beside the target and renames it into
// place, so a reader never sees a partial object.
func (f *FSStore) Put(_ context.Context, key string, body io.Reader, size int64, _ string) error {
	full, err := f.pathOf(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("attachment directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), tempPrefix+filepath.Base(full)+"-*")
	if err != nil {
		return fmt.Errorf("write attachment: %w", err)
	}
	written, err := io.Copy(tmp, body)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil && size >= 0 && written != size {
		err = fmt.Errorf("wrote %d bytes of %d", written, size)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write attachment: %w", err)
	}
	if err := os.Rename(tmp.Name(), full); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write attachment: %w", err)
	}
	return nil
}

func (f *FSStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	full, err := f.pathOf(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoObject
	}
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if info, err := file.Stat(); err == nil && info.IsDir() {
		_ = file.Close()
		return nil, ErrNoObject
	}
	return file, nil
}

// Delete removes the file and any directory it leaves empty, so a store
// that has been emptied is an empty directory again.
func (f *FSStore) Delete(_ context.Context, key string) error {
	full, err := f.pathOf(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete attachment: %w", err)
	}
	for dir := filepath.Dir(full); dir != f.root && strings.HasPrefix(dir, f.root); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

// Object is one file the store holds, as Walk reports it.
type Object struct {
	Key  string
	Size int64
}

// Walk visits every object, in path order; files still being written are
// skipped. It is how a migration finds what to copy.
func (f *FSStore) Walk(ctx context.Context, visit func(Object) error) error {
	return filepath.WalkDir(f.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), tempPrefix) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(f.root, path)
		if err != nil {
			return err
		}
		return visit(Object{Key: filepath.ToSlash(rel), Size: info.Size()})
	})
}
