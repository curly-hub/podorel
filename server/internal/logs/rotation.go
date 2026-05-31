package logs

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultPerPodLimitBytes = int64(100 * 1024 * 1024)
	DefaultTotalLimitBytes  = int64(5 * 1024 * 1024 * 1024)
)

func RotateIfNeeded(path string, maxBytes int64, now time.Time) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if info.Size() < maxBytes {
		return "", false, nil
	}

	rotated := fmt.Sprintf("%s.%s.log.gz", trimCurrentLogSuffix(path), now.UTC().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(rotated), 0o755); err != nil {
		return "", false, err
	}
	if err := gzipFile(path, rotated); err != nil {
		return "", false, err
	}
	if err := os.Truncate(path, 0); err != nil {
		return "", false, err
	}
	return rotated, true, nil
}

func PruneToTotalLimit(dir string, maxBytes int64) ([]string, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("max bytes must be positive")
	}
	files, total, err := logFiles(dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})
	var removed []string
	for _, file := range files {
		if total <= maxBytes {
			break
		}
		if err := os.Remove(file.path); err != nil {
			return removed, err
		}
		total -= file.size
		removed = append(removed, file.path)
	}
	return removed, nil
}

func RemoveExpired(dir string, retention time.Duration, now time.Time) ([]string, error) {
	if retention <= 0 {
		return nil, fmt.Errorf("retention must be positive")
	}
	files, _, err := logFiles(dir)
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-retention)
	var removed []string
	for _, file := range files {
		if file.modTime.Before(cutoff) {
			if err := os.Remove(file.path); err != nil {
				return removed, err
			}
			removed = append(removed, file.path)
		}
	}
	sort.Strings(removed)
	return removed, nil
}

func gzipFile(source string, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		_ = gz.Close()
		return err
	}
	return gz.Close()
}

func trimCurrentLogSuffix(path string) string {
	ext := ".current.log"
	if len(path) >= len(ext) && path[len(path)-len(ext):] == ext {
		return path[:len(path)-len(ext)]
	}
	return path
}

type logFile struct {
	path    string
	size    int64
	modTime time.Time
}

func logFiles(dir string) ([]logFile, int64, error) {
	var files []logFile
	var total int64
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !isLogFile(path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, logFile{path: path, size: info.Size(), modTime: info.ModTime()})
		total += info.Size()
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	return files, total, nil
}

func isLogFile(path string) bool {
	return strings.HasSuffix(path, ".log") || strings.HasSuffix(path, ".log.gz")
}
