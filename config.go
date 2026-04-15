package dispatch

import "time"

// Flag controls optional response behavior encoded into generated URLs.
type Flag uint64

const (
	// FlagWebP marks requests that can be varied for WebP-optimized nested assets.
	FlagWebP Flag = 1 << iota
	// FlagContentAttachment makes the served response use Content-Disposition: attachment.
	FlagContentAttachment
)

// CacheConfig controls the cache headers used when a request URL contains the
// current content hash.
type CacheConfig struct {
	Vary      string
	Duration  time.Duration
	Immutable bool
}

func defaultCacheConfig() CacheConfig {
	return CacheConfig{
		Vary:      "Accept-Encoding, Accept",
		Duration:  365 * 24 * time.Hour,
		Immutable: true,
	}
}

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithBaseURI sets the URI prefix used in generated URLs. It can be absolute,
// such as https://assets.example.com, or path-only, such as /assets.
func WithBaseURI(baseURI string) Option {
	return func(d *Dispatcher) {
		d.baseURI = trimSlashes(baseURI)
		if len(baseURI) > 0 && baseURI[0] == '/' {
			d.baseURI = "/" + d.baseURI
		}
	}
}

// WithHashSalt changes the salt used when generating dispatch hashes.
func WithHashSalt(salt string) Option {
	return func(d *Dispatcher) {
		d.hashSalt = salt
	}
}

// WithRequireFileHash requires URLs to contain the current file content hash.
// When disabled, URLs with a matching path hash can still be served without
// long-lived cache headers. This mirrors the PHP package's fallback behavior.
func WithRequireFileHash(require bool) Option {
	return func(d *Dispatcher) {
		d.requireFileHash = require
	}
}

// WithAcceptableContentTypes supplies the request-independent Accept values
// used while generating URLs outside an HTTP request.
func WithAcceptableContentTypes(types ...string) Option {
	return func(d *Dispatcher) {
		d.acceptableTypes = append([]string(nil), types...)
	}
}

// WithWebPOptimization enables .webp substitutions when the dispatcher is
// generating URLs for a client that accepts image/webp.
func WithWebPOptimization(enabled bool) Option {
	return func(d *Dispatcher) {
		d.optimizeWebP = enabled
	}
}

// WithCacheConfig overrides the default lifetime cache settings.
func WithCacheConfig(config CacheConfig) Option {
	return func(d *Dispatcher) {
		d.cache = config
	}
}
