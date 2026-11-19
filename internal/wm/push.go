package wm

// pushGap 弹开落点与遮挡者之间保留的间隙（与装箱边距同源）。
const pushGap = PlaceMargin

// 弹开动画节奏（GlideNote 参数，wails 侧按 ms 步进）。
const (
	GlideMs        = 250
	GlideStaggerMs = 40
)

// PushNote 一张候选被弹便签的输入（实时几何：px/py 优先 + 缩放后 ClampW/H）。
type PushNote struct {
	ID   string
	Rect Rect
}

// PushMode 被弹者解析方式。
type PushMode int

const (
	PushSlide PushMode = iota // 方向弹开（滑到目标位）
	PushPack                  // 四向不可行，装箱兜底找空位
)

// Push 一张被弹便签的解析结果（仅含实际移动者；接受重叠者不在结果中）。
type Push struct {
	ID   string
	X, Y int
	Mode PushMode
}

// ResolveOverlaps 召唤弹开核心（纯函数）。self 为已落地召唤便签矩形；notes 为
// 其余全部可见便签（实时几何，皆可推）。与「已落地矩形集」相交的便签被弹开：
// 右/下/左/上迭代越过已落地遮挡者（迭代有界），取可行者中位移最小者（平手偏向
// 右/下——候选按此顺序生成、严格小于才替换），落点留 pushGap 间隙且完整在屏内
//（PlaceMargin 内缩）；四向均不可行 → 装箱对全部其他便签避让+复核；仍无空位 →
// 接受重叠（原地不动、不出现在结果）。被弹者落点撞上未处理便签时，该便签在其
// 后的扫描轮被弹开（级联）；每张至多解析一次，循环有界。已落地矩形两两无交
//（归纳：每次落点都对当时全部已落地者复核），推进迭代必然终止。
func ResolveOverlaps(self Rect, notes []PushNote, screenW, screenH float32) []Push {
	landed := []Rect{self}
	byID := make(map[string]Rect, len(notes))
	for _, n := range notes {
		byID[n.ID] = n.Rect
	}
	processed := map[string]bool{}
	out := make([]Push, 0, len(notes))

	for {
		progressed := false
		for _, n := range notes {
			if processed[n.ID] {
				continue
			}
			cur := byID[n.ID]
			if !overlapsAny(cur, landed) {
				continue
			}
			processed[n.ID] = true
			bestDist := float32(-1)
			var bestX, bestY float32
			for _, d := range slideDisps(cur, landed, screenW, screenH) {
				dist := absF(d.dx) + absF(d.dy)
				if bestDist < 0 || dist < bestDist {
					bestDist, bestX, bestY = dist, d.dx, d.dy
				}
			}
			if bestDist >= 0 {
				nx, ny := int(cur.X+bestX), int(cur.Y+bestY)
				nr := Rect{X: float32(nx), Y: float32(ny), W: cur.W, H: cur.H}
				landed = append(landed, nr)
				byID[n.ID] = nr
				out = append(out, Push{ID: n.ID, X: nx, Y: ny, Mode: PushSlide})
				progressed = true
				continue
			}
			// 装箱兜底：对全部其他便签（含 self）当前矩形避让 + 复核
			rects := make([]Rect, 0, len(notes)+1)
			rects = append(rects, self)
			for _, o := range notes {
				if o.ID != n.ID {
					rects = append(rects, byID[o.ID])
				}
			}
			pnx, pny := PackTopLeft(rects, cur.W, cur.H, screenW, screenH)
			nr := Rect{X: pnx, Y: pny, W: cur.W, H: cur.H}
			if !overlapsAny(nr, rects) {
				landed = append(landed, nr)
				byID[n.ID] = nr
				out = append(out, Push{ID: n.ID, X: int(pnx), Y: int(pny), Mode: PushPack})
				progressed = true
			}
			// 否则接受重叠：原地不动
		}
		if !progressed {
			break
		}
	}
	return out
}

type disp struct{ dx, dy float32 }

// slideDisps 四方向候选（右/下/左/上顺序，供平手偏向右/下），只返回可行者。
func slideDisps(r Rect, landed []Rect, screenW, screenH float32) []disp {
	const m = PlaceMargin
	var out []disp
	if dx, ok := advanceX(r, landed, screenW-m, true); ok {
		out = append(out, disp{dx: dx})
	}
	if dy, ok := advanceY(r, landed, screenH-m, true); ok {
		out = append(out, disp{dy: dy})
	}
	if dx, ok := advanceX(r, landed, m, false); ok {
		out = append(out, disp{dx: dx})
	}
	if dy, ok := advanceY(r, landed, m, false); ok {
		out = append(out, disp{dy: dy})
	}
	return out
}

// advanceX 水平迭代推进：不断越过相交的已落地矩形直到候选无交；出界不可行。
// toRight=true 向右（limit=右界），否则向左（limit=左界）。返回位移量。
func advanceX(r Rect, landed []Rect, limit float32, toRight bool) (float32, bool) {
	x := r.X
	for i := 0; i <= len(landed); i++ {
		cand := Rect{X: x, Y: r.Y, W: r.W, H: r.H}
		hit := false
		for _, l := range landed {
			if rectsIntersect(cand, l) {
				if toRight {
					x = l.X + l.W + pushGap
				} else {
					x = l.X - pushGap - r.W
				}
				hit = true
				break
			}
		}
		if !hit {
			if toRight {
				if x+r.W <= limit {
					return x - r.X, true
				}
			} else if x >= limit {
				return x - r.X, true
			}
			return 0, false
		}
	}
	return 0, false
}

// advanceY 垂直迭代推进（同 advanceX 语义）。
func advanceY(r Rect, landed []Rect, limit float32, toDown bool) (float32, bool) {
	y := r.Y
	for i := 0; i <= len(landed); i++ {
		cand := Rect{X: r.X, Y: y, W: r.W, H: r.H}
		hit := false
		for _, l := range landed {
			if rectsIntersect(cand, l) {
				if toDown {
					y = l.Y + l.H + pushGap
				} else {
					y = l.Y - pushGap - r.H
				}
				hit = true
				break
			}
		}
		if !hit {
			if toDown {
				if y+r.H <= limit {
					return y - r.Y, true
				}
			} else if y >= limit {
				return y - r.Y, true
			}
			return 0, false
		}
	}
	return 0, false
}

func overlapsAny(r Rect, rects []Rect) bool {
	for _, o := range rects {
		if rectsIntersect(r, o) {
			return true
		}
	}
	return false
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
