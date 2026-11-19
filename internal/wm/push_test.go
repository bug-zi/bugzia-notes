package wm

import "testing"

// 平手偏右：右/下位移同为 58 → 取右。
func TestResolveTiePrefersRight(t *testing.T) {
	self := Rect{X: 8, Y: 8, W: 100, H: 100}
	notes := []PushNote{{ID: "b", Rect: Rect{X: 58, Y: 58, W: 100, H: 100}}}
	got, _ := ResolveOverlaps(self, notes, 800, 600)
	if len(got) != 1 || got[0].ID != "b" || got[0].Mode != PushSlide || got[0].X != 116 || got[0].Y != 58 {
		t.Fatalf("平手应偏向右弹 (116,58): %+v", got)
	}
}

// 最小位移方向：下比右小 → 取下。
func TestResolvePicksSmallestDisplacement(t *testing.T) {
	self := Rect{X: 8, Y: 8, W: 100, H: 100}
	notes := []PushNote{{ID: "b", Rect: Rect{X: 58, Y: 90, W: 100, H: 100}}}
	got, _ := ResolveOverlaps(self, notes, 800, 600)
	if len(got) != 1 || got[0].X != 58 || got[0].Y != 116 {
		t.Fatalf("应取最小位移向下弹 (58,116): %+v", got)
	}
}

// 级联：B 下弹的落点（advance 只避已落地者）压到未处理的 C → C 随后被弹开。
// self(8,8,300,200) B(150,100,100,100) C(158,308,100,100)，屏 800x600：
// B 右 dx=166、下 dy=116 → 下 → B(150,216)；C 与 B 新矩形相交 → C 下弹 (158,324)。
func TestResolveCascade(t *testing.T) {
	self := Rect{X: 8, Y: 8, W: 300, H: 200}
	notes := []PushNote{
		{ID: "b", Rect: Rect{X: 150, Y: 100, W: 100, H: 100}},
		{ID: "c", Rect: Rect{X: 158, Y: 308, W: 100, H: 100}},
	}
	got, _ := ResolveOverlaps(self, notes, 800, 600)
	if len(got) != 2 ||
		got[0].ID != "b" || got[0].X != 150 || got[0].Y != 216 || got[0].Mode != PushSlide ||
		got[1].ID != "c" || got[1].X != 158 || got[1].Y != 324 || got[1].Mode != PushSlide {
		t.Fatalf("级联应弹 B(150,216) 再弹 C(158,324): %+v", got)
	}
}

// 级联传导 + 平手偏右：B 右/下位移同为 158 → 偏右；B 落点压到 r → r 被弹开；
// r 的落点不碰 t/d，但 t/d 与 self 相交被独立解析（t 上滑 28、d 下滑 28 取最小）。
// 五便签终态两两无交、全部滑动解决，无需装箱。
func TestResolveCascadeChain(t *testing.T) {
	self := Rect{X: 300, Y: 200, W: 200, H: 200}
	notes := []PushNote{
		{ID: "b", Rect: Rect{X: 350, Y: 250, W: 100, H: 100}},
		{ID: "r", Rect: Rect{X: 500, Y: 250, W: 290, H: 100}},
		{ID: "t", Rect: Rect{X: 350, Y: 100, W: 100, H: 120}},
		{ID: "d", Rect: Rect{X: 350, Y: 380, W: 100, H: 150}},
	}
	got, _ := ResolveOverlaps(self, notes, 800, 600)
	want := []Push{
		{ID: "b", X: 508, Y: 250, Mode: PushSlide},
		{ID: "r", X: 500, Y: 358, Mode: PushSlide},
		{ID: "t", X: 350, Y: 72, Mode: PushSlide},
		{ID: "d", X: 350, Y: 408, Mode: PushSlide},
	}
	if len(got) != len(want) {
		t.Fatalf("应弹开全部四张: got %+v want %+v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("第 %d 张应为 %+v: %+v", i, w, got[i])
		}
	}
}

// 四向均不可行（self 横贯全宽、屏高不足）→ 装箱兜底找到空位 (8,308)。
func TestResolvePackFallback(t *testing.T) {
	self := Rect{X: 8, Y: 8, W: 784, H: 300}
	notes := []PushNote{{ID: "b", Rect: Rect{X: 300, Y: 200, W: 100, H: 100}}}
	got, _ := ResolveOverlaps(self, notes, 800, 420)
	if len(got) != 1 || got[0].ID != "b" || got[0].Mode != PushPack || got[0].X != 8 || got[0].Y != 308 {
		t.Fatalf("四向不可行应装箱到 (8,308): %+v", got)
	}
}

// 屏幕满：装箱回退点也重叠 → 接受重叠（结果为空，被弹者原地不动）。
func TestResolveAcceptOverlapWhenFull(t *testing.T) {
	self := Rect{X: 8, Y: 8, W: 784, H: 584}
	notes := []PushNote{{ID: "b", Rect: Rect{X: 100, Y: 100, W: 100, H: 100}}}
	got, _ := ResolveOverlaps(self, notes, 800, 600)
	if len(got) != 0 {
		t.Fatalf("屏幕满应接受重叠（无移动）: %+v", got)
	}
}

// 无重叠不弹。
func TestResolveNoOverlapNoop(t *testing.T) {
	self := Rect{X: 8, Y: 8, W: 100, H: 100}
	notes := []PushNote{{ID: "b", Rect: Rect{X: 300, Y: 300, W: 100, H: 100}}}
	if got, _ := ResolveOverlaps(self, notes, 800, 600); len(got) != 0 {
		t.Fatalf("无重叠不应有移动: %+v", got)
	}
}

// 屏幕满：接受重叠 → packed=true（桌面塞不下的见证，触发拖拽手势熔断）。
func TestResolveReportsPackedOnAcceptOverlap(t *testing.T) {
	self := Rect{X: 8, Y: 8, W: 784, H: 584}
	notes := []PushNote{{ID: "b", Rect: Rect{X: 100, Y: 100, W: 100, H: 100}}}
	got, packed := ResolveOverlaps(self, notes, 800, 600)
	if len(got) != 0 || !packed {
		t.Fatalf("接受重叠应报告满屏: got %+v packed=%v", got, packed)
	}
}

// 正常弹开 → packed=false。
func TestResolveNotPackedOnSuccess(t *testing.T) {
	self := Rect{X: 8, Y: 8, W: 100, H: 100}
	notes := []PushNote{{ID: "b", Rect: Rect{X: 58, Y: 58, W: 100, H: 100}}}
	got, packed := ResolveOverlaps(self, notes, 1920, 1080)
	if len(got) != 1 || packed {
		t.Fatalf("成功弹开不应报告满屏: got %+v packed=%v", got, packed)
	}
}
