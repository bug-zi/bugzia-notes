package store

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeExt(t *testing.T) {
	cases := []struct{ in, want string }{
		{"png", "png"}, {"jpg", "jpg"}, {"jpeg", "jpg"},
		{"webp", "webp"}, {"gif", "gif"},
	}
	for _, c := range cases {
		got, err := NormalizeExt(c.in)
		if err != nil || got != c.want {
			t.Fatalf("NormalizeExt(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "exe", "bmp", "png.exe"} {
		if _, err := NormalizeExt(bad); err == nil {
			t.Fatalf("NormalizeExt(%q) 应报错", bad)
		}
	}
}

func TestIsPictureName(t *testing.T) {
	ok := []string{"20261126-223000-a1b2.png", "20261126-223001-z9z9.jpg", "20261126-223002-abcd.webp", "20261126-223003-eeee.gif"}
	bad := []string{"..", ".", "a.png", "20261126-223000-a1b2.exe", "20261126-22300-abcd.png",
		"20261126_223000-abcd.png", "20261126-223000-ABCD.png", "x/../../etc.png", ""}
	for _, n := range ok {
		if !IsPictureName(n) {
			t.Fatalf("IsPictureName(%q) 应为 true", n)
		}
	}
	for _, n := range bad {
		if IsPictureName(n) {
			t.Fatalf("IsPictureName(%q) 应为 false（防穿越：.. 必须拒）", n)
		}
	}
}

func TestSavePictureRoundTrip(t *testing.T) {
	dir := t.TempDir()
	data := []byte("fake-png-bytes-0123456789")
	name, err := SavePicture(dir, "png", base64.StdEncoding.EncodeToString(data))
	if err != nil {
		t.Fatalf("SavePicture: %v", err)
	}
	if !IsPictureName(name) {
		t.Fatalf("文件名不符严格格式: %q", name)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("内容不一致: got %q want %q", got, data)
	}
	if strings.Contains(name, ".tmp") {
		t.Fatalf("不应残留 tmp 文件名: %q", name)
	}
}

func TestSavePictureRejects(t *testing.T) {
	dir := t.TempDir()
	if _, err := SavePicture(dir, "exe", "AAAA"); err == nil {
		t.Fatal("非法扩展名应报错")
	}
	if _, err := SavePicture(dir, "png", "!!!不是base64!!!"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	if _, err := SavePicture(dir, "png", ""); err == nil {
		t.Fatal("空数据应报错")
	}
	big := make([]byte, MaxPictureBytes+1)
	if _, err := SavePicture(dir, "png", base64.StdEncoding.EncodeToString(big)); err == nil {
		t.Fatal("超 20MB 应报错")
	}
}

func TestExtractPictureRefs(t *testing.T) {
	content := "before\n" +
		"![img](picture/20261126-223000-a1b2.png)\n" +
		"text ![img](picture/20261126-223001-cccc.png) inline\n" +
		"![img](picture/not-strict-name.png)\n" +
		"![img](picture/20261126-223002-dddd.gif)\n" +
		"![img](picture/20261126-223003-eeee.png =320)\n" +
		"![img](picture/20261126-223004-ffff.jpg =99999)"
	refs := ExtractPictureRefs([]string{content, "plain", ""})
	if len(refs) != 3 {
		t.Fatalf("应恰好 3 个引用: %v", refs)
	}
	if !refs["20261126-223000-a1b2.png"] || !refs["20261126-223002-dddd.gif"] || !refs["20261126-223003-eeee.png"] {
		t.Fatalf("独占一行 token（含宽度后缀）应被提取: %v", refs)
	}
	if refs["20261126-223001-cccc.png"] || refs["not-strict-name.png"] || refs["20261126-223004-ffff.jpg"] {
		t.Fatalf("行内混排/非严格名/超宽后缀不应被提取: %v", refs)
	}
}

func TestGCPictures(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("20261126-223000-a1b2.png") // 引用中，保留
	mk("20261126-223001-cccc.png") // 无引用，删除
	mk("notes.json.tmp")           // 非严格名，不动
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	refs := map[string]bool{"20261126-223000-a1b2.png": true}
	removed, err := GCPictures(dir, refs)
	if err != nil {
		t.Fatalf("GCPictures: %v", err)
	}
	if removed != 1 {
		t.Fatalf("应删 1 个, 实删 %d", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "20261126-223000-a1b2.png")); err != nil {
		t.Fatal("被引用文件不应被删")
	}
	if _, err := os.Stat(filepath.Join(dir, "20261126-223001-cccc.png")); !os.IsNotExist(err) {
		t.Fatal("无引用文件应被删")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.json.tmp")); err != nil {
		t.Fatal("非图片名文件不应被删")
	}
}

func TestGCPicturesMissingDir(t *testing.T) {
	removed, err := GCPictures(filepath.Join(t.TempDir(), "nope"), map[string]bool{})
	if err != nil || removed != 0 {
		t.Fatalf("目录不存在应 (0, nil), got (%d, %v)", removed, err)
	}
}
