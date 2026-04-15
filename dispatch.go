package dispatch

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	ResourcesDir = "resources"
	PublicDir    = "public"
	VendorDir    = "vendor"
)

// Dispatcher generates content-hashed resource URLs and serves matching HTTP
// requests with cache headers suitable for immutable assets.
type Dispatcher struct {
	projectRoot     string
	baseURI         string
	hashSalt        string
	requireFileHash bool
	store           *ResourceStore

	aliases              map[string]string
	vendorAliases        map[string][2]string
	vendorReverseAliases map[string]string

	acceptableTypes []string
	optimizeWebP    bool
	cache           CacheConfig

	mu sync.RWMutex
}

// New creates a Dispatcher rooted at projectRoot. By convention, resources are
// served from projectRoot/resources, public assets from projectRoot/public, and
// vendor assets from projectRoot/vendor/{vendor}/{package}.
func New(projectRoot string, opts ...Option) *Dispatcher {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		root = projectRoot
	}
	d := &Dispatcher{
		projectRoot:          filepath.Clean(root),
		hashSalt:             defaultHashSalt,
		store:                NewResourceStore(),
		aliases:              map[string]string{},
		vendorAliases:        map[string][2]string{},
		vendorReverseAliases: map[string]string{},
		cache:                defaultCacheConfig(),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// ProjectRoot returns the absolute project root configured for this dispatcher.
func (d *Dispatcher) ProjectRoot() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.projectRoot
}

// BaseURI returns the URI prefix used when generating resource URLs.
func (d *Dispatcher) BaseURI() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.baseURI
}

// Store returns the global resource store used by managers created from d.
func (d *Dispatcher) Store() *ResourceStore {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.store
}

// SetResourceStore replaces the dispatcher's global resource store.
func (d *Dispatcher) SetResourceStore(store *ResourceStore) *Dispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	if store == nil {
		store = NewResourceStore()
	}
	d.store = store
	return d
}

// ResourcesPath returns projectRoot/resources.
func (d *Dispatcher) ResourcesPath() string {
	return filepath.Join(d.ProjectRoot(), ResourcesDir)
}

// PublicPath returns projectRoot/public.
func (d *Dispatcher) PublicPath() string {
	return filepath.Join(d.ProjectRoot(), PublicDir)
}

// VendorPath returns projectRoot/vendor/vendor/package.
func (d *Dispatcher) VendorPath(vendor, pkg string) string {
	return filepath.Join(d.ProjectRoot(), VendorDir, vendor, pkg)
}

// AddAlias maps a short URL segment to a project-root-relative or absolute path.
func (d *Dispatcher) AddAlias(alias, path string) *Dispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.aliases[alias] = path
	return d
}

// AliasPath resolves a configured alias. The boolean is false when the alias is unknown.
func (d *Dispatcher) AliasPath(alias string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	p, ok := d.aliases[alias]
	if !ok {
		return "", false
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), true
	}
	return filepath.Join(d.projectRoot, p), true
}

// AddVendorAlias maps a compact URL segment to a vendor/package pair.
func (d *Dispatcher) AddVendorAlias(vendor, pkg, alias string) *Dispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.vendorAliases[alias] = [2]string{vendor, pkg}
	d.vendorReverseAliases[vendor+"/"+pkg] = alias
	return d
}

func (d *Dispatcher) vendorAlias(vendor, pkg string) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.vendorReverseAliases[vendor+"/"+pkg]
}

func (d *Dispatcher) vendorAliasTarget(alias string) (vendor, pkg string, ok bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	target, ok := d.vendorAliases[alias]
	if !ok {
		return "", "", false
	}
	return target[0], target[1], true
}

// GenerateHash returns md5(content + salt), optionally truncated to length.
func (d *Dispatcher) GenerateHash(content string, length int) string {
	d.mu.RLock()
	salt := d.hashSalt
	d.mu.RUnlock()
	return truncateHash(md5Hex([]byte(content+salt)), length)
}

// RelativePath calculates a slash-separated path relative to the project root
// when possible.
func (d *Dispatcher) RelativePath(filePath string) string {
	root := d.ProjectRoot()
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return slashPath(filePath)
	}
	rel, err := filepath.Rel(root, abs)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return slashPath(rel)
	}
	return strings.TrimLeft(slashPath(abs), "/")
}

func (d *Dispatcher) bits() Flag {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.optimizeWebP {
		for _, typ := range d.acceptableTypes {
			if typ == "image/webp" {
				return FlagWebP
			}
		}
	}
	return 0
}

func (d *Dispatcher) requestBits(r *http.Request, encoded Flag) Flag {
	d.mu.RLock()
	optimizeWebP := d.optimizeWebP
	d.mu.RUnlock()
	if optimizeWebP && strings.Contains(r.Header.Get("Accept"), "image/webp") {
		encoded |= FlagWebP
	}
	return encoded
}

// Resources returns a manager for projectRoot/resources.
func (d *Dispatcher) Resources() *ResourceManager {
	return newResourceManager(d, MapResources, nil)
}

// Public returns a manager for projectRoot/public.
func (d *Dispatcher) Public() *ResourceManager {
	return newResourceManager(d, MapPublic, nil)
}

// Alias returns a manager for a configured alias.
func (d *Dispatcher) Alias(alias string) *ResourceManager {
	return newResourceManager(d, MapAlias, []string{alias})
}

// Vendor returns a manager for projectRoot/vendor/vendor/package.
func (d *Dispatcher) Vendor(vendor, pkg string) *ResourceManager {
	return newResourceManager(d, MapVendor, []string{vendor, pkg})
}

// Inline returns a manager that stores inline CSS and JavaScript.
func (d *Dispatcher) Inline() *ResourceManager {
	return newResourceManager(d, MapInline, nil)
}

// External returns a manager that returns URLs unchanged.
func (d *Dispatcher) External() *ResourceManager {
	return newResourceManager(d, MapExternal, nil)
}

// URL is a convenience wrapper for d.Resources().URL(path).
func (d *Dispatcher) URL(path string, flags ...Flag) (string, error) {
	return d.Resources().URL(path, flags...)
}

// MustURL is a convenience wrapper for d.Resources().MustURL(path).
func (d *Dispatcher) MustURL(path string, flags ...Flag) string {
	return d.Resources().MustURL(path, flags...)
}

// Middleware serves dispatch URLs and delegates all other requests to next.
func (d *Dispatcher) Middleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.matchesDispatchPath(r.URL.Path) {
			d.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (d *Dispatcher) matchesDispatchPath(requestPath string) bool {
	parts := d.requestParts(requestPath)
	if len(parts) < 3 {
		return false
	}
	switch MapType(parts[0]) {
	case MapResources, MapPublic:
		return true
	case MapAlias:
		return len(parts) >= 4
	case MapVendor:
		return len(parts) >= 5
	default:
		return false
	}
}

func (d *Dispatcher) requestParts(requestPath string) []string {
	base := baseURIPath(d.BaseURI())
	if base != "" && base != "/" {
		if requestPath == base {
			requestPath = "/"
		} else if strings.HasPrefix(requestPath, base+"/") {
			requestPath = strings.TrimPrefix(requestPath, base)
		}
	}
	requestPath = strings.Trim(requestPath, "/")
	if requestPath == "" {
		return nil
	}
	parts := strings.Split(requestPath, "/")
	out := parts[:0]
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ServeHTTP serves a generated dispatch URL.
func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	manager, hashSegment, resourcePath, ok := d.managerFromRequest(r)
	if !ok {
		http.Error(w, "File Not Found", http.StatusNotFound)
		return
	}

	compareHash, bits := splitHashAndBits(hashSegment)
	fullPath, err := manager.FilePath(resourcePath)
	if err != nil {
		http.Error(w, "File Not Found", http.StatusNotFound)
		return
	}

	fileHash, err := manager.FileHash(fullPath)
	if err != nil {
		http.Error(w, "File Not Found", http.StatusNotFound)
		return
	}

	fileHashPart := compareHash
	relativeHashPart := ""
	if len(fileHashPart) > 8 {
		relativeHashPart = fileHashPart[8:]
		fileHashPart = fileHashPart[:8]
	}

	contentHashMatch := fileHashPart == fileHash
	failedHash := true
	d.mu.RLock()
	requireFileHash := d.requireFileHash
	d.mu.RUnlock()
	if !requireFileHash && relativeHashPart != "" && relativeHashPart == manager.RelativeHash(fullPath) {
		failedHash = false
	}
	if (relativeHashPart == "" || failedHash) && contentHashMatch {
		failedHash = false
	}
	if failedHash {
		http.Error(w, "File Not Found", http.StatusNotFound)
		return
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		http.Error(w, "File Not Found", http.StatusNotFound)
		return
	}

	encodedBits := d.requestBits(r, bits)
	manager.extraBits = encodedBits
	processed, contentType := processContent(manager, resourcePath, fullPath, content)
	writeResourceResponse(w, r, fullPath, processed, contentType, contentHashMatch, encodedBits, d.cache)
}

func (d *Dispatcher) managerFromRequest(r *http.Request) (*ResourceManager, string, string, bool) {
	parts := d.requestParts(r.URL.EscapedPath())
	if len(parts) < 3 {
		return nil, "", "", false
	}
	for i, part := range parts {
		unescaped, err := filepathFromURLSegment(part)
		if err != nil {
			return nil, "", "", false
		}
		parts[i] = unescaped
	}

	mapType := MapType(parts[0])
	switch mapType {
	case MapResources:
		if len(parts) < 3 {
			return nil, "", "", false
		}
		return d.Resources(), parts[1], strings.Join(parts[2:], "/"), true
	case MapPublic:
		if len(parts) < 3 {
			return nil, "", "", false
		}
		return d.Public(), parts[1], strings.Join(parts[2:], "/"), true
	case MapAlias:
		if len(parts) < 4 {
			return nil, "", "", false
		}
		return d.Alias(parts[1]), parts[2], strings.Join(parts[3:], "/"), true
	case MapVendor:
		if len(parts) < 5 {
			return nil, "", "", false
		}
		if vendor, pkg, ok := d.vendorAliasTarget(parts[1]); ok {
			return d.Vendor(vendor, pkg), parts[2], strings.Join(parts[3:], "/"), true
		}
		return d.Vendor(parts[1], parts[2]), parts[3], strings.Join(parts[4:], "/"), true
	default:
		return nil, "", "", false
	}
}

func filepathFromURLSegment(segment string) (string, error) {
	if strings.Contains(segment, "%2f") || strings.Contains(segment, "%2F") {
		return "", fmt.Errorf("escaped slash is not allowed")
	}
	return url.PathUnescape(segment)
}

func splitHashAndBits(segment string) (string, Flag) {
	hash, encodedBits, ok := strings.Cut(segment, "-")
	if !ok || encodedBits == "" {
		return hash, 0
	}
	parsed, err := strconv.ParseUint(strings.Trim(encodedBits, ";-/"), 36, 64)
	if err != nil {
		return hash, 0
	}
	return hash, Flag(parsed)
}

func encodeBits(bits Flag) string {
	if bits == 0 {
		return ""
	}
	return "-" + strconv.FormatUint(uint64(bits), 36)
}

func safeJoin(base, relative string) (string, error) {
	cleaned, ok := cleanRelativePath(relative)
	if !ok {
		return "", fmt.Errorf("invalid relative path %q", relative)
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	full := filepath.Join(baseAbs, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(baseAbs, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes base directory")
	}
	return full, nil
}
