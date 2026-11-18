package wm

import "testing"

func TestPackTopLeftEmptyScreen(t *testing.T) {
	x, y := PackTopLeft(nil, 240, 220, 1920, 1080)
	if x != PlaceMargin || y != PlaceMargin {
		t.Fatalf("空屏应落在左上锚点: (%v,%v)", x, y)
	}
}

func TestPackTopLeftFlushAgainstExisting(t *testing.T) {
	rects := []Rect{{X: 8, Y: 8, W: 240, H: 220}}
	x, y := PackTopLeft(rects, 240, 220, 1920, 1080)
	if y != 8 {
		t.Fatalf("应在同一行: y=%v", y)
	}
	if x != 248 {
		t.Fatalf("应紧贴已有便签右缘（无间隙）: x=%v", x)
	}
}

func TestPackTopLeftChainPush(t *testing.T) {
	rects := []Rect{{X: 8, Y: 8, W: 492, H: 220}}
	x, _ := PackTopLeft(rects, 240, 220, 1920, 1080)
	if x != 500 {
		t.Fatalf("应从重叠处右移到紧贴位置: x=%v", x)
	}
}

func TestPackTopLeftWrapsToNextRow(t *testing.T) {
	rects := []Rect{
		{X: 8, Y: 8, W: 240, H: 220},
		{X: 248, Y: 8, W: 240, H: 220},
		{X: 488, Y: 8, W: 240, H: 220},
	}
	// 屏宽 800 → 右界 792；488+240=728 已是行尾，第四张放不下 → 换行
	x, y := PackTopLeft(rects, 240, 220, 800, 1080)
	if x != 8 || y != 228 {
		t.Fatalf("行满应换行到左端: (%v,%v)", x, y)
	}
}

func TestPackTopLeftBandFilterIgnoresOtherRows(t *testing.T) {
	rects := []Rect{{X: 8, Y: 300, W: 240, H: 220}} // 只占第二行带
	x, y := PackTopLeft(rects, 240, 220, 1920, 1080)
	if x != 8 || y != 8 {
		t.Fatalf("不与首行带相交的便签不应影响首行落点: (%v,%v)", x, y)
	}
}

func TestPackTopLeftClampsWhenFull(t *testing.T) {
	rects := []Rect{{X: 8, Y: 8, W: 2000, H: 100}}
	x, y := PackTopLeft(rects, 240, 220, 2048, 400)
	if x != 8 || y != 8 {
		t.Fatalf("全满应夹回锚点而不是漂出屏幕: (%v,%v)", x, y)
	}
}
