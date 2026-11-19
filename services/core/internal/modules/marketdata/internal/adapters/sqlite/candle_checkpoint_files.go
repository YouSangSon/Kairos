package sqlite

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Both files can hold the bounded snapshots plus SQLite pages and rollback overhead.
const maxCandleCheckpointFileBytes = 64 << 20

func lockCandleCheckpointDirectory(dir string) (*os.File, error) {
	if dir == "" {
		return nil, errors.New("a private candle checkpoint directory is required")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := checkCandleCheckpointDirectory(dir, true)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if canonical != dir {
		resolved, err := checkCandleCheckpointDirectory(canonical, false)
		if err != nil {
			return nil, err
		}
		if !os.SameFile(info, resolved) {
			return nil, errors.New("candle checkpoint directory changed")
		}
	}
	work, err := os.OpenFile(canonical, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	opened, err := work.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.Join(errors.New("candle checkpoint directory changed"), err, work.Close())
	}
	// Hold the directory inode to serialize capture processes without a stale lock file.
	if err := syscall.Flock(int(work.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("candle checkpoint directory cannot be locked"), err, work.Close())
	}
	return work, nil
}

func checkCandleCheckpointDirectory(dir string, allowSystemAliases bool) (os.FileInfo, error) {
	uid := uint32(os.Geteuid())
	var leaf os.FileInfo
	for path := dir; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (stat.Uid != uid && stat.Uid != 0) {
			return nil, errors.New("candle checkpoint directory requires trusted ownership")
		}
		mode := info.Mode()
		if path == dir {
			if stat.Uid != uid || mode != os.ModeDir|0o700 {
				return nil, errors.New("candle checkpoint requires an owned mode 0700 directory")
			}
			leaf = info
		} else if !(allowSystemAliases && stat.Uid == 0 && mode&os.ModeSymlink != 0) {
			rootSticky := stat.Uid == 0 && mode&os.ModeSticky != 0
			if !info.IsDir() || (mode.Perm()&0o022 != 0 && !rootSticky) {
				return nil, errors.New("candle checkpoint directory has an unsafe ancestor")
			}
		}
		if path == filepath.Dir(path) {
			return leaf, nil
		}
	}
}

func candleCheckpointFileLinks(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode() != 0o600 || info.Size() > maxCandleCheckpointFileBytes {
		return 0
	}
	return uint64(stat.Nlink)
}

func checkCandleCheckpointSidecars(path string, mayRecover bool) error {
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if suffix != "-journal" || !mayRecover || candleCheckpointFileLinks(info) != 1 {
			return errors.New("unsafe candle checkpoint sidecar")
		}
	}
	return nil
}

func candleCheckpointMarker(path string, info os.FileInfo) string {
	stat := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%s.pending-%d-%d", path, stat.Dev, stat.Ino)
}

func existingCandleCheckpointMarker(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	// A restrictive umask can leave fewer owner bits if the process stops before Chmod.
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || info.Mode()&^0o600 != 0 || info.Size() != 0 {
		return false, errors.New("unsafe candle checkpoint recovery marker")
	}
	return true, nil
}

func (c *CandleCheckpoint) beginWrite() error {
	if err := checkCandleCheckpointSidecars(c.path, false); err != nil {
		return err
	}
	marker, err := os.OpenFile(c.pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := errors.Join(marker.Chmod(0o600), marker.Close(), c.work.Sync()); err != nil {
		return errors.Join(err, os.Remove(c.pending))
	}
	return nil
}

func (c *CandleCheckpoint) finishWrite(recovered bool) error {
	if err := checkCandleCheckpointSidecars(c.path, recovered); err != nil {
		return err
	}
	if recovered {
		if err := os.Remove(c.path + "-journal"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(c.pending); err != nil {
		return err
	}
	return c.work.Sync()
}

func checkCandleCheckpointHeader(path string, expected os.FileInfo) (resultErr error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil || !os.SameFile(expected, info) || candleCheckpointFileLinks(expected) != candleCheckpointFileLinks(info) {
		return errors.Join(errors.New("candle checkpoint file changed"), err)
	}
	var header [100]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return err
	}
	if string(header[:16]) != "SQLite format 3\x00" || header[18] != 1 || header[19] != 1 ||
		binary.BigEndian.Uint32(header[68:72]) != candleCheckpointApplicationID {
		return errors.New("unrecognized candle checkpoint database")
	}
	return nil
}

func recoverCandleCheckpointStage(work *os.File, published os.FileInfo) error {
	entries, err := work.ReadDir(129)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > 128 {
		return errors.New("candle checkpoint work directory contains too many files")
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), candleCheckpointStagePrefix) {
			continue
		}
		path := filepath.Join(work.Name(), entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if candleCheckpointFileLinks(info) == 2 && os.SameFile(info, published) {
			if err := os.Remove(path); err != nil {
				return err
			}
			return work.Sync()
		}
	}
	return errors.New("unsafe linked candle checkpoint database")
}
