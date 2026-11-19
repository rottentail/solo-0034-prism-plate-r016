// Package effect implements bounded post-compositing layer effects.
//
// All buffers hold linear-light, premultiplied-alpha pixels. Convolution runs
// on the premultiplied channels directly: samples outside the known region
// contribute nothing, so transparent edges stay transparent instead of
// picking up black fringe from unassociated zero RGB values.
package effect

import "math"

// Bounds shared with validate; effects outside these ranges are rejected at
// scene admission rather than silently clamped during rendering.
const (
	MaxRadius = 32.0 // Gaussian sigma, canvas pixels
	MaxOffset = 32.0 // absolute shadow offset per axis, canvas pixels
	KernelCut = 3    // kernel truncation at 3 sigma
	MaxTap    = 97   // 2*ceil(3*32)+1
)

// Kernel returns the normalized, symmetric Gaussian weights for sigma.
// Radius zero yields the single-tap identity kernel (hard, unblurred shadow).
func Kernel(sigma float64) []float32 {
	if sigma <= 0 {
		return []float32{1}
	}
	r := int(math.Ceil(KernelCut * sigma))
	if r > MaxTap/2 {
		r = MaxTap / 2
	}
	k := make([]float32, 2*r+1)
	inv := 1 / (2 * sigma * sigma)
	var sum float64
	for i := -r; i <= r; i++ {
		v := math.Exp(-float64(i*i) * inv)
		k[i+r] = float32(v)
		sum += v
	}
	for i := range k {
		k[i] = float32(float64(k[i]) / sum)
	}
	return k
}

// BlurRGBA applies a separable Gaussian in place to an RGBA premultiplied
// tile of width w and height h. scratch must have capacity 4*w*h. Samples
// beyond the tile are transparent zero (not edge-clamped) and the kernel is
// not renormalized, so content reaching the boundary loses energy exactly as
// it does under an SVG filter region: edges blur out instead of being held.
func BlurRGBA(buf []float32, w, h int, k, scratch []float32) {
	r := (len(k) - 1) / 2
	// Horizontal pass: buf -> scratch.
	for y := 0; y < h; y++ {
		row := y * w * 4
		for x := 0; x < w; x++ {
			var sr, sg, sb, sa float32
			for dx := -r; dx <= r; dx++ {
				sx := x + dx
				if sx < 0 || sx >= w {
					continue // transparent padding contributes nothing
				}
				wt := k[dx+r]
				o := row + sx*4
				sr += wt * buf[o]
				sg += wt * buf[o+1]
				sb += wt * buf[o+2]
				sa += wt * buf[o+3]
			}
			o := row + x*4
			scratch[o] = sr
			scratch[o+1] = sg
			scratch[o+2] = sb
			scratch[o+3] = sa
		}
	}
	// Vertical pass: scratch -> buf.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sr, sg, sb, sa float32
			for dy := -r; dy <= r; dy++ {
				sy := y + dy
				if sy < 0 || sy >= h {
					continue
				}
				wt := k[dy+r]
				o := (sy*w + x) * 4
				sr += wt * scratch[o]
				sg += wt * scratch[o+1]
				sb += wt * scratch[o+2]
				sa += wt * scratch[o+3]
			}
			o := (y*w + x) * 4
			buf[o] = sr
			buf[o+1] = sg
			buf[o+2] = sb
			buf[o+3] = sa
		}
	}
}

// BlurAlpha convolves only the alpha plane of src into the single-channel dst.
// scratch must have capacity w*h. Used to derive the soft shadow silhouette;
// out-of-tile samples are transparent zero with no kernel renormalization.
func BlurAlpha(dst []float32, src []float32, w, h int, k, scratch []float32) {
	r := (len(k) - 1) / 2
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			var acc float32
			for dx := -r; dx <= r; dx++ {
				sx := x + dx
				if sx < 0 || sx >= w {
					continue
				}
				acc += k[dx+r] * src[(row+sx)*4+3]
			}
			scratch[row+x] = acc
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var acc float32
			for dy := -r; dy <= r; dy++ {
				sy := y + dy
				if sy < 0 || sy >= h {
					continue
				}
				acc += k[dy+r] * scratch[sy*w+x]
			}
			dst[y*w+x] = acc
		}
	}
}
