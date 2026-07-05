package importing

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

const minVideoSize = 50 * 1024 * 1024 // 50 MB — below this is treated as a sample/extra

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".m4v": true, ".ts": true, ".wmv": true, ".mov": true,
}

func isVideoFile(name string) bool { return videoExts[strings.ToLower(filepath.Ext(name))] }

func isSample(name string, size int64) bool {
	if strings.Contains(strings.ToLower(name), "sample") {
		return true
	}
	return size < minVideoSize
}

// placeFile hardlinks src to dst, falling back to a copy on cross-device/link
// failure. It creates parent directories. If dst already exists it is left as-is
// (idempotent retry). The original src is never removed (seeding-safe).
func placeFile(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil // already placed (retry)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// videoFilesIn walks root and returns non-sample video files (absolute paths).
func videoFilesIn(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() { // single-file download
		if isVideoFile(root) && !isSample(filepath.Base(root), info.Size()) {
			return []string{root}, nil
		}
		return nil, nil
	}
	var out []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !isVideoFile(d.Name()) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if isSample(d.Name(), fi.Size()) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out, err
}
