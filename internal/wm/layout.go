package wm

// Rect 逻辑像素矩形（定位算法用）。
type Rect struct{ X, Y, W, H float32 }

// PlaceMargin 便签默认落点距屏幕边缘的内缩（与 bugzia 一致）。
const PlaceMargin = 8

// PackTopLeft 贪心左上装箱（bugzia packTopLeft 的逐行移植）：
// 从屏幕左上角开始逐行扫描（行高 = 新便签高）；行内右移紧贴已有便签
//（无间隙）；行满换下一行；全程不出屏；全满时夹回左上锚点。
// 返回新便签左上角的逻辑像素坐标。
func PackTopLeft(rects []Rect, w, h, screenW, screenH float32) (float32, float32) {
	const m = PlaceMargin
	left, top := float32(m), float32(m)
	right, bottom := screenW-float32(m), screenH-float32(m)
	for rowTop := top; rowTop+h <= bottom; rowTop += h {
		bandBottom := rowTop + h
		var inBand []Rect
		for _, r := range rects {
			if r.Y < bandBottom && r.Y+r.H > rowTop {
				inBand = append(inBand, r)
			}
		}
		x := left
		for {
			hit := false
			for _, r := range inBand {
				if r.X < x+w && r.X+r.W > x {
					x = r.X + r.W // 紧贴右缘，无间隙
					hit = true
					break
				}
			}
			if !hit {
				break
			}
		}
		if x+w <= right {
			return x, rowTop
		}
	}
	return left, top
}
