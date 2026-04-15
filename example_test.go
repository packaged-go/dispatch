package dispatch_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/packaged-go/dispatch"
)

func ExampleDispatcher_Middleware() {
	root, cleanup := exampleRoot()
	defer cleanup()

	assets := dispatch.New(root, dispatch.WithBaseURI("/assets"))
	app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "app")
	})
	handler := assets.Middleware(app)

	resourceURL := assets.MustURL("css/app.css")
	resourceResponse := httptest.NewRecorder()
	handler.ServeHTTP(resourceResponse, httptest.NewRequest(http.MethodGet, resourceURL, nil))

	appResponse := httptest.NewRecorder()
	handler.ServeHTTP(appResponse, httptest.NewRequest(http.MethodGet, "/", nil))

	fmt.Println(resourceResponse.Code)
	fmt.Println(strings.HasPrefix(resourceResponse.Header().Get("Cache-Control"), "public, max-age="))
	fmt.Println(appResponse.Body.String())

	// Output:
	// 200
	// true
	// app
}

func ExampleResourceStore() {
	root, cleanup := exampleRoot()
	defer cleanup()

	assets := dispatch.New(root, dispatch.WithBaseURI("/assets"))
	_ = assets.Resources().RequireCSS("css/app.css", nil, dispatch.PriorityDefault)
	_ = assets.Resources().RequireJS("js/app.js", dispatch.Attrs{"defer": nil}, dispatch.PriorityHigh)

	fmt.Println(strings.Contains(assets.Store().GenerateHTMLIncludes(dispatch.TypeCSS), `<link href="/assets/r/`))
	fmt.Println(strings.Contains(assets.Store().GenerateHTMLIncludes(dispatch.TypeJS), `defer`))

	// Output:
	// true
	// true
}

func exampleRoot() (string, func()) {
	root, err := os.MkdirTemp("", "dispatch-example-*")
	if err != nil {
		panic(err)
	}
	writeExampleFile(root, "resources/css/app.css", "body { color: red; }")
	writeExampleFile(root, "resources/js/app.js", "console.log('app');")
	return root, func() {
		_ = os.RemoveAll(root)
	}
}

func writeExampleFile(root, relativePath, content string) {
	fullPath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		panic(err)
	}
}
