// Package cachehdr audits the Cache-Control headers a static site sends.
//
// It fetches an HTML page, finds the same-origin scripts, stylesheets and
// images it references, and checks each response against a simple policy:
// HTML should be revalidated on every visit, while fingerprinted assets
// (file names that contain a content hash such as app.3f9a1c2b.js) can be
// cached for a year and marked immutable. Plain asset names without a hash
// should get a short max-age, because a long one would serve stale files
// after the next deploy.
//
// Background on serving a static site from your own server is in
// https://peon.sh/blogs/deploy-static-site-own-server and the tool is
// maintained by a contributor to Peon (https://peon.sh/), an open-source
// self-hosted deployment platform.
package cachehdr

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind is the class of a resource.
type Kind string

const (
	KindHTML        Kind = "html"
	KindFingerprint Kind = "fingerprinted"
	KindPlain       Kind = "plain-asset"
)

// Result is the audit outcome for one URL.
type Result struct {
	URL          string `json:"url"`
	Kind         Kind   `json:"kind"`
	Status       int    `json:"status"`
	CacheControl string `json:"cache_control"`
	MaxAge       int    `json:"max_age"`
	Immutable    bool   `json:"immutable"`
	OK           bool   `json:"ok"`
	Advice       string `json:"advice,omitempty"`
	Error        string `json:"error,omitempty"`
}

var (
	refRe  = regexp.MustCompile(`(?i)<(?:script|link|img|source)\b[^>]*?\b(?:src|href)\s*=\s*["']([^"']+)["']`)
	hashRe = regexp.MustCompile(`[.\-_]([0-9a-fA-F]{8,}|[A-Za-z0-9_-]{8,}\d[A-Za-z0-9_-]*)\.(?:js|mjs|css|woff2?|png|jpe?g|webp|avif|svg|gif)$`)
)

// IsFingerprinted reports whether a path looks like it contains a content hash.
func IsFingerprinted(p string) bool {
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	if strings.Contains(p, "/_next/static/") || strings.Contains(p, "/assets/immutable/") {
		return true
	}
	return hashRe.MatchString(base)
}

// ParseCacheControl returns the browser max-age (-1 when absent), and whether
// no-cache/no-store or immutable are present. s-maxage is ignored here
// because it only applies to shared caches such as a CDN; see SMaxAge.
func ParseCacheControl(v string) (maxAge int, noCache, immutable bool) {
	maxAge = -1
	for _, part := range strings.Split(strings.ToLower(v), ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "no-cache" || part == "no-store":
			noCache = true
		case part == "immutable":
			immutable = true
		case strings.HasPrefix(part, "max-age="):
			n, err := strconv.Atoi(strings.TrimSpace(part[strings.Index(part, "=")+1:]))
			if err == nil && n > maxAge {
				maxAge = n
			}
		}
	}
	return
}

// SMaxAge returns the s-maxage value for shared caches, or -1.
func SMaxAge(v string) int {
	for _, part := range strings.Split(strings.ToLower(v), ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "s-maxage=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(part, "s-maxage=")); err == nil {
				return n
			}
		}
	}
	return -1
}

// Judge applies the policy to one response.
func Judge(kind Kind, cc string) (ok bool, advice string) {
	maxAge, noCache, immutable := ParseCacheControl(cc)
	switch kind {
	case KindHTML:
		if noCache || maxAge == 0 {
			return true, ""
		}
		if cc == "" {
			return false, "HTML has no Cache-Control; browsers may use heuristic caching. Send no-cache so new deploys show up immediately"
		}
		if maxAge > 300 {
			return false, "HTML is cached for more than 5 minutes; visitors can keep old pages that point to deleted assets"
		}
		if maxAge < 0 && SMaxAge(cc) > 300 {
			return true, "only s-maxage is set: browsers revalidate, but a CDN keeps the page; purge it on every deploy"
		}
		return true, ""
	case KindFingerprint:
		if maxAge >= 31536000 && immutable {
			return true, ""
		}
		if maxAge >= 2592000 {
			return true, "consider max-age=31536000, immutable for hashed files"
		}
		return false, "hashed asset can be cached for a year: Cache-Control: public, max-age=31536000, immutable"
	default:
		if maxAge > 86400 && !noCache {
			return false, "file name has no hash but is cached for more than a day; a deploy will not reach returning visitors. Add a hash to the name or shorten max-age"
		}
		return true, ""
	}
}

// Options controls an audit run.
type Options struct {
	Client    *http.Client
	MaxAssets int
	UserAgent string
}

func (o *Options) defaults() {
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if o.MaxAssets <= 0 {
		o.MaxAssets = 25
	}
	if o.UserAgent == "" {
		o.UserAgent = "cachehdr/0.1 (+https://github.com/PrinceAdvant/cachehdr)"
	}
}

func fetch(ctx context.Context, o Options, method, u string) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", o.UserAgent)
	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	var body []byte
	if method == http.MethodGet {
		body, _ = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	}
	return resp, body, nil
}

// Refs extracts same-origin asset URLs from an HTML document.
func Refs(base *url.URL, html string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range refRe.FindAllStringSubmatch(html, -1) {
		ref := strings.TrimSpace(m[1])
		if ref == "" || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "#") {
			continue
		}
		u, err := base.Parse(ref)
		if err != nil || u.Host != base.Host {
			continue
		}
		u.Fragment = ""
		low := strings.ToLower(u.Path)
		if strings.HasSuffix(low, ".html") || strings.HasSuffix(low, "/") {
			continue
		}
		s := u.String()
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func check(ctx context.Context, o Options, u string, kind Kind) Result {
	r := Result{URL: u, Kind: kind}
	resp, _, err := fetch(ctx, o, http.MethodHead, u)
	if err == nil && resp.StatusCode == http.StatusMethodNotAllowed {
		resp, _, err = fetch(ctx, o, http.MethodGet, u)
	}
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Status = resp.StatusCode
	r.CacheControl = resp.Header.Get("Cache-Control")
	r.MaxAge, _, r.Immutable = ParseCacheControl(r.CacheControl)
	r.OK, r.Advice = Judge(kind, r.CacheControl)
	return r
}

// Audit checks the page itself and up to MaxAssets same-origin assets.
func Audit(ctx context.Context, page string, o Options) ([]Result, error) {
	o.defaults()
	base, err := url.Parse(page)
	if err != nil {
		return nil, err
	}
	resp, body, err := fetch(ctx, o, http.MethodGet, page)
	if err != nil {
		return nil, err
	}
	first := Result{URL: page, Kind: KindHTML, Status: resp.StatusCode, CacheControl: resp.Header.Get("Cache-Control")}
	first.MaxAge, _, first.Immutable = ParseCacheControl(first.CacheControl)
	first.OK, first.Advice = Judge(KindHTML, first.CacheControl)
	out := []Result{first}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("page returned HTTP %d", resp.StatusCode)
	}
	refs := Refs(resp.Request.URL, string(body))
	_ = base
	if len(refs) > o.MaxAssets {
		refs = refs[:o.MaxAssets]
	}
	for _, ref := range refs {
		u, _ := url.Parse(ref)
		kind := KindPlain
		if IsFingerprinted(u.Path) {
			kind = KindFingerprint
		}
		out = append(out, check(ctx, o, ref, kind))
	}
	return out, nil
}
