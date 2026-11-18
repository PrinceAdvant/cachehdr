package cachehdr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFingerprint(t *testing.T) {
	cases := map[string]bool{
		"/assets/index.3f9a1c2b.js":    true,
		"/assets/index-BqK7x2Lm9.css":  true,
		"/_next/static/chunks/main.js": true,
		"/js/app.js":                   false,
		"/img/logo.png":                false,
		"/css/style.min.css":           false,
	}
	for p, want := range cases {
		if got := IsFingerprinted(p); got != want {
			t.Errorf("%s: got %v want %v", p, got, want)
		}
	}
}

func TestJudge(t *testing.T) {
	if ok, _ := Judge(KindHTML, "no-cache"); !ok {
		t.Error("no-cache html should pass")
	}
	if ok, _ := Judge(KindHTML, "max-age=3600"); ok {
		t.Error("1h html should fail")
	}
	if ok, adv := Judge(KindHTML, "s-maxage=31536000"); !ok || adv == "" {
		t.Error("s-maxage only html should pass with advice")
	}
	if ok, _ := Judge(KindFingerprint, "public, max-age=31536000, immutable"); !ok {
		t.Error("immutable hashed should pass")
	}
	if ok, _ := Judge(KindFingerprint, "max-age=600"); ok {
		t.Error("short hashed should fail")
	}
	if ok, _ := Judge(KindPlain, "max-age=31536000"); ok {
		t.Error("long plain should fail")
	}
}

func TestAudit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprint(w, `<html><head><link rel="stylesheet" href="/assets/app.1a2b3c4d.css"><script src="/js/app.js"></script><script src="https://cdn.example.com/x.js"></script></head></html>`)
	})
	mux.HandleFunc("/assets/app.1a2b3c4d.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	})
	mux.HandleFunc("/js/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=31536000")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	res, err := Audit(context.Background(), srv.URL+"/", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("want 3 results, got %d", len(res))
	}
	if !res[0].OK || !res[1].OK || res[2].OK {
		t.Fatalf("unexpected verdicts: %+v", res)
	}
	u, _ := url.Parse(srv.URL)
	if n := len(Refs(u, `<img src="data:x"><img src="/a.png"><img src="/a.png">`)); n != 1 {
		t.Fatalf("refs dedupe failed: %d", n)
	}
}
