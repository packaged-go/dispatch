# Dispatch

Go resource dispatch for immutable, content-hashed asset URLs.

Dispatch generates URLs with the current file content hash embedded in the path:

```text
/assets/r/6f3a8b2c9d10/css/app.css
```

When `css/app.css` changes, its URL changes. Responses for current content-hash
URLs are served with long-lived public cache headers, including `immutable`, so
CDNs and browsers can cache them for the configured lifetime.

## Install

```sh
go get github.com/packaged-go/dispatch
```

## Quick Guide

Project layout:

```text
myapp/
  resources/
    css/app.css
    js/app.js
    img/logo.png
  public/
    favicon.ico
```

1. Create a dispatcher rooted at your project directory.
2. Mount it at the same path as `WithBaseURI`.
3. Inject generated URLs into templates or handlers with `MustURL` or `URL`.

```go
package main

import (
	"html/template"
	"net/http"

	"github.com/packaged-go/dispatch"
)

func main() {
	assets := dispatch.New(".", dispatch.WithBaseURI("/assets"))

	mux := http.NewServeMux()
	mux.Handle("/assets/", assets)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tpl := template.Must(template.New("page").Parse(`
			<link rel="stylesheet" href="{{ .CSS }}">
			<script src="{{ .JS }}" defer></script>
		`))

		_ = tpl.Execute(w, map[string]string{
			"CSS": assets.MustURL("css/app.css"),
			"JS":  assets.Resources().MustURL("js/app.js"),
		})
	})

	_ = http.ListenAndServe(":8080", mux)
}
```

For an existing handler, use the middleware form instead of manually mounting
the dispatcher:

```go
handler := assets.Middleware(appHandler)
_ = http.ListenAndServe(":8080", handler)
```

## Resource Roots

The built-in managers mirror the source PHP package's route maps:

```go
assets.Resources().MustURL("css/app.css")              // /assets/r/...
assets.Public().MustURL("favicon.ico")                 // /assets/p/...
assets.Alias("shared").MustURL("icons/check.svg")      // /assets/a/shared/...
assets.Vendor("acme", "ui").MustURL("css/widget.css")  // /assets/v/acme/ui/...
```

Aliases are project-root-relative unless an absolute path is supplied:

```go
assets.AddAlias("shared", "frontend/shared")
assets.AddVendorAlias("acme", "ui", "aui")
```

## Include Store

The resource store keeps CSS and JS includes unique and sorted by priority:

```go
_ = assets.Resources().RequireCSS("css/app.css", nil, dispatch.PriorityDefault)
_ = assets.Resources().RequireJS("js/app.js", dispatch.Attrs{"defer": nil}, dispatch.PriorityHigh)
assets.Inline().RequireCSS("body{background:#fff}", nil, dispatch.PriorityLow)

cssHTML := assets.Store().GenerateHTMLIncludes(dispatch.TypeCSS)
jsHTML := assets.Store().GenerateHTMLIncludes(dispatch.TypeJS)
```

## CSS and JavaScript Rewriting

CSS `url(...)` and `@import` values are rewritten to dispatch URLs when the
referenced files exist under the same manager root. JavaScript ES import paths
are also rewritten when they resolve to files. Query strings and fragments are
preserved:

```css
.logo { background-image: url("../img/logo.png?v=1#mark"); }
```

becomes a URL like:

```css
.logo{background-image:url("/assets/r/1a2b3c4d9abc/img/logo.png?v=1#mark")}
```

Add `@do-not-dispatch` or `@do-not-minify` anywhere in a CSS or JS file to skip
that processing step for the file.

## WebP and Attachments

When WebP optimization is enabled and `.webp` siblings exist, generated URLs can
point at the optimized file:

```go
assets := dispatch.New(
	".",
	dispatch.WithWebPOptimization(true),
	dispatch.WithAcceptableContentTypes("image/webp"),
)
```

Attachment behavior is encoded into the URL:

```go
downloadURL := assets.Resources().MustURL("reports/export.pdf", dispatch.FlagContentAttachment)
```

## Cache Behavior

Current content-hash requests receive:

```text
Cache-Control: public, max-age=31536000, immutable
Vary: Accept-Encoding, Accept
ETag: "<processed-content-md5>"
```

By default, Dispatch mirrors the PHP package's compatibility behavior: if a URL
contains a matching path hash but an old content hash, the file can still be
served without the immutable cache headers. To require exact content hashes:

```go
assets := dispatch.New(".", dispatch.WithRequireFileHash(true))
```
