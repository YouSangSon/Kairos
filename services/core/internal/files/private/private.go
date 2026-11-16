package privatefile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func Target(out string) (string, string, error) {
	out, err := filepath.Abs(out)
	if err != nil {
		return "", "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return "", "", err
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return "", "", err
	}
	for path := parent; ; path = filepath.Dir(path) {
		info, err := os.Stat(path)
		if err != nil {
			return "", "", err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || !ok || (stat.Uid != uint32(os.Getuid()) && stat.Uid != 0) || (path == parent && stat.Uid != uint32(os.Getuid())) {
			return "", "", errors.New("private output requires a trusted owned directory path")
		}
		rootSticky := path != parent && stat.Uid == 0 && info.Mode()&os.ModeSticky != 0
		if info.Mode().Perm()&0022 != 0 && !rootSticky {
			return "", "", errors.New("private output directory path permits shared writes")
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	target := filepath.Join(parent, filepath.Base(out))
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return "", "", errors.New("private output requires an unused output path")
	}
	return parent, target, nil
}

func Publish(ctx context.Context, parent, target, pattern string, encode func(io.Writer) error) (digest string, resultErr error) {
	f, err := os.CreateTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	temporary := f.Name()
	defer func() {
		if f != nil {
			resultErr = errors.Join(resultErr, f.Close())
		}
		if temporary != "" {
			resultErr = errors.Join(resultErr, os.Remove(temporary))
		}
	}()
	hash := sha256.New()
	if err := encode(io.MultiWriter(f, hash)); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	err = f.Close()
	f = nil
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Link(temporary, target); err != nil {
		return "", err
	}
	if err := os.Remove(temporary); err != nil {
		return "", err
	}
	temporary = ""
	dir, err := os.Open(parent)
	if err != nil {
		return "", err
	}
	if err := errors.Join(dir.Sync(), dir.Close()); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ReadBounded(path string, limit int64) ([]byte, error) {
	// Open first without waiting for a FIFO writer, then validate the actual
	// descriptor. A path-only Stat check would race a regular-file replacement.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("strategy evidence path must be a regular file")
	}
	artifact, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(artifact)) > limit {
		return nil, errors.New("input exceeds the byte limit")
	}
	return artifact, nil
}
