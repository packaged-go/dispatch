package dispatch

import (
	"bytes"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	cssURLPattern    = regexp.MustCompile(`(?is)url\(\s*(?:"[^"]*"|'[^']*'|[^)]*?)\s*\)`)
	cssImportPattern = regexp.MustCompile(`(?is)(@import\s+)(?:"([^"]*)"|'([^']*)'|([^;\s]+))(\s*;?)`)
	jsImportPattern  = regexp.MustCompile(`(?m)(import(?:\s+.+?\s+from)?\s*)(["'])([^"']+)(["'])(\s*;?)`)
	cssComment       = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssSymbolSpace   = regexp.MustCompile(`\s*([{}:;,])\s*`)
	cssSemicolon     = regexp.MustCompile(`;}`)
	jsLineComment    = regexp.MustCompile(`(?m)//[^\n\r]*`)
	jsBlockComment   = regexp.MustCompile(`(?s)/\*.*?\*/`)
	jsWhitespace     = regexp.MustCompile(`\s+`)
	jsSymbolSpace    = regexp.MustCompile(`\s*([{}();,:=+\-*/<>])\s*`)
)

func processContent(manager *ResourceManager, requestPath, fullPath string, content []byte) ([]byte, string) {
	ext := strings.ToLower(filepath.Ext(fullPath))
	contentType := contentTypeForExtension(ext)
	switch ext {
	case ".css":
		return []byte(processCSS(manager, requestPath, fullPath, string(content))), contentType
	case ".js", ".mjs":
		return []byte(processJS(manager, requestPath, fullPath, string(content))), contentType
	default:
		return content, contentType
	}
}

func processCSS(manager *ResourceManager, requestPath, fullPath, content string) string {
	if !strings.Contains(content, "@do-not-dispatch") {
		content = cssURLPattern.ReplaceAllStringFunc(content, func(match string) string {
			value, quote, ok := parseCSSURL(match)
			if !ok {
				return match
			}
			return "url(" + quote + dispatchNestedURL(manager, requestPath, value) + quote + ")"
		})
		content = cssImportPattern.ReplaceAllStringFunc(content, func(match string) string {
			parts := cssImportPattern.FindStringSubmatch(match)
			if len(parts) != 6 {
				return match
			}
			value, quote := importValueAndQuote(parts[2], parts[3], parts[4])
			return parts[1] + quote + dispatchNestedURL(manager, requestPath, value) + quote + parts[5]
		})
	}
	if shouldMinify(fullPath, content) {
		content = minifyCSS(content)
	}
	return content
}

func processJS(manager *ResourceManager, requestPath, fullPath, content string) string {
	if !strings.Contains(content, "@do-not-dispatch") {
		content = jsImportPattern.ReplaceAllStringFunc(content, func(match string) string {
			parts := jsImportPattern.FindStringSubmatch(match)
			if len(parts) != 6 {
				return match
			}
			return parts[1] + parts[2] + dispatchNestedURLWithFallback(manager, requestPath, parts[3], filepath.Ext(fullPath)) + parts[4] + parts[5]
		})
	}
	if shouldMinify(fullPath, content) {
		content = minifyJS(content)
	}
	return content
}

func parseCSSURL(match string) (value string, quote string, ok bool) {
	open := strings.Index(match, "(")
	close := strings.LastIndex(match, ")")
	if open < 0 || close <= open {
		return "", "", false
	}
	value = strings.TrimSpace(match[open+1 : close])
	if len(value) >= 2 {
		first := value[:1]
		last := value[len(value)-1:]
		if (first == `"` || first == `'`) && first == last {
			return value[1 : len(value)-1], first, true
		}
	}
	return value, "", true
}

func importValueAndQuote(doubleQuoted, singleQuoted, bare string) (string, string) {
	switch {
	case doubleQuoted != "":
		return doubleQuoted, `"`
	case singleQuoted != "":
		return singleQuoted, `'`
	default:
		return bare, ""
	}
}

func dispatchNestedURL(manager *ResourceManager, requestPath, rawPath string) string {
	return dispatchNestedURLWithFallback(manager, requestPath, rawPath, "")
}

func dispatchNestedURLWithFallback(manager *ResourceManager, requestPath, rawPath, fallbackExt string) string {
	if rawPath == "" || IsExternalURL(rawPath) || strings.HasPrefix(rawPath, "data:") {
		return rawPath
	}
	cleanPath, appendage := splitURLAppendage(rawPath)
	workingDirectory := path.Dir(strings.ReplaceAll(requestPath, "\\", "/"))
	if workingDirectory == "." {
		workingDirectory = ""
	}
	resourcePath := makeRelativeResourcePath(cleanPath, workingDirectory)
	u, err := manager.URL(resourcePath)
	if err != nil || u == "" {
		if fallbackExt == "" || path.Ext(resourcePath) != "" {
			return rawPath
		}
		u, err = manager.URL(resourcePath + fallbackExt)
		if err != nil || u == "" {
			return rawPath
		}
	}
	return u + appendage
}

func splitURLAppendage(rawPath string) (string, string) {
	query := strings.Index(rawPath, "?")
	fragment := strings.Index(rawPath, "#")
	if query < 0 && fragment < 0 {
		return rawPath, ""
	}
	pos := -1
	if query >= 0 && fragment >= 0 {
		if query < fragment {
			pos = query
		} else {
			pos = fragment
		}
	} else if query >= 0 {
		pos = query
	} else {
		pos = fragment
	}
	return rawPath[:pos], rawPath[pos:]
}

func shouldMinify(fullPath, content string) bool {
	base := filepath.Base(fullPath)
	if strings.Contains(base, ".min.") || strings.Contains(base, "-min.") {
		return false
	}
	return !strings.Contains(content, "@do-not-minify")
}

func minifyCSS(content string) string {
	content = cssComment.ReplaceAllString(content, "")
	content = cssSymbolSpace.ReplaceAllString(content, "$1")
	content = cssSemicolon.ReplaceAllString(content, "}")
	return strings.TrimSpace(content)
}

func minifyJS(content string) string {
	content = jsBlockComment.ReplaceAllString(content, "")
	content = jsLineComment.ReplaceAllString(content, "")
	content = jsWhitespace.ReplaceAllString(content, " ")
	content = jsSymbolSpace.ReplaceAllString(content, "$1")
	return strings.TrimSpace(content)
}

func contentTypeForExtension(ext string) string {
	switch ext {
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	default:
		if typ := mime.TypeByExtension(ext); typ != "" {
			return typ
		}
		return "application/octet-stream"
	}
}

func writeResourceResponse(
	w http.ResponseWriter,
	r *http.Request,
	fullPath string,
	content []byte,
	contentType string,
	cacheable bool,
	flags Flag,
	cache CacheConfig,
) {
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("X-Content-Type-Options", "nosniff")
	if flags&FlagContentAttachment != 0 {
		header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(fullPath)))
	}
	if cacheable {
		if cache.Vary != "" {
			header.Set("Vary", cache.Vary)
		}
		maxAge := int(cache.Duration.Seconds())
		cacheControl := "public, max-age=" + strconv.Itoa(maxAge)
		if cache.Immutable {
			cacheControl += ", immutable"
		}
		header.Set("Cache-Control", cacheControl)
		header.Set("ETag", `"`+md5Hex(content)+`"`)
		header.Set("Expires", time.Now().Add(cache.Duration).UTC().Format(http.TimeFormat))
	}
	modTime := time.Now()
	if info, err := os.Stat(fullPath); err == nil {
		modTime = info.ModTime()
	}
	http.ServeContent(w, r, filepath.Base(fullPath), modTime, bytes.NewReader(content))
}
