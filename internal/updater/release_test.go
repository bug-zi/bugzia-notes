package updater

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const atomFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/" xml:lang="en-US">
  <id>tag:github.com,2008:https://github.com/bug-zi/bugzia-notes/releases</id>
  <link type="text/html" rel="alternate" href="https://github.com/bug-zi/bugzia-notes/releases"/>
  <title>Release notes from bugzia-notes</title>
  <entry>
    <id>tag:github.com,2008:https://github.com/bug-zi/bugzia-notes/releases/409909871</id>
    <updated>2026-11-18T06:59:27Z</updated>
    <link type="text/html" rel="alternate" href="https://github.com/bug-zi/bugzia-notes/releases/tag/v2.0.0"/>
    <title>v2.0.0</title>
  </entry>
  <entry>
    <id>tag:github.com,2008:https://github.com/bug-zi/bugzia-notes/releases/400000000</id>
    <updated>2026-11-17T10:00:00Z</updated>
    <link type="text/html" rel="alternate" href="https://github.com/bug-zi/bugzia-notes/releases/tag/v1.0.0"/>
    <title>v1.0.0</title>
  </entry>
</feed>`

func newAtomTestConfig(ts *httptest.Server) *Config {
	return &Config{RepoBase: ts.URL, AssetName: "bugzia-notes.exe", Client: ts.Client()}
}

func TestLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases.atom" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(atomFixture))
	}))
	defer ts.Close()

	rel, err := Latest(newAtomTestConfig(ts))
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != "v2.0.0" {
		t.Fatalf("应取最新条目 tag v2.0.0, got %q", rel.Tag)
	}
	want := ts.URL + "/releases/latest/download/bugzia-notes.exe"
	if rel.AssetURL != want {
		t.Fatalf("下载直链错误: got %q want %q", rel.AssetURL, want)
	}
	if rel.AssetSize != 0 {
		t.Fatalf("atom 路径无声明尺寸, AssetSize 应为 0: %d", rel.AssetSize)
	}
}

func TestLatestEmptyFeed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>empty</title></feed>`))
	}))
	defer ts.Close()
	if _, err := Latest(newAtomTestConfig(ts)); err == nil {
		t.Fatalf("空 feed（无发布）应报错")
	}
}

func TestLatestBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()
	if _, err := Latest(newAtomTestConfig(ts)); err == nil {
		t.Fatalf("非 200 应报错")
	}
}

func TestLatestBadFeed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not xml at all"))
	}))
	defer ts.Close()
	if _, err := Latest(newAtomTestConfig(ts)); err == nil {
		t.Fatalf("坏 feed 应报错")
	}
}
