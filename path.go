package dispatch

import (
	"net/url"
	"path"
	"strings"
)

func trimSlashes(value string) string {
	return strings.Trim(value, "/")
}

func joinURL(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	prefix := ""
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 {
			if u, err := url.Parse(part); err == nil && u.Scheme != "" && u.Host != "" {
				prefix = strings.TrimRight(part, "/")
				continue
			}
			if strings.HasPrefix(part, "//") {
				prefix = strings.TrimRight(part, "/")
				continue
			}
			if strings.HasPrefix(part, "/") {
				prefix = "/"
			}
		}
		filtered = append(filtered, trimSlashes(part))
	}

	joined := path.Join(filtered...)
	if joined == "." {
		joined = ""
	}
	if prefix == "/" {
		if joined == "" {
			return "/"
		}
		return "/" + joined
	}
	if prefix != "" {
		if joined == "" {
			return prefix
		}
		return prefix + "/" + joined
	}
	return joined
}

func escapePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func baseURIPath(baseURI string) string {
	if baseURI == "" {
		return ""
	}
	u, err := url.Parse(baseURI)
	if err == nil && u.Scheme != "" {
		return strings.TrimRight(u.EscapedPath(), "/")
	}
	return strings.TrimRight(baseURI, "/")
}
