package canvas

import (
	"math"
)

// BeginPath clears the current path and starts a new one
func (cv *Canvas) BeginPath() {
	if cv.path.p == nil {
		cv.path.p = make([]pathPoint, 0, 100)
	}
	cv.path.p = cv.path.p[:0]
}

func isSamePoint(a, b vec, maxDist float64) bool {
	return math.Abs(b[0]-a[0]) <= maxDist && math.Abs(b[1]-a[1]) <= maxDist
}

// MoveTo adds a gap and moves the end of the path to x/y
func (cv *Canvas) MoveTo(x, y float64) {
	tf := cv.tf(vec{x, y})
	cv.path.MoveTo(tf[0], tf[1])
}

// LineTo adds a line to the end of the path
func (cv *Canvas) LineTo(x, y float64) {
	tf := cv.tf(vec{x, y})
	cv.path.LineTo(tf[0], tf[1])
}

// Arc adds a circle segment to the end of the path. x/y is the center, radius
// is the radius, startAngle and endAngle are angles in radians, anticlockwise
// means that the line is added anticlockwise
func (cv *Canvas) Arc(x, y, radius, startAngle, endAngle float64, anticlockwise bool) {
	tf := cv.tf(vec{x, y})
	ax, ay := math.Sincos(startAngle)
	startAngle2 := vec{ay, ax}.mulMat2(cv.state.transform.mat2()).atan2()
	endAngle2 := startAngle2 + (endAngle - startAngle)
	cv.path.Arc(tf[0], tf[1], radius, startAngle2, endAngle2, anticlockwise)
}

// ArcTo adds to the current path by drawing a line toward x1/y1 and a circle
// segment of a radius given by the radius parameter. The circle touches the
// lines from the end of the path to x1/y1, and from x1/y1 to x2/y2. The line
// will only go to where the circle segment would touch the latter line
func (cv *Canvas) ArcTo(x1, y1, x2, y2, radius float64) {
	tf1 := cv.tf(vec{x1, y1})
	tf2 := cv.tf(vec{x2, y2})
	cv.path.ArcTo(tf1[0], tf1[1], tf2[0], tf2[1], radius)
}

// QuadraticCurveTo adds a quadratic curve to the path. It uses the current end
// point of the path, x1/y1 defines the curve, and x2/y2 is the end point
func (cv *Canvas) QuadraticCurveTo(x1, y1, x2, y2 float64) {
	tf1 := cv.tf(vec{x1, y1})
	tf2 := cv.tf(vec{x2, y2})
	cv.path.QuadraticCurveTo(tf1[0], tf1[1], tf2[0], tf2[1])
}

// BezierCurveTo adds a bezier curve to the path. It uses the current end point
// of the path, x1/y1 and x2/y2 define the curve, and x3/y3 is the end point
func (cv *Canvas) BezierCurveTo(x1, y1, x2, y2, x3, y3 float64) {
	tf1 := cv.tf(vec{x1, y1})
	tf2 := cv.tf(vec{x2, y2})
	tf3 := cv.tf(vec{x3, y3})
	cv.path.BezierCurveTo(tf1[0], tf1[1], tf2[0], tf2[1], tf3[0], tf3[1])
}

// ClosePath closes the path to the beginning of the path or the last point
// from a MoveTo call
func (cv *Canvas) ClosePath() {
	cv.path.ClosePath()
}

// Stroke uses the current StrokeStyle to draw the current path
func (cv *Canvas) Stroke() {
	cv.strokePath(&cv.path, cv.state.transform.invert(), true)
}

// StrokePath uses the current StrokeStyle to draw the given path
func (cv *Canvas) StrokePath(path *Path2D) {
	// todo avoid allocation
	path2 := Path2D{
		p: make([]pathPoint, len(path.p)),
	}
	copy(path2.p, path.p)
	cv.strokePath(&path2, mat{}, false)
}

func (cv *Canvas) strokePath(path *Path2D, inv mat, doInv bool) {
	if len(path.p) == 0 {
		return
	}

	var triBuf [500][2]float64
	tris := cv.strokeTris(path, inv, doInv, triBuf[:0])

	cv.drawShadow2(tris, nil)

	stl := cv.backendFillStyle(&cv.state.stroke, 1)
	cv.b.Fill(&stl, tris)
}

func (cv *Canvas) strokeTris(path *Path2D, inv mat, doInv bool, target [][2]float64) [][2]float64 {
	if len(path.p) == 0 {
		return target
	}

	if doInv {
		for i, pt := range path.p {
			path.p[i].pos = pt.pos.mulMat(inv)
			path.p[i].next = pt.next.mulMat(inv)
		}
	}

	dashedPath := cv.applyLineDash(path.p)

	start := true
	var p0 vec
	for _, p := range dashedPath {
		if p.flags&pathMove != 0 {
			p0 = p.pos
			start = true
			continue
		}
		p1 := p.pos

		v0 := p1.sub(p0).norm()
		v1 := vec{v0[1], -v0[0]}.mulf(cv.state.lineWidth * 0.5)
		v0 = v0.mulf(cv.state.lineWidth * 0.5)

		lp0 := p0.add(v1)
		lp1 := p1.add(v1)
		lp2 := p0.sub(v1)
		lp3 := p1.sub(v1)

		if start {
			switch cv.state.lineCap {
			case Butt:
				// no need to do anything
			case Square:
				lp0 = lp0.sub(v0)
				lp2 = lp2.sub(v0)
			case Round:
				target = cv.addCircleTris(p0, cv.state.lineWidth*0.5, target)
			}
		}

		if p.flags&pathAttach == 0 {
			switch cv.state.lineCap {
			case Butt:
				// no need to do anything
			case Square:
				lp1 = lp1.add(v0)
				lp3 = lp3.add(v0)
			case Round:
				target = cv.addCircleTris(p1, cv.state.lineWidth*0.5, target)
			}
		}

		target = append(target, cv.tf(lp0), cv.tf(lp1), cv.tf(lp3), cv.tf(lp0), cv.tf(lp3), cv.tf(lp2))

		if p.flags&pathAttach != 0 && cv.state.lineWidth > 1 {
			target = cv.lineJoint(p0, p1, p.next, lp0, lp1, lp2, lp3, target)
		}

		p0 = p1
		start = false
	}

	return target
}

func (cv *Canvas) applyLineDash(path []pathPoint) []pathPoint {
	if len(cv.state.lineDash) < 2 || len(path) < 2 {
		return path
	}

	ldo := cv.state.lineDashOffset
	ldp := cv.state.lineDashPoint

	path2 := make([]pathPoint, 0, len(path)*2)

	var lp pathPoint
	for i, pp := range path {
		if i == 0 || pp.flags&pathMove != 0 {
			path2 = append(path2, pp)
			lp = pp
			continue
		}

		v := pp.pos.sub(lp.pos)
		vl := v.len()
		prev := ldo
		for vl > 0 {
			draw := ldp%2 == 0
			newp := pathPoint{pos: pp.pos}
			ldo += vl
			if ldo > cv.state.lineDash[ldp] {
				ldo = 0
				dl := cv.state.lineDash[ldp] - prev
				dist := dl / vl
				newp.pos = lp.pos.add(v.mulf(dist))
				vl -= dl
				ldp++
				ldp %= len(cv.state.lineDash)
				prev = 0
			} else {
				vl = 0
			}

			if draw {
				path2[len(path2)-1].next = newp.pos
				path2[len(path2)-1].flags |= pathAttach
				path2 = append(path2, newp)
			} else {
				newp.flags = pathMove
				path2 = append(path2, newp)
			}

			lp = newp
			v = pp.pos.sub(lp.pos)
		}
		lp = pp
	}

	return path2
}

func (cv *Canvas) lineJoint(p0, p1, p2, l0p0, l0p1, l0p2, l0p3 vec, tris [][2]float64) [][2]float64 {
	v2 := p1.sub(p2).norm()
	v3 := vec{v2[1], -v2[0]}.mulf(cv.state.lineWidth * 0.5)

	switch cv.state.lineJoin {
	case Miter:
		l1p0 := p2.sub(v3)
		l1p1 := p1.sub(v3)
		l1p2 := p2.add(v3)
		l1p3 := p1.add(v3)

		var ip0, ip1 vec
		if l0p1.sub(l1p1).lenSqr() < 0.000000001 {
			ip0 = l0p1.sub(l1p1).mulf(0.5).add(l1p1)
		} else {
			var q float64
			ip0, _, q = lineIntersection(l0p0, l0p1, l1p1, l1p0)
			if q >= 1 {
				ip0 = l0p1.add(l1p1).mulf(0.5)
			}
		}

		if dist := ip0.sub(l0p1).lenSqr(); dist > cv.state.miterLimitSqr {
			l1p1 := p1.sub(v3)
			l1p3 := p1.add(v3)

			tris = append(tris, cv.tf(p1), cv.tf(l0p1), cv.tf(l1p1),
				cv.tf(p1), cv.tf(l1p3), cv.tf(l0p3))
			return tris
		}

		if l0p3.sub(l1p3).lenSqr() < 0.000000001 {
			ip1 = l0p3.sub(l1p3).mulf(0.5).add(l1p3)
		} else {
			var q float64
			ip1, _, q = lineIntersection(l0p2, l0p3, l1p3, l1p2)
			if q >= 1 {
				ip1 = l0p3.add(l1p3).mulf(0.5)
			}
		}

		if dist := ip1.sub(l1p1).lenSqr(); dist > cv.state.miterLimitSqr {
			l1p1 := p1.sub(v3)
			l1p3 := p1.add(v3)

			tris = append(tris, cv.tf(p1), cv.tf(l0p1), cv.tf(l1p1),
				cv.tf(p1), cv.tf(l1p3), cv.tf(l0p3))
			return tris
		}

		tris = append(tris, cv.tf(p1), cv.tf(l0p1), cv.tf(ip0),
			cv.tf(p1), cv.tf(ip0), cv.tf(l1p1),
			cv.tf(p1), cv.tf(l1p3), cv.tf(ip1),
			cv.tf(p1), cv.tf(ip1), cv.tf(l0p3))
	case Bevel:
		l1p1 := p1.sub(v3)
		l1p3 := p1.add(v3)

		tris = append(tris, cv.tf(p1), cv.tf(l0p1), cv.tf(l1p1),
			cv.tf(p1), cv.tf(l1p3), cv.tf(l0p3))
	case Round:
		tris = cv.addCircleTris(p1, cv.state.lineWidth*0.5, tris)
	}

	return tris
}

func (cv *Canvas) addCircleTris(center vec, radius float64, tris [][2]float64) [][2]float64 {
	step := 6 / radius
	if step > 0.8 {
		step = 0.8
	} else if step < 0.05 {
		step = 0.05
	}
	centertf := cv.tf(center)
	p0 := cv.tf(vec{center[0], center[1] + radius})
	for angle := step; angle <= math.Pi*2+step; angle += step {
		s, c := math.Sincos(angle)
		p1 := cv.tf(vec{center[0] + s*radius, center[1] + c*radius})
		tris = append(tris, centertf, p0, p1)
		p0 = p1
	}
	return tris
}

func lineIntersection(a0, a1, b0, b1 vec) (vec, float64, float64) {
	va := a1.sub(a0)
	vb := b1.sub(b0)

	if (va[0] == 0 && vb[0] == 0) || (va[1] == 0 && vb[1] == 0) || (va[0] == 0 && va[1] == 0) || (vb[0] == 0 && vb[1] == 0) {
		return vec{}, float64(math.Inf(1)), float64(math.Inf(1))
	}
	d := va[1]*vb[0] - va[0]*vb[1]
	if d == 0 {
		return vec{}, float64(math.Inf(1)), float64(math.Inf(1))
	}
	p := (vb[1]*(a0[0]-b0[0]) - a0[1]*vb[0] + b0[1]*vb[0]) / d
	var q float64
	if vb[0] == 0 {
		q = (a0[1] + p*va[1] - b0[1]) / vb[1]
	} else {
		q = (a0[0] + p*va[0] - b0[0]) / vb[0]
	}

	return a0.add(va.mulf(p)), p, q
}

// Fill fills the current path with the current FillStyle
func (cv *Canvas) Fill() {
	cv.fillPath(&cv.path, matIdentity())
}

// FillPath fills the given path with the current FillStyle
func (cv *Canvas) FillPath(path *Path2D) {
	cv.fillPath(path, cv.state.transform)
}

// FillPath fills the given path with the current FillStyle
func (cv *Canvas) fillPath(path *Path2D, tf mat) {
	if len(path.p) < 3 {
		return
	}

	var triBuf [500][2]float64
	tris := triBuf[:0]
	runSubPaths(path.p, func(sp []pathPoint) bool {
		tris = appendSubPathTriangles(tris, tf, sp)
		return false
	})
	if len(tris) == 0 {
		return
	}

	cv.drawShadow2(tris, nil)

	stl := cv.backendFillStyle(&cv.state.fill, 1)
	cv.b.Fill(&stl, tris)
}

func appendSubPathTriangles(tris [][2]float64, mat mat, path []pathPoint) [][2]float64 {
	last := path[len(path)-1]
	if last.flags&pathIsConvex != 0 {
		p0, p1 := path[0].pos.mulMat(mat), path[1].pos.mulMat(mat)
		last := len(path)
		for i := 2; i < last; i++ {
			p2 := path[i].pos.mulMat(mat)
			tris = append(tris, p0, p1, p2)
			p1 = p2
		}
	} else if last.flags&pathSelfIntersects != 0 {
		selfIntersectingPathParts(path, func(sp []pathPoint) bool {
			tris = triangulatePath(sp, mat, tris)
			return false
		})
	} else {
		tris = triangulatePath(path, mat, tris)
	}
	return tris
}

// Clip uses the current path to clip any further drawing. Use Save/Restore to
// remove the clipping again
func (cv *Canvas) Clip() {
	cv.clip(&cv.path, matIdentity())
}

func (cv *Canvas) clip(path *Path2D, tf mat) {
	if len(path.p) < 3 {
		return
	}

	var triBuf [500][2]float64
	tris := triBuf[:0]
	runSubPaths(path.p, func(sp []pathPoint) bool {
		tris = appendSubPathTriangles(tris, tf, sp)
		return false
	})
	if len(tris) == 0 {
		return
	}

	cv.state.clip.p = make([]pathPoint, len(path.p))
	copy(cv.state.clip.p, path.p)

	cv.b.Clip(tris)
}

// Rect creates a closed rectangle path for stroking or filling
func (cv *Canvas) Rect(x, y, w, h float64) {
	lastWasMove := len(cv.path.p) == 0 || cv.path.p[len(cv.path.p)-1].flags&pathMove != 0
	cv.MoveTo(x, y)
	cv.LineTo(x+w, y)
	cv.LineTo(x+w, y+h)
	cv.LineTo(x, y+h)
	cv.LineTo(x, y)
	if lastWasMove {
		cv.path.p[len(cv.path.p)-1].flags |= pathIsRect
		cv.path.p[len(cv.path.p)-1].flags |= pathIsConvex
	}
}

// StrokeRect draws a rectangle using the current stroke style
func (cv *Canvas) StrokeRect(x, y, w, h float64) {
	v0 := vec{x, y}
	v1 := vec{x + w, y}
	v2 := vec{x + w, y + h}
	v3 := vec{x, y + h}
	var p [5]pathPoint
	p[0] = pathPoint{pos: v0, flags: pathMove | pathAttach, next: v1}
	p[1] = pathPoint{pos: v1, next: v2, flags: pathAttach}
	p[2] = pathPoint{pos: v2, next: v3, flags: pathAttach}
	p[3] = pathPoint{pos: v3, next: v0, flags: pathAttach}
	p[4] = pathPoint{pos: v0, next: v1, flags: pathAttach}
	path := Path2D{p: p[:]}
	cv.strokePath(&path, mat{}, false)
}

// FillRect fills a rectangle with the active fill style
func (cv *Canvas) FillRect(x, y, w, h float64) {
	p0 := cv.tf(vec{x, y})
	p1 := cv.tf(vec{x, y + h})
	p2 := cv.tf(vec{x + w, y + h})
	p3 := cv.tf(vec{x + w, y})

	data := [4][2]float64{{p0[0], p0[1]}, {p1[0], p1[1]}, {p2[0], p2[1]}, {p3[0], p3[1]}}

	cv.drawShadow2(data[:], nil)

	stl := cv.backendFillStyle(&cv.state.fill, 1)
	cv.b.Fill(&stl, data[:])
}

// ClearRect sets the color of the rectangle to transparent black
func (cv *Canvas) ClearRect(x, y, w, h float64) {
	p0 := cv.tf(vec{x, y})
	p1 := cv.tf(vec{x, y + h})
	p2 := cv.tf(vec{x + w, y + h})
	p3 := cv.tf(vec{x + w, y})
	data := [4][2]float64{{p0[0], p0[1]}, {p1[0], p1[1]}, {p2[0], p2[1]}, {p3[0], p3[1]}}

	cv.b.Clear(data)
}
