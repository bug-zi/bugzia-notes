package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchSilent(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"手动启动", []string{}, false},
		{"自启动", []string{"--silent"}, true},
		{"混合参数", []string{"--other", "--silent"}, true},
		{"相似参数不误判", []string{"--silently"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := launchSilent(c.args); got != c.want {
				t.Fatalf("launchSilent(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestUnderGoBuildTmp(t *testing.T) {
	tmp := t.TempDir() // 同为系统临时目录风格路径，Rel 语义一致
	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{"临时目录 go-build 内", filepath.Join(tmp, "go-build123", "exe"), true},
		{"临时目录 go-build 更深层", filepath.Join(tmp, "go-build123", "exe", "b001"), true},
		{"临时目录非 go-build", filepath.Join(tmp, "其他工具"), false},
		{"临时目录本身", tmp, false},
		{"普通项目目录", filepath.Join(tmp, "project", "bugzia-notes"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := underGoBuildTmp(c.dir); got != c.want {
				t.Fatalf("underGoBuildTmp(%q) = %v, want %v", c.dir, got, c.want)
			}
		})
	}
}

func TestMigrateLegacyDataDir(t *testing.T) {
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("旧数据全量迁移并清理旧目录", func(t *testing.T) {
		root := t.TempDir()
		legacy := filepath.Join(root, "legacy")
		newDir := filepath.Join(root, "new", "data")
		write(filepath.Join(legacy, "notes.json"), `{"notes":[]}`)
		write(filepath.Join(legacy, "settings.json"), `{"version":1}`)

		migrateLegacyData(newDir, legacy)

		for _, name := range []string{"notes.json", "settings.json"} {
			data, err := os.ReadFile(filepath.Join(newDir, name))
			if err != nil {
				t.Fatalf("新位置缺 %s: %v", name, err)
			}
			if len(data) == 0 {
				t.Fatalf("新位置 %s 内容为空", name)
			}
		}
		if _, err := os.Stat(legacy); !os.IsNotExist(err) {
			t.Fatalf("旧目录应已删除, err=%v", err)
		}
	})

	t.Run("新位置已有文件以新为准", func(t *testing.T) {
		root := t.TempDir()
		legacy := filepath.Join(root, "legacy")
		newDir := filepath.Join(root, "new", "data")
		write(filepath.Join(legacy, "notes.json"), "old")
		write(filepath.Join(newDir, "notes.json"), "new")

		migrateLegacyData(newDir, legacy)

		data, err := os.ReadFile(filepath.Join(newDir, "notes.json"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "new" {
			t.Fatalf("新位置文件被覆盖: %q", string(data))
		}
		if _, err := os.Stat(legacy); !os.IsNotExist(err) {
			t.Fatalf("旧副本应已清理, err=%v", err)
		}
	})

	t.Run("旧目录不存在则无事发生", func(t *testing.T) {
		root := t.TempDir()
		newDir := filepath.Join(root, "new", "data")

		migrateLegacyData(newDir, filepath.Join(root, "not-exist"))

		if _, err := os.Stat(newDir); !os.IsNotExist(err) {
			t.Fatalf("不应创建新数据目录, err=%v", err)
		}
	})

	t.Run("旧目录残留杂物时目录保留但数据迁走", func(t *testing.T) {
		root := t.TempDir()
		legacy := filepath.Join(root, "legacy")
		newDir := filepath.Join(root, "new", "data")
		write(filepath.Join(legacy, "notes.json"), `{"notes":[]}`)
		write(filepath.Join(legacy, "notes.json.tmp"), "leftover")

		migrateLegacyData(newDir, legacy)

		if _, err := os.Stat(filepath.Join(newDir, "notes.json")); err != nil {
			t.Fatalf("数据应已迁移: %v", err)
		}
		if _, err := os.Stat(filepath.Join(legacy, "notes.json.tmp")); err != nil {
			t.Fatalf("旧目录杂物应保留不动: %v", err)
		}
	})
}
