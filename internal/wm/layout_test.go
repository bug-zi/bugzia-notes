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

func TestRectsIntersect(t *testing.T) {
	cases := []struct {
		name string
		a, b Rect
		want bool
	}{
		{"中心相交", Rect{X: 0, Y: 0, W: 10, H: 10}, Rect{X: 5, Y: 5, W: 10, H: 10}, true},
		{"贴边不交", Rect{X: 0, Y: 0, W: 10, H: 10}, Rect{X: 10, Y: 0, W: 10, H: 10}, false},
		{"包含", Rect{X: 0, Y: 0, W: 10, H: 10}, Rect{X: 2, Y: 2, W: 3, H: 3}, true},
		{"完全分离", Rect{X: 0, Y: 0, W: 10, H: 10}, Rect{X: 100, Y: 100, W: 5, H: 5}, false},
		{"负坐标相交", Rect{X: -10, Y: -10, W: 15, H: 15}, Rect{X: 0, Y: 0, W: 10, H: 10}, true},
	}
	for _, c := range cases {
		if got := rectsIntersect(c.a, c.b); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
