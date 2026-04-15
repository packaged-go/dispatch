package dispatch

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResourceProcessingVariants(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "resources/css/app.css", `
		@import 'theme.css';
		.icon { background: url(./img/icon.svg#icon); }
		.data { background: url(data:image/png;base64,abc); }
		.remote { background: url(https://example.com/x.png); }
		.missing { background: url(missing.png); }
	`)
	writeTestFile(t, root, "resources/css/theme.css", "body { color: blue; }")
	writeTestFile(t, root, "resources/css/img/icon.svg", "<svg></svg>")

	d := New(root, WithBaseURI("/assets"))
	manager := d.Resources()
	fullPath, err := manager.FilePath("css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	raw := mustReadTestFile(t, fullPath)
	processed := processCSS(manager, "css/app.css", fullPath, raw)

	for _, want := range []string{
		`@import '/assets/r/`,
		`url(/assets/r/`,
		`data:image/png;base64,abc`,
		`https://example.com/x.png`,
		`missing.png`,
	} {
		if !strings.Contains(processed, want) {
			t.Fatalf("expected processed CSS to contain %q: %s", want, processed)
		}
	}

	unmodified := `@do-not-dispatch
		.icon { background: url(./img/icon.svg); }`
	if got := processCSS(manager, "css/app.css", fullPath, unmodified); !strings.Contains(got, "url(./img/icon.svg)") {
		t.Fatalf("do-not-dispatch was not honored: %s", got)
	}

	notMinified := "@do-not-minify\nbody {\n color: red;\n}"
	if got := processCSS(manager, "css/app.css", fullPath, notMinified); got != notMinified {
		t.Fatalf("do-not-minify was not honored: %q", got)
	}
}

func TestResourceHelperBranches(t *testing.T) {
	if _, _, ok := parseCSSURL("url("); ok {
		t.Fatalf("expected invalid CSS url parse")
	}

	if value, quote := importValueAndQuote("a.css", "", ""); value != "a.css" || quote != `"` {
		t.Fatalf("unexpected double-quoted import parse")
	}
	if value, quote := importValueAndQuote("", "a.css", ""); value != "a.css" || quote != `'` {
		t.Fatalf("unexpected single-quoted import parse")
	}
	if value, quote := importValueAndQuote("", "", "a.css"); value != "a.css" || quote != "" {
		t.Fatalf("unexpected bare import parse")
	}

	appendageCases := map[string]string{
		"app.css":       "",
		"app.css?a=b":   "?a=b",
		"app.css#x":     "#x",
		"app.css?a=b#x": "?a=b#x",
		"app.css#x?a=b": "#x?a=b",
	}
	for input, wantAppend := range appendageCases {
		_, gotAppend := splitURLAppendage(input)
		if gotAppend != wantAppend {
			t.Fatalf("splitURLAppendage(%q) = %q, want %q", input, gotAppend, wantAppend)
		}
	}

	root := t.TempDir()
	writeTestFile(t, root, "resources/js/app.min.js", "function app() {\n return 1;\n}")
	writeTestFile(t, root, "resources/js/raw.js", "@do-not-minify\nfunction app() {\n return 1;\n}")
	writeTestFile(t, root, "resources/js/mod.js", `import value from "./missing";`)
	writeTestFile(t, root, "resources/js/no-dispatch.js", "@do-not-dispatch\nimport value from \"./mod\";")

	d := New(root)
	manager := d.Resources()
	minPath := filepath.Join(root, "resources/js/app.min.js")
	if got := processJS(manager, "js/app.min.js", minPath, mustReadTestFile(t, minPath)); strings.Contains(got, "function app(){") {
		t.Fatalf("pre-minified JS should not be minified: %s", got)
	}
	rawPath := filepath.Join(root, "resources/js/raw.js")
	if got := processJS(manager, "js/raw.js", rawPath, mustReadTestFile(t, rawPath)); !strings.Contains(got, "\n") {
		t.Fatalf("do-not-minify JS should retain whitespace: %s", got)
	}
	modPath := filepath.Join(root, "resources/js/mod.js")
	if got := processJS(manager, "js/mod.js", modPath, mustReadTestFile(t, modPath)); !strings.Contains(got, `"./missing"`) {
		t.Fatalf("missing import should remain unchanged: %s", got)
	}
	noDispatchPath := filepath.Join(root, "resources/js/no-dispatch.js")
	if got := processJS(manager, "js/no-dispatch.js", noDispatchPath, mustReadTestFile(t, noDispatchPath)); !strings.Contains(got, `"./mod"`) {
		t.Fatalf("do-not-dispatch JS should retain import: %s", got)
	}

	for ext, want := range map[string]string{
		".json":  "application/json",
		".svg":   "image/svg+xml",
		".woff":  "font/woff",
		".woff2": "font/woff2",
		".ttf":   "font/ttf",
		".otf":   "font/otf",
		".eot":   "application/vnd.ms-fontobject",
		".jpg":   "image/jpeg",
		".mp4":   "video/mp4",
		".bin":   "application/octet-stream",
	} {
		if got := contentTypeForExtension(ext); got != want {
			t.Fatalf("contentTypeForExtension(%q) = %q, want %q", ext, got, want)
		}
	}

	if got := makeRelativeResourcePath("/img/logo.png", "css"); got != "img/logo.png" {
		t.Fatalf("unexpected absolute resource path: %s", got)
	}
	if got := makeRelativeResourcePath("./logo.png", "."); got != "logo.png" {
		t.Fatalf("unexpected current-directory resource path: %s", got)
	}
	if got := pathClean("."); got != "" {
		t.Fatalf("unexpected clean path: %s", got)
	}
}

func mustReadTestFile(t *testing.T, path string) string {
	t.Helper()
	data := readFileBytes(t, path)
	return string(data)
}
