package dispatch

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"sort"
	"strings"
	"sync"
)

const (
	TypeCSS = "css"
	TypeJS  = "js"
	TypeIMG = "img"

	typePreload       = "_preload"
	priorityPreloaded = -1

	PriorityPreload = 1
	PriorityHigh    = 10
	PriorityDefault = 500
	PriorityLow     = 1000
)

// Attrs contains HTML attributes for generated include tags. A value of true
// or nil renders as a valueless boolean attribute.
type Attrs map[string]any

// Resource is an item stored in ResourceStore.
type Resource struct {
	URI      string
	Attrs    Attrs
	Priority int
}

type resourceBucket struct {
	items []Resource
	index map[string]int
}

// ResourceStore keeps CSS and JavaScript includes unique and priority sorted.
type ResourceStore struct {
	mu      sync.RWMutex
	buckets map[string]map[int]*resourceBucket
}

// NewResourceStore creates an empty resource store.
func NewResourceStore() *ResourceStore {
	return &ResourceStore{buckets: map[string]map[int]*resourceBucket{}}
}

func (s *ResourceStore) ensureBuckets() {
	if s.buckets == nil {
		s.buckets = map[string]map[int]*resourceBucket{}
	}
}

// Clear removes all resources, or only resources of resourceType when supplied.
func (s *ResourceStore) Clear(resourceType ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(resourceType) == 0 || resourceType[0] == "" {
		s.buckets = map[string]map[int]*resourceBucket{}
		return
	}
	delete(s.buckets, resourceType[0])
}

// AddResource adds or updates a resource.
func (s *ResourceStore) AddResource(resourceType, uri string, attrs Attrs, priority int) *ResourceStore {
	if uri == "" {
		return s
	}
	attrs = defaultAttrs(resourceType, attrs)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureBuckets()
	if s.buckets[resourceType] == nil {
		s.buckets[resourceType] = map[int]*resourceBucket{}
	}
	bucket := s.buckets[resourceType][priority]
	if bucket == nil {
		bucket = &resourceBucket{index: map[string]int{}}
		s.buckets[resourceType][priority] = bucket
	}
	if idx, ok := bucket.index[uri]; ok {
		bucket.items[idx].Attrs = attrs
		return s
	}
	bucket.index[uri] = len(bucket.items)
	bucket.items = append(bucket.items, Resource{URI: uri, Attrs: attrs, Priority: priority})
	if priority == PriorityPreload {
		s.preloadLocked(resourceType, uri)
	}
	return s
}

// RequireCSS stores one or more CSS URLs.
func (s *ResourceStore) RequireCSS(uri string, attrs Attrs, priority int) *ResourceStore {
	return s.AddResource(TypeCSS, uri, attrs, priority)
}

// RequireJS stores one or more JavaScript URLs.
func (s *ResourceStore) RequireJS(uri string, attrs Attrs, priority int) *ResourceStore {
	return s.AddResource(TypeJS, uri, attrs, priority)
}

// RequireInlineCSS stores inline CSS content.
func (s *ResourceStore) RequireInlineCSS(stylesheet string, attrs Attrs, priority int) *ResourceStore {
	return s.AddResource(TypeCSS, inlineKey(stylesheet), mergeAttrs(attrs, Attrs{"_": stylesheet}), priority)
}

// RequireInlineJS stores inline JavaScript content.
func (s *ResourceStore) RequireInlineJS(script string, attrs Attrs, priority int) *ResourceStore {
	return s.AddResource(TypeJS, inlineKey(script), mergeAttrs(attrs, Attrs{"_": script}), priority)
}

// PreloadResource adds a preload tag entry for uri.
func (s *ResourceStore) PreloadResource(resourceType, uri string) *ResourceStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preloadLocked(resourceType, uri)
	return s
}

func (s *ResourceStore) preloadLocked(resourceType, uri string) {
	as := ""
	switch resourceType {
	case TypeCSS:
		as = "style"
	case TypeJS:
		as = "script"
	default:
		return
	}
	s.ensureBuckets()
	if s.buckets[typePreload] == nil {
		s.buckets[typePreload] = map[int]*resourceBucket{}
	}
	bucket := s.buckets[typePreload][priorityPreloaded]
	if bucket == nil {
		bucket = &resourceBucket{index: map[string]int{}}
		s.buckets[typePreload][priorityPreloaded] = bucket
	}
	if idx, ok := bucket.index[uri]; ok {
		bucket.items[idx].Attrs = Attrs{"as": as}
		return
	}
	bucket.index[uri] = len(bucket.items)
	bucket.items = append(bucket.items, Resource{URI: uri, Attrs: Attrs{"as": as}, Priority: priorityPreloaded})
}

// Resources returns resources sorted by ascending priority. Priorities in
// excludePriority are skipped.
func (s *ResourceStore) Resources(resourceType string, excludePriority ...int) []Resource {
	return s.ResourcesAt(resourceType, nil, excludePriority...)
}

// ResourcesAt returns resources for one priority when priority is non-nil,
// otherwise all priorities sorted ascending.
func (s *ResourceStore) ResourcesAt(resourceType string, priority *int, excludePriority ...int) []Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byPriority := s.buckets[resourceType]
	if len(byPriority) == 0 {
		return nil
	}
	excluded := map[int]bool{}
	for _, p := range excludePriority {
		excluded[p] = true
	}

	var priorities []int
	if priority != nil {
		priorities = []int{*priority}
	} else {
		for p := range byPriority {
			priorities = append(priorities, p)
		}
		sort.Ints(priorities)
	}

	var resources []Resource
	for _, p := range priorities {
		if excluded[p] {
			continue
		}
		bucket := byPriority[p]
		if bucket == nil {
			continue
		}
		for _, resource := range bucket.items {
			resources = append(resources, Resource{
				URI:      resource.URI,
				Attrs:    cloneAttrs(resource.Attrs),
				Priority: resource.Priority,
			})
		}
	}
	return resources
}

// GenerateHTMLPreloads renders all preload resources.
func (s *ResourceStore) GenerateHTMLPreloads() string {
	var b strings.Builder
	priority := priorityPreloaded
	for _, resource := range s.ResourcesAt(typePreload, &priority) {
		as, _ := resource.Attrs["as"].(string)
		b.WriteString(fmt.Sprintf(`<link rel="preload" href="%s" as="%s">`, html.EscapeString(resource.URI), html.EscapeString(as)))
	}
	return b.String()
}

// GenerateHTMLIncludes renders CSS or JavaScript include tags.
func (s *ResourceStore) GenerateHTMLIncludes(resourceType string) template.HTML {
	return s.GenerateHTMLIncludesAt(resourceType, nil)
}

// GenerateHTMLIncludesAt renders include tags for one priority when priority is non-nil.
func (s *ResourceStore) GenerateHTMLIncludesAt(resourceType string, priority *int, excludePriority ...int) template.HTML {
	var b strings.Builder
	for _, resource := range s.ResourcesAt(resourceType, priority, excludePriority...) {
		if isInlineKey(resource.URI) {
			b.WriteString(renderInline(resourceType, resource.Attrs))
			continue
		}
		if resource.URI == "" {
			continue
		}
		switch resourceType {
		case TypeJS:
			b.WriteString(fmt.Sprintf(`<script src="%s"%s></script>`, html.EscapeString(resource.URI), renderAttrs(resource.Attrs, nil)))
		default:
			b.WriteString(fmt.Sprintf(`<link href="%s"%s>`, html.EscapeString(resource.URI), renderAttrs(resource.Attrs, nil)))
		}
	}
	return template.HTML(b.String())
}

func defaultAttrs(resourceType string, attrs Attrs) Attrs {
	out := cloneAttrs(attrs)
	if out == nil {
		out = Attrs{}
	}
	if resourceType == TypeCSS {
		if _, ok := out["rel"]; !ok {
			out["rel"] = "stylesheet"
		}
		if _, ok := out["type"]; !ok {
			out["type"] = "text/css"
		}
	}
	return out
}

func renderInline(resourceType string, attrs Attrs) string {
	content, _ := attrs["_"].(string)
	renderedAttrs := renderAttrs(attrs, map[string]bool{"_": true, "rel": true})
	switch resourceType {
	case TypeCSS:
		return "<style" + renderedAttrs + ">" + content + "</style>"
	case TypeJS:
		return "<script" + renderedAttrs + ">" + content + "</script>"
	default:
		return content
	}
}

func renderAttrs(attrs Attrs, skip map[string]bool) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		if skip != nil && skip[key] {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		value := attrs[key]
		if value == nil {
			b.WriteByte(' ')
			b.WriteString(html.EscapeString(key))
			continue
		}
		if boolValue, ok := value.(bool); ok && boolValue {
			b.WriteByte(' ')
			b.WriteString(html.EscapeString(key))
			continue
		} else if ok && !boolValue {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(html.EscapeString(key))
		b.WriteString(`="`)
		b.WriteString(html.EscapeString(fmt.Sprint(value)))
		b.WriteByte('"')
	}
	return b.String()
}

func inlineKey(content string) string {
	sum := md5.Sum([]byte(content))
	return hex.EncodeToString(sum[:])
}

func isInlineKey(uri string) bool {
	if len(uri) != 32 || strings.Contains(uri, "/") {
		return false
	}
	for _, r := range uri {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func cloneAttrs(attrs Attrs) Attrs {
	if attrs == nil {
		return nil
	}
	out := Attrs{}
	for key, value := range attrs {
		out[key] = value
	}
	return out
}

func mergeAttrs(base, extra Attrs) Attrs {
	out := cloneAttrs(base)
	if out == nil {
		out = Attrs{}
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}
