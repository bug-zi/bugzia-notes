package updater

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadOK(t *testing.T) {
	content := make([]byte, 1000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write(content)
	}))
	defer ts.Close()
	cfg := &Config{AssetName: "bugzia-notes.exe", Client: ts.Client()}
	rel := &Release{AssetURL: ts.URL, AssetSize: 1000}

	dest, err := Download(cfg, rel, t.TempDir())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || len(data) != 1000 {
		t.Fatalf("成品内容错误: len=%d err=%v", len(data), err)
	}
	if filepath.Base(dest) != "bugzia-notes.update.exe" {
		t.Fatalf("成品名错误: %s", dest)
	}
}

func TestDownloadSizeMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "500")
		_, _ = w.Write(make([]byte, 500))
	}))
	defer ts.Close()
	cfg := &Config{AssetName: "bugzia-notes.exe", Client: ts.Client()}
	dir := t.TempDir()
	if _, err := Download(cfg, &Release{AssetURL: ts.URL, AssetSize: 1000}, dir); err == nil {
		t.Fatalf("尺寸不符应报错")
	}
	// 失败不得留下成品或 .part
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("失败应无残留: %d 个文件", len(entries))
	}
}

func TestDownloadUnknownSize(t *testing.T) {
	// atom 直链路径无声明尺寸：以响应 Content-Length 为准
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write(make([]byte, 1000))
	}))
	defer ts.Close()
	cfg := &Config{AssetName: "bugzia-notes.exe", Client: ts.Client()}
	dest, err := Download(cfg, &Release{AssetURL: ts.URL, AssetSize: 0}, t.TempDir())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || len(data) != 1000 {
		t.Fatalf("成品内容错误: len=%d err=%v", len(data), err)
	}
}

func TestDownloadTruncated(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 声明 1000 只发 10（无 Content-Length，客户端按实收校验）
		_, _ = w.Write(make([]byte, 10))
	}))
	defer ts.Close()
	cfg := &Config{AssetName: "bugzia-notes.exe", Client: ts.Client()}
	if _, err := Download(cfg, &Release{AssetURL: ts.URL, AssetSize: 1000}, t.TempDir()); err == nil {
		t.Fatalf("传输截断应报错")
	}
}

func TestCleanupStale(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"bugzia-notes.update.exe", "bugzia-notes.update.exe.part", "bugzia-notes.exe.update.bat",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	CleanupStale(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("残留未清干净: %d 个", len(entries))
	}
}
