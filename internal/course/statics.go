package course

import (
	"errors"
	"math"
)

var ErrIndeterminate = errors.New("course: fbd not statically determinate")

// solveFBD 解 2D 单刚体平衡(ΣFx=ΣFy=ΣM=0)。v1 支座 pin(2 未知)/ roller(1 未知,沿法向),
// 未知反力分量须恰好 3 个(静定)。返回 supportID → 反力矢量。
func solveFBD(spec *FBDSpec) (map[string]FBDVec, error) {
	// 每个未知量一列,记录它对 [ΣFx, ΣFy, ΣM] 三个方程的系数,以及归属支座/装配方式。
	type col struct {
		supID  string
		comp   string // "x" / "y"(pin 分量)| "n"(roller 沿法向标量)
		nx, ny float64
		a      [3]float64
	}
	var cols []col
	for _, s := range spec.Supports {
		switch s.Type {
		case "pin":
			cols = append(cols, col{supID: s.ID, comp: "x", a: [3]float64{1, 0, -s.Y}})
			cols = append(cols, col{supID: s.ID, comp: "y", a: [3]float64{0, 1, s.X}})
		case "roller":
			rad := s.Angle * math.Pi / 180
			nx, ny := math.Cos(rad), math.Sin(rad)
			cols = append(cols, col{
				supID: s.ID, comp: "n", nx: nx, ny: ny,
				a: [3]float64{nx, ny, s.X*ny - s.Y*nx},
			})
		default:
			return nil, ErrIndeterminate
		}
	}
	if len(cols) != 3 {
		return nil, ErrIndeterminate
	}

	// RHS = -Σ已知载荷(力 + 对原点的力矩)
	var sfx, sfy, sm float64
	for _, l := range spec.Loads {
		sfx += l.Fx
		sfy += l.Fy
		sm += l.X*l.Fy - l.Y*l.Fx
	}
	b := [3]float64{-sfx, -sfy, -sm}

	var A [3][3]float64
	for j := 0; j < 3; j++ {
		A[0][j] = cols[j].a[0]
		A[1][j] = cols[j].a[1]
		A[2][j] = cols[j].a[2]
	}
	x, err := solve3(A, b)
	if err != nil {
		return nil, err
	}

	out := map[string]FBDVec{}
	for j, c := range cols {
		v := out[c.supID]
		switch c.comp {
		case "x":
			v.Fx += x[j]
		case "y":
			v.Fy += x[j]
		case "n":
			v.Fx += x[j] * c.nx
			v.Fy += x[j] * c.ny
		}
		out[c.supID] = v
	}
	return out, nil
}

// solve3 用克拉默法则解 3×3。行列式接近 0(几何退化/非静定)报错。
func solve3(A [3][3]float64, b [3]float64) ([3]float64, error) {
	det := det3(A)
	if math.Abs(det) < 1e-9 {
		return [3]float64{}, ErrIndeterminate
	}
	var x [3]float64
	for i := 0; i < 3; i++ {
		Ai := A
		for r := 0; r < 3; r++ {
			Ai[r][i] = b[r]
		}
		x[i] = det3(Ai) / det
	}
	return x, nil
}

func det3(m [3][3]float64) float64 {
	return m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
}

// fbdReactionOK 学生反力分量与标准解在容差内(相对容差 + 绝对下限)。
func fbdReactionOK(got, want FBDVec, tol float64) bool {
	if tol <= 0 {
		tol = 0.05
	}
	mag := math.Hypot(want.Fx, want.Fy)
	eps := math.Max(0.5, tol*math.Max(mag, 1))
	return math.Abs(got.Fx-want.Fx) <= eps && math.Abs(got.Fy-want.Fy) <= eps
}
