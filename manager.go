package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MapType is the compact route prefix used in generated dispatch URLs.
type MapType string

const (
	MapInline    MapType = "i"
	MapVendor    MapType = "v"
	MapAlias     MapType = "a"
	MapResources MapType = "r"
	MapPublic    MapType = "p"
	MapExternal  MapType = "e"
)

// ResourceManager builds URLs for one resource root and can add generated URLs
// to a ResourceStore.
type ResourceManager struct {
	dispatcher *Dispatcher
	mapType    MapType
	mapOptions []string
	store      *ResourceStore
	extraBits  Flag
}

func newResourceManager(dispatcher *Dispatcher, mapType MapType, mapOptions []string) *ResourceManager {
	return &ResourceManager{
		dispatcher: dispatcher,
		mapType:    mapType,
		mapOptions: append([]string(nil), mapOptions...),
	}
}

// MapType returns this manager's route map type.
func (m *ResourceManager) MapType() MapType {
	return m.mapType
}

// MapOptions returns the route map options, such as an alias name or vendor/package pair.
func (m *ResourceManager) MapOptions() []string {
	return append([]string(nil), m.mapOptions...)
}

// SetResourceStore makes this manager write required resources to store instead
// of the dispatcher's global store.
func (m *ResourceManager) SetResourceStore(store *ResourceStore) *ResourceManager {
	m.store = store
	return m
}

// UseGlobalResourceStore makes this manager write to its dispatcher's global store.
func (m *ResourceManager) UseGlobalResourceStore() *ResourceManager {
	m.store = nil
	return m
}

// HasResourceStore reports whether this manager has a custom store.
func (m *ResourceManager) HasResourceStore() bool {
	return m.store != nil
}

func (m *ResourceManager) resourceStore() *ResourceStore {
	if m.store != nil {
		return m.store
	}
	if m.dispatcher == nil {
		return NewResourceStore()
	}
	return m.dispatcher.Store()
}

// BaseURI returns this manager's generated URL prefix.
func (m *ResourceManager) BaseURI() string {
	if m.dispatcher == nil {
		return string(m.mapType)
	}
	parts := []string{m.dispatcher.BaseURI(), string(m.mapType)}
	switch m.mapType {
	case MapVendor:
		if len(m.mapOptions) == 2 {
			if alias := m.dispatcher.vendorAlias(m.mapOptions[0], m.mapOptions[1]); alias != "" {
				parts = append(parts, alias)
			} else {
				parts = append(parts, m.mapOptions...)
			}
		}
	case MapAlias:
		parts = append(parts, m.mapOptions...)
	default:
		parts = append(parts, m.mapOptions...)
	}
	return joinURL(parts...)
}

// URL returns a content-hashed URL for relativePath.
func (m *ResourceManager) URL(relativePath string, flags ...Flag) (string, error) {
	if m.mapType == MapExternal || IsExternalURL(relativePath) {
		return relativePath, nil
	}
	if m.dispatcher == nil {
		return "", fmt.Errorf("dispatch: resource manager has no dispatcher")
	}

	cleaned, ok := cleanRelativePath(relativePath)
	if !ok {
		return "", fmt.Errorf("dispatch: invalid resource path %q", relativePath)
	}

	filePath, err := m.FilePath(cleaned)
	if err != nil {
		return "", err
	}
	filePath, cleaned = m.optimisePath(filePath, cleaned)

	fileHash, err := m.FileHash(filePath)
	if err != nil {
		return "", err
	}
	relativeHash := m.RelativeHash(filePath)

	bits := m.bits()
	for _, flag := range flags {
		bits |= flag
	}

	hashSegment := fileHash + relativeHash + encodeBits(bits)
	return joinURL(m.BaseURI(), hashSegment, escapePath(cleaned)), nil
}

// MustURL returns URL(relativePath) and panics on error.
func (m *ResourceManager) MustURL(relativePath string, flags ...Flag) string {
	u, err := m.URL(relativePath, flags...)
	if err != nil {
		panic(err)
	}
	return u
}

func (m *ResourceManager) optimisePath(filePath, relativePath string) (string, string) {
	if m.dispatcher == nil {
		return filePath, relativePath
	}
	if m.bits()&FlagWebP == 0 {
		return filePath, relativePath
	}
	if !webPEligible(relativePath) {
		return filePath, relativePath
	}
	webpPath := filePath + ".webp"
	if fileExists(webpPath) {
		return webpPath, relativePath + ".webp"
	}
	return filePath, relativePath
}

func (m *ResourceManager) bits() Flag {
	if m.dispatcher == nil {
		return m.extraBits
	}
	return m.dispatcher.bits() | m.extraBits
}

func webPEligible(path string) bool {
	lower := strings.ToLower(path)
	for _, suffix := range []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tiff", ".svg"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// FilePath resolves a manager-relative path to a filesystem path.
func (m *ResourceManager) FilePath(relativePath string) (string, error) {
	if m.dispatcher == nil {
		return "", fmt.Errorf("dispatch: resource manager has no dispatcher")
	}
	var base string
	switch m.mapType {
	case MapResources:
		base = m.dispatcher.ResourcesPath()
	case MapPublic:
		base = m.dispatcher.PublicPath()
	case MapVendor:
		if len(m.mapOptions) != 2 {
			return "", fmt.Errorf("dispatch: vendor manager requires vendor and package")
		}
		base = m.dispatcher.VendorPath(m.mapOptions[0], m.mapOptions[1])
	case MapAlias:
		if len(m.mapOptions) != 1 {
			return "", fmt.Errorf("dispatch: alias manager requires an alias")
		}
		aliasBase, ok := m.dispatcher.AliasPath(m.mapOptions[0])
		if !ok {
			return "", fmt.Errorf("dispatch: unknown alias %q", m.mapOptions[0])
		}
		base = aliasBase
	default:
		return "", fmt.Errorf("dispatch: invalid map type %q", m.mapType)
	}
	return safeJoin(base, relativePath)
}

// FileHash returns the dispatch content hash for fullPath.
func (m *ResourceManager) FileHash(fullPath string) (string, error) {
	if m.dispatcher == nil {
		return "", fmt.Errorf("dispatch: resource manager has no dispatcher")
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return m.dispatcher.GenerateHash(md5Hex(data), 8), nil
}

// RelativeHash returns the dispatch path hash for fullPath.
func (m *ResourceManager) RelativeHash(fullPath string) string {
	if m.dispatcher == nil {
		return ""
	}
	return m.dispatcher.GenerateHash(m.dispatcher.RelativePath(fullPath), 4)
}

// IncludeCSS adds a CSS URL to the resource store, ignoring missing files.
func (m *ResourceManager) IncludeCSS(relativePath string, attrs Attrs, priority int) *ResourceManager {
	_ = m.RequireCSS(relativePath, attrs, priority)
	return m
}

// RequireCSS adds a CSS URL to the resource store.
func (m *ResourceManager) RequireCSS(relativePath string, attrs Attrs, priority int) error {
	if m.mapType == MapInline {
		m.resourceStore().RequireInlineCSS(relativePath, attrs, priority)
		return nil
	}
	u, err := m.URL(relativePath)
	if err != nil {
		return err
	}
	m.resourceStore().RequireCSS(u, attrs, priority)
	return nil
}

// IncludeJS adds a JavaScript URL to the resource store, ignoring missing files.
func (m *ResourceManager) IncludeJS(relativePath string, attrs Attrs, priority int) *ResourceManager {
	_ = m.RequireJS(relativePath, attrs, priority)
	return m
}

// RequireJS adds a JavaScript URL to the resource store.
func (m *ResourceManager) RequireJS(relativePath string, attrs Attrs, priority int) error {
	if m.mapType == MapInline {
		m.resourceStore().RequireInlineJS(relativePath, attrs, priority)
		return nil
	}
	u, err := m.URL(relativePath)
	if err != nil {
		return err
	}
	m.resourceStore().RequireJS(u, attrs, priority)
	return nil
}

// IsExternalURL reports whether path has an HTTP(S) or protocol-relative URL prefix.
func IsExternalURL(path string) bool {
	if len(path) < 8 {
		return false
	}
	lower := strings.ToLower(path)
	return strings.HasPrefix(path, "//") ||
		strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://")
}

func makeRelativeResourcePath(relativePath, workingDirectory string) string {
	relativePath = strings.ReplaceAll(relativePath, "\\", "/")
	if strings.HasPrefix(relativePath, "/") {
		return strings.TrimLeft(pathClean(relativePath), "/")
	}
	if workingDirectory == "." {
		workingDirectory = ""
	}
	return strings.TrimLeft(pathClean(workingDirectory+"/"+relativePath), "/")
}

func pathClean(p string) string {
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if cleaned == "." {
		return ""
	}
	return cleaned
}
