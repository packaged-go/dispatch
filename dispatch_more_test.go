package dispatch

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDispatcherOptionsAndConvenience(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "resources/css/app.css", "body { color: red; }")
	writeTestFile(t, root, "public/favicon.ico", "ico")

	d := New(root)
	defaultURL := d.MustURL("css/app.css")
	saltedURL := New(root, WithHashSalt("salt")).MustURL("css/app.css")
	if defaultURL == saltedURL {
		t.Fatalf("expected hash salt to change URL")
	}

	u, err := d.URL("css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	if u != defaultURL {
		t.Fatalf("URL and MustURL mismatch: %q != %q", u, defaultURL)
	}

	publicURL, err := d.Public().URL("favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	d.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestTarget(publicURL), nil))
	if recorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected public resource to serve")
	}
	if d.PublicPath() != filepath.Join(root, PublicDir) {
		t.Fatalf("unexpected public path: %s", d.PublicPath())
	}

	customCache := CacheConfig{Vary: "Accept-Encoding", Duration: 2 * time.Second}
	d = New(root, WithCacheConfig(customCache))
	recorder = httptest.NewRecorder()
	d.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestTarget(d.MustURL("css/app.css")), nil))
	if got := recorder.Result().Header.Get("Cache-Control"); got != "public, max-age=2" {
		t.Fatalf("unexpected custom cache-control: %q", got)
	}
	if got := recorder.Result().Header.Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("unexpected vary header: %q", got)
	}

	store := NewResourceStore()
	d.SetResourceStore(store)
	if d.Store() != store {
		t.Fatalf("custom store was not installed")
	}
	d.SetResourceStore(nil)
	if d.Store() == nil || d.Store() == store {
		t.Fatalf("nil store should reset to a fresh store")
	}

	absAlias := filepath.Join(root, "public")
	d.AddAlias("abs", absAlias)
	if got, ok := d.AliasPath("abs"); !ok || got != absAlias {
		t.Fatalf("absolute alias was not preserved: %q %v", got, ok)
	}
	if got := d.RelativePath(filepath.Join(filepath.Dir(root), "outside.css")); !strings.HasSuffix(got, "outside.css") {
		t.Fatalf("unexpected outside relative path: %s", got)
	}
}

func TestDispatcherRouteErrorsAndBasePath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "resources/css/app.css", "body{}")
	writeTestFile(t, root, "shared/app.css", "body{}")
	writeTestFile(t, root, "vendor/acme/widgets/css/widget.css", "body{}")

	d := New(root, WithBaseURI("/assets"))
	d.AddAlias("shared", "shared")
	d.AddVendorAlias("acme", "widgets", "aw")

	for _, target := range []string{
		"/assets/not-dispatch",
		"/assets/r/badhash/css/app.css",
		"/assets/r/badhash/css%2Fapp.css",
		"/assets/a/missing/badhash/app.css",
	} {
		recorder := httptest.NewRecorder()
		d.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		if recorder.Result().StatusCode != http.StatusNotFound {
			t.Fatalf("expected %s to 404, got %d", target, recorder.Result().StatusCode)
		}
	}

	aliasURL := d.Alias("shared").MustURL("app.css")
	recorder := httptest.NewRecorder()
	d.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, aliasURL, nil))
	if recorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected alias URL to serve")
	}

	vendorURL := d.Vendor("acme", "widgets").MustURL("css/widget.css")
	recorder = httptest.NewRecorder()
	d.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, vendorURL, nil))
	if recorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected vendor alias URL to serve")
	}

	d = New(root, WithBaseURI("https://cdn.example.com/assets"))
	if got := d.Resources().MustURL("css/app.css"); !strings.HasPrefix(got, "https://cdn.example.com/assets/r/") {
		t.Fatalf("absolute base URI was not preserved: %s", got)
	}
}

func TestDispatcherRouteParsingAndMiddlewareDefaults(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "resources/css/app.css", "body{}")
	writeTestFile(t, root, "public/app.css", "body{}")
	writeTestFile(t, root, "vendor/acme/widgets/app.css", "body{}")

	d := New(root, WithBaseURI("/assets"))
	for target, want := range map[string]bool{
		"/assets/r/hash/app.css":              true,
		"/assets/p/hash/app.css":              true,
		"/assets/a/shared/hash/app.css":       true,
		"/assets/v/acme/widgets/hash/app.css": true,
		"/assets/i/hash/app.css":              false,
		"/assets/r":                           false,
		"/other/r/hash/app.css":               false,
	} {
		if got := d.matchesDispatchPath(target); got != want {
			t.Fatalf("matchesDispatchPath(%q) = %v, want %v", target, got, want)
		}
	}

	for _, target := range []string{
		"/assets/p/hash/app.css",
		"/assets/v/acme/widgets/hash/app.css",
	} {
		if _, _, _, ok := d.managerFromRequest(httptest.NewRequest(http.MethodGet, target, nil)); !ok {
			t.Fatalf("expected manager from %s", target)
		}
	}
	if _, _, _, ok := d.managerFromRequest(httptest.NewRequest(http.MethodGet, "/assets/e/hash/app.css", nil)); ok {
		t.Fatalf("external map should not be served")
	}
	if parts := d.requestParts("/assets"); len(parts) != 0 {
		t.Fatalf("expected base path to produce no parts: %#v", parts)
	}

	recorder := httptest.NewRecorder()
	d.Middleware(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/not-dispatch", nil))
	if recorder.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("nil middleware fallback should be not found")
	}
}

func requestTarget(u string) string {
	if strings.HasPrefix(u, "/") || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return "/" + u
}

func TestResourceManagerMetadataAndErrors(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "resources/js/app.js", "console.log('app');")

	d := New(root)
	m := d.Resources()
	if m.MapType() != MapResources {
		t.Fatalf("unexpected map type: %s", m.MapType())
	}
	if len(m.MapOptions()) != 0 {
		t.Fatalf("unexpected map options: %#v", m.MapOptions())
	}

	vendor := d.Vendor("acme", "ui")
	if vendor.MapType() != MapVendor || strings.Join(vendor.MapOptions(), "/") != "acme/ui" {
		t.Fatalf("unexpected vendor metadata")
	}

	customStore := NewResourceStore()
	m.SetResourceStore(customStore)
	if !m.HasResourceStore() {
		t.Fatalf("expected custom store")
	}
	if err := m.RequireJS("js/app.js", nil, PriorityDefault); err != nil {
		t.Fatal(err)
	}
	if len(customStore.Resources(TypeJS)) != 1 {
		t.Fatalf("expected JS in custom store")
	}
	m.UseGlobalResourceStore()
	if m.HasResourceStore() {
		t.Fatalf("expected global store")
	}

	m.IncludeCSS("missing.css", nil, PriorityDefault)
	m.IncludeJS("missing.js", nil, PriorityDefault)

	if _, err := d.Alias("missing").URL("app.css"); err == nil {
		t.Fatalf("expected unknown alias error")
	}
	if _, err := d.Resources().URL("../outside.css"); err == nil {
		t.Fatalf("expected path traversal error")
	}
	if _, err := d.Resources().FileHash(filepath.Join(root, "missing.css")); err == nil {
		t.Fatalf("expected file hash error")
	}

	if got, err := d.External().URL("https://example.com/app.css"); err != nil || got != "https://example.com/app.css" {
		t.Fatalf("unexpected external URL result: %q %v", got, err)
	}
	if got, err := d.Resources().URL("https://example.com/app.css"); err != nil || got != "https://example.com/app.css" {
		t.Fatalf("unexpected external passthrough: %q %v", got, err)
	}
	if err := d.Inline().RequireJS("alert('inline');", nil, PriorityDefault); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Store().GenerateHTMLIncludes(TypeJS), "alert('inline');") {
		t.Fatalf("inline manager did not store JS")
	}

	noDispatcher := newResourceManager(nil, MapResources, nil)
	if noDispatcher.BaseURI() != "r" {
		t.Fatalf("unexpected no-dispatcher base uri")
	}
	if _, err := noDispatcher.URL("x.css"); err == nil {
		t.Fatalf("expected missing dispatcher error")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatalf("expected MustURL panic")
			}
		}()
		_ = noDispatcher.MustURL("x.css")
	}()
	if store := noDispatcher.resourceStore(); store == nil {
		t.Fatalf("expected fallback store")
	}
	if noDispatcher.bits() != 0 {
		t.Fatalf("unexpected no-dispatcher bits")
	}
	if noDispatcher.RelativeHash("x.css") != "" {
		t.Fatalf("unexpected no-dispatcher relative hash")
	}

	if _, err := newResourceManager(d, MapVendor, nil).FilePath("x.css"); err == nil {
		t.Fatalf("expected invalid vendor manager error")
	}
	if _, err := newResourceManager(d, MapAlias, nil).FilePath("x.css"); err == nil {
		t.Fatalf("expected invalid alias manager error")
	}
	if _, err := newResourceManager(d, MapExternal, nil).FilePath("x.css"); err == nil {
		t.Fatalf("expected invalid map type error")
	}

	noWebPURL := New(root, WithWebPOptimization(true), WithAcceptableContentTypes("image/webp")).Resources().MustURL("js/app.js")
	if strings.Contains(noWebPURL, ".webp") {
		t.Fatalf("non-image resource should not be webp optimized: %s", noWebPURL)
	}
}

func TestPathAndHashHelpers(t *testing.T) {
	if got := truncateHash("abcdef", 0); got != "abcdef" {
		t.Fatalf("unexpected untruncated hash: %s", got)
	}
	for _, input := range []string{"", ".", "..", "../x"} {
		if cleaned, ok := cleanRelativePath(input); ok {
			t.Fatalf("expected %q to be rejected, got %q", input, cleaned)
		}
	}
	if cleaned, ok := cleanRelativePath(`/a\..\b.css`); !ok || cleaned != "b.css" {
		t.Fatalf("unexpected cleaned path: %q %v", cleaned, ok)
	}
	if _, err := safeJoin("/tmp/base", "../x"); err == nil {
		t.Fatalf("expected safeJoin traversal error")
	}
	if got := joinURL("/", "assets", "r"); got != "/assets/r" {
		t.Fatalf("unexpected rooted URL: %s", got)
	}
	if got := joinURL("//cdn.example.com", "assets"); got != "//cdn.example.com/assets" {
		t.Fatalf("unexpected protocol-relative URL: %s", got)
	}
	if got := baseURIPath("https://cdn.example.com/assets/"); got != "/assets" {
		t.Fatalf("unexpected base URI path: %s", got)
	}
	if _, got := splitHashAndBits("abc-notbase36!"); got != 0 {
		t.Fatalf("expected invalid bits to decode to zero")
	}
	if got := New(t.TempDir()).requestBits(httptest.NewRequest(http.MethodGet, "/", nil), FlagContentAttachment); got != FlagContentAttachment {
		t.Fatalf("unexpected request bits: %v", got)
	}
	if got := joinURL("assets", ""); got != "assets" {
		t.Fatalf("unexpected simple URL: %s", got)
	}
}
