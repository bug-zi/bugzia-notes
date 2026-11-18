package updater

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestConfig(ts *httptest.Server) *Config {
	return &Config{RepoAPI: ts.URL + "/releases/latest", AssetName: "bugzia-notes.exe",
		Client: ts.Client()}
}

func TestLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "tag_name": "v1.1.0",
		  "body": "修复若干问题",
		  "assets": [
		    {"name": "notes-src.zip", "browser_download_url": "http://x/src.zip", "size": 10},
		    {"name": "bugzia-notes.exe", "browser_download_url": "http://x/app.exe", "size": 18300000}
		  ]
		}`))
	}))
	defer ts.Close()

	rel, err := Latest(newTestConfig(ts))
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != "v1.1.0" || rel.Notes != "修复若干问题" {
		t.Fatalf("字段解析错误: %+v", rel)
	}
	if rel.AssetURL != "http://x/app.exe" || rel.AssetSize != 18300000 {
		t.Fatalf("资产挑选错误: %+v", rel)
	}
}

func TestLatestAssetMissing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","assets":[{"name":"other.zip","browser_download_url":"http://x/o","size":1}]}`))
	}))
	defer ts.Close()
	if _, err := Latest(newTestConfig(ts)); err == nil {
		t.Fatalf("缺目标资产应报错")
	}
}

func TestLatestBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()
	if _, err := Latest(newTestConfig(ts)); err == nil {
		t.Fatalf("非 200 应报错")
	}
}

func TestLatestBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer ts.Close()
	if _, err := Latest(newTestConfig(ts)); err == nil {
		t.Fatalf("坏 JSON 应报错")
	}
}
