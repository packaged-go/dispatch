package dispatch

import (
	"os"
	"strings"
	"testing"
)

func TestResourceStoreZeroValuePriorityPreloadAndClear(t *testing.T) {
	var store ResourceStore
	store.RequireCSS("css/default.css", nil, PriorityDefault)
	store.RequireCSS("css/high.css", nil, PriorityHigh)
	store.RequireCSS("css/preload.css", nil, PriorityPreload)
	store.RequireJS("js/app.js", Attrs{"async": true, "defer": false, "data-id": 7}, PriorityDefault)
	store.RequireInlineJS("alert('hi');", nil, PriorityLow)

	resources := store.Resources(TypeCSS)
	if got := []string{resources[0].URI, resources[1].URI, resources[2].URI}; strings.Join(got, ",") != "css/preload.css,css/high.css,css/default.css" {
		t.Fatalf("unexpected priority order: %#v", got)
	}
	high := PriorityHigh
	if got := store.ResourcesAt(TypeCSS, &high); len(got) != 1 || got[0].URI != "css/high.css" {
		t.Fatalf("unexpected high priority resources: %#v", got)
	}
	if got := store.Resources(TypeCSS, PriorityPreload); len(got) != 2 {
		t.Fatalf("expected preload exclusion, got %#v", got)
	}

	if got := store.GenerateHTMLPreloads(); got != `<link rel="preload" href="css/preload.css" as="style">` {
		t.Fatalf("unexpected preloads: %s", got)
	}
	js := store.GenerateHTMLIncludes(TypeJS)
	if !strings.Contains(string(js), `async`) || strings.Contains(js, `defer`) || !strings.Contains(js, `data-id="7"`) {
		t.Fatalf("unexpected JS attributes: %s", js)
	}
	if !strings.Contains(string(js), `<script>alert('hi');</script>`) {
		t.Fatalf("expected inline JS: %s", js)
	}

	store.Clear(TypeJS)
	if got := store.GenerateHTMLIncludes(TypeJS); got != "" {
		t.Fatalf("expected JS to be cleared: %s", got)
	}
	if got := store.GenerateHTMLIncludes(TypeCSS); got == "" {
		t.Fatalf("expected CSS to remain after clearing JS")
	}
	store.Clear()
	if got := store.GenerateHTMLIncludes(TypeCSS); got != "" {
		t.Fatalf("expected store to be cleared: %s", got)
	}
}

func TestResourceStoreUpdatesAndPreloadResource(t *testing.T) {
	store := NewResourceStore()
	store.AddResource(TypeJS, "", nil, PriorityDefault)
	store.RequireJS("js/app.js", Attrs{"type": "module"}, PriorityDefault)
	store.RequireJS("js/app.js", Attrs{"defer": nil}, PriorityDefault)
	if resources := store.Resources(TypeJS); len(resources) != 1 || resources[0].Attrs["defer"] != nil {
		t.Fatalf("expected duplicate URI to update attrs: %#v", resources)
	}

	store.PreloadResource(TypeJS, "js/app.js")
	store.PreloadResource(TypeIMG, "img/noop.png")
	if got := store.GenerateHTMLPreloads(); got != `<link rel="preload" href="js/app.js" as="script">` {
		t.Fatalf("unexpected preload output: %s", got)
	}
	store.PreloadResource(TypeCSS, "shared")
	store.PreloadResource(TypeJS, "shared")
	if got := store.GenerateHTMLPreloads(); !strings.Contains(got, `href="shared" as="script"`) {
		t.Fatalf("expected duplicate preload to update attrs: %s", got)
	}
	missingPriority := 99
	if got := store.ResourcesAt(TypeCSS, &missingPriority); len(got) != 0 {
		t.Fatalf("expected missing priority to return no resources: %#v", got)
	}
	if got := store.GenerateHTMLIncludesAt(TypeCSS, &missingPriority); got != "" {
		t.Fatalf("expected missing priority includes to be empty: %s", got)
	}

	if got := renderInline(TypeIMG, Attrs{"_": "content"}); got != "content" {
		t.Fatalf("unexpected generic inline render: %s", got)
	}
	if got := renderAttrs(nil, nil); got != "" {
		t.Fatalf("expected empty attrs: %s", got)
	}
	if isInlineKey("not-an-inline-key") {
		t.Fatalf("unexpected inline key match")
	}
	if isInlineKey("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz") {
		t.Fatalf("invalid hex inline key should not match")
	}
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
