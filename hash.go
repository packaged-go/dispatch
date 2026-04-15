package dispatch

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

const defaultHashSalt = "dispatch"

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

func truncateHash(hash string, length int) string {
	if length > 0 && len(hash) > length {
		return hash[:length]
	}
	return hash
}

func slashPath(p string) string {
	return filepath.ToSlash(filepath.Clean(p))
}

func cleanRelativePath(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimLeft(p, "/")
	cleaned := filepath.ToSlash(filepath.Clean(p))
	if cleaned == "." || cleaned == "" {
		return "", false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
