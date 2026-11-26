// picture.go 图片粘贴支持（v4.2.0）：picture 目录的保存、引用提取与启动 GC。
// 设计见 docs/project/便签/ 下 v4.2.0 spec（落地后归档）。
package store

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MaxPictureBytes 单张图片字节上限（超限拒绝落盘）。
const MaxPictureBytes = 20 << 20

// pictureNameRe 严格文件名格式：YYYYMMDD-HHMMSS-4位随机小写字母数字.扩展名。
// 宽松白名单（[\w.-]+）会放行「..」使 /picture/.. 指向上级目录，必须严格格式。
var pictureNameRe = regexp.MustCompile(`^\d{8}-\d{6}-[a-z0-9]{4}\.(png|jpg|webp|gif)$`)

// pictureTokenRe 图片引用 token 行（独占一行才识别，提取/GC 共用）。
// 可选 ` =<宽>` 后缀为 Ctrl+滚轮调整的显示宽度（px），名称组恒为第 1 组。
var pictureTokenRe = regexp.MustCompile(`^!\[img\]\(picture/(\d{8}-\d{6}-[a-z0-9]{4}\.(?:png|jpg|webp|gif))(?: =\d{2,4})?\)$`)

// IsPictureName 判断文件名是否符合严格格式（资产路由与打开事件复核共用）。
func IsPictureName(name string) bool { return pictureNameRe.MatchString(name) }

// NormalizeExt 扩展名白名单归一（jpeg→jpg）；非法返回错误。
func NormalizeExt(ext string) (string, error) {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "png":
		return "png", nil
	case "jpg", "jpeg":
		return "jpg", nil
	case "webp":
		return "webp", nil
	case "gif":
		return "gif", nil
	}
	return "", fmt.Errorf("不支持的图片格式: %q", ext)
}

// SavePicture 解码 base64 图片写入 dir（tmp/rename 原子写），返回生成的文件名。
func SavePicture(dir, ext, b64 string) (string, error) {
	e, err := NormalizeExt(ext)
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("图片数据解码失败: %w", err)
	}
	if len(data) == 0 {
		return "", errors.New("图片数据为空")
	}
	if len(data) > MaxPictureBytes {
		return "", fmt.Errorf("图片超过 %dMB 上限", MaxPictureBytes>>20)
	}
	name, err := newPictureName(e)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := atomicWrite(filepath.Join(dir, name), data); err != nil {
		return "", err
	}
	return name, nil
}

// newPictureName 生成「时间戳-4位随机小写字母数字」文件名。
func newPictureName(ext string) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	suffix := make([]byte, 4)
	for i := range suffix {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		suffix[i] = alphabet[n.Int64()]
	}
	return time.Now().Format("20060102-150405") + "-" + string(suffix) + "." + ext, nil
}

// ExtractPictureRefs 提取内容中的图片文件名引用（token 须独占一行才计入）。
func ExtractPictureRefs(contents []string) map[string]bool {
	refs := map[string]bool{}
	for _, c := range contents {
		for _, line := range strings.Split(c, "\n") {
			if m := pictureTokenRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				refs[m[1]] = true
			}
		}
	}
	return refs
}

// ReferencedPictures 汇总便签与回收站引用的图片名（启动 GC 用；
// LoadNotes 幂等可重复调用，GC 在控制器启动恢复之前执行）。
func ReferencedPictures(st *Store) map[string]bool {
	contents := make([]string, 0, 16)
	for _, n := range st.LoadNotes() {
		contents = append(contents, n.Content)
	}
	for _, e := range st.RecycleEntries() {
		contents = append(contents, e.Content)
	}
	return ExtractPictureRefs(contents)
}

// GCPictures 删除 dir 中未被引用的图片文件，返回删除个数。
// 目录不存在安全返回 0；非严格名文件与子目录一律不动。
func GCPictures(dir string, refs map[string]bool) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, en := range entries {
		if en.IsDir() || !IsPictureName(en.Name()) || refs[en.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, en.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
