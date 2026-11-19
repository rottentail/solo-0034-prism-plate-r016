// Package raster produces deterministic, supersampled PNG exposures.
//
// Two pipelines share one geometry contract:
//
//   - Documents without layer effects use the original per-pixel, four-sample
//     evaluator and remain byte-identical to earlier releases.
//   - Documents with at least one effect use a banded pipeline. Effects apply
//     to the whole composited layer RGBA (never per shape), computed in
//     premultiplied alpha, so shape intersections never reveal internal
//     edges and transparent edges never turn black.
package raster

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"prismplate/internal/effect"
	"prismplate/internal/geom"
	"prismplate/internal/paint"
	"prismplate/internal/scene"
)

// Band geometry for the effect pipeline. The layer working tile extends
// effect.MaxRadius pixels on every side so a truncated 3-sigma kernel still
// reaches every needed sample inside the visible band.
const (
	bandRows   = 64
	tileMargin = 96
	tileRows   = bandRows + 2*tileMargin
)

func hasEffects(c scene.Compiled) bool {
	for _, l := range c.Layers {
		if l.Layer.Effects != nil {
			return true
		}
	}
	return false
}

// renderLegacy is the original supersampled evaluator, retained unchanged so
// effect-free scenes keep the exact bytes of prior releases.
func renderLegacy(ctx context.Context, c scene.Compiled) ([]byte, error) {
	d := c.Document
	img := image.NewNRGBA(image.Rect(0, 0, d.Width, d.Height))
	for y := 0; y < d.Height; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < d.Width; x++ {
			sum := paint.Pixel{}
			for sy := 0; sy < 2; sy++ {
				for sx := 0; sx < 2; sx++ {
					p := geom.Point{X: float64(x) + (float64(sx)+0.5)/2, Y: float64(y) + (float64(sy)+0.5)/2}
					dst := paint.From(d.Background)
					for _, l := range c.Layers {
						if len(l.Clip) > 0 && !l.Clip.Contains(p, "nonzero") {
							continue
						}
						if len(l.Mask) > 0 && !l.Mask.Contains(p, "nonzero") {
							continue
						}
						src := paint.Pixel{}
						for _, s := range l.Shapes {
							q := s.Inverse.Apply(p)
							if s.Path.Contains(q, s.Shape.FillRule) {
								src = paint.Over(src, paint.Sample(s.Shape.Brush, q), "normal")
							}
						}
						dst = paint.Over(dst, src.Scale(l.Layer.Opacity), l.Layer.Blend)
					}
					sum.R += dst.R / 4
					sum.G += dst.G / 4
					sum.B += dst.B / 4
					sum.A += dst.A / 4
				}
			}
			r, g, b, a := sum.RGBA8()
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	var out bytes.Buffer
	err := png.Encode(&out, img)
	return out.Bytes(), err
}

// layerPixel evaluates one layer at canvas point p, averaging the four
// coverage samples within the pixel before effects are applied. The effect
// itself smooths sub-pixel variation, so averaging at the layer boundary is
// within one quantization step of full four-sample compositing.
func layerPixel(l scene.CompiledLayer, p geom.Point) paint.Pixel {
	if len(l.Clip) > 0 && !l.Clip.Contains(p, "nonzero") {
		return paint.Pixel{}
	}
	if len(l.Mask) > 0 && !l.Mask.Contains(p, "nonzero") {
		return paint.Pixel{}
	}
	src := paint.Pixel{}
	for _, s := range l.Shapes {
		q := s.Inverse.Apply(p)
		if s.Path.Contains(q, s.Shape.FillRule) {
			src = paint.Over(src, paint.Sample(s.Shape.Brush, q), "normal")
		}
	}
	return src
}

func putPixel(buf []float32, w, x, y int, p paint.Pixel) {
	o := (y*w + x) * 4
	buf[o] = float32(p.R)
	buf[o+1] = float32(p.G)
	buf[o+2] = float32(p.B)
	buf[o+3] = float32(p.A)
}

func getPixel(buf []float32, w, x, y int) paint.Pixel {
	o := (y*w + x) * 4
	return paint.Pixel{R: float64(buf[o]), G: float64(buf[o+1]), B: float64(buf[o+2]), A: float64(buf[o+3])}
}

// renderEffects runs the bounded banded pipeline. Every layer effect is
// resolved before layer opacity and blend mode composite against the canvas,
// matching "the whole composited layer, then its effect" semantics.
func renderEffects(ctx context.Context, c scene.Compiled) ([]byte, error) {
	d := c.Document
	w, h := d.Width, d.Height
	img := image.NewNRGBA(image.Rect(0, 0, w, h))

	frame := make([]float32, bandRows*w*4)
	tile := make([]float32, tileRows*w*4)
	work := make([]float32, tileRows*w*4)
	alpha := make([]float32, tileRows*w)
	alphaWork := make([]float32, tileRows*w)

	blurKernels := map[float64][]float32{}
	kernel := func(sigma float64) []float32 {
		if k, ok := blurKernels[sigma]; ok {
			return k
		}
		k := effect.Kernel(sigma)
		blurKernels[sigma] = k
		return k
	}

	for top := 0; top < h; top += bandRows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows := bandRows
		if top+rows > h {
			rows = h - top
		}
		for i := range frame {
			frame[i] = 0
		}
		// Background is a canvas-level fact evaluated per band pixel.
		for y := 0; y < rows; y++ {
			for x := 0; x < w; x++ {
				putPixel(frame, w, x, y, paint.From(d.Background))
			}
		}

		for _, l := range c.Layers {
			if l.Layer.Effects == nil {
				for y := 0; y < rows; y++ {
					for x := 0; x < w; x++ {
						p := geom.Point{X: float64(x) + 0.5, Y: float64(top+y) + 0.5}
						src := layerPixel(l, p)
						dst := getPixel(frame, w, x, y)
						putPixel(frame, w, x, y, paint.Over(dst, src.Scale(l.Layer.Opacity), l.Layer.Blend))
					}
				}
				continue
			}
			for i := range tile {
				tile[i] = 0
			}
			tileTop := top - tileMargin
			for ty := 0; ty < tileRows; ty++ {
				gy := tileTop + ty
				if gy < 0 || gy >= h {
					continue // outside the canvas is transparent, the tile stays zero
				}
				for x := 0; x < w; x++ {
					sum := paint.Pixel{}
					for sy := 0; sy < 2; sy++ {
						for sx := 0; sx < 2; sx++ {
							p := geom.Point{X: float64(x) + (float64(sx)+0.5)/2, Y: float64(gy) + (float64(sy)+0.5)/2}
							q := layerPixel(l, p)
							sum.R += q.R / 4
							sum.G += q.G / 4
							sum.B += q.B / 4
							sum.A += q.A / 4
						}
					}
					putPixel(tile, w, x, ty, sum)
				}
			}
			eff := l.Layer.Effects
			if eff.Blur != nil {
				effect.BlurRGBA(tile, w, tileRows, kernel(eff.Blur.Radius), work)
			}
			if eff.Shadow != nil {
				effect.BlurAlpha(alpha, tile, w, tileRows, kernel(eff.Shadow.Radius), alphaWork)
			}
			for y := 0; y < rows; y++ {
				for x := 0; x < w; x++ {
					ty := y + tileMargin
					// Build the complete layer result (shadow under body) first,
					// then apply layer opacity and blend mode once, so both the
					// blurred body and its shadow respect group compositing.
					layerOut := getPixel(tile, w, x, ty)
					if eff.Shadow != nil {
						// Fractional offsets sample the alpha plane bilinearly in
						// tile-index space: integer offsets land on an exact cell,
						// fractional offsets interpolate between neighbors instead
						// of snapping to a whole pixel (notable for radius 0).
						fx := float64(x) - eff.Shadow.Offset.X
						fy := float64(ty) - eff.Shadow.Offset.Y
						if a := sampleAlpha(alpha, w, tileRows, fx, fy); a > 0 {
							sh := paint.From(eff.Shadow.Color).Scale(a)
							layerOut = paint.Over(layerOut, sh, "normal")
						}
					}
					dst := getPixel(frame, w, x, y)
					putPixel(frame, w, x, y, paint.Over(dst, layerOut.Scale(l.Layer.Opacity), l.Layer.Blend))
				}
			}
		}

		for y := 0; y < rows; y++ {
			for x := 0; x < w; x++ {
				p := getPixel(frame, w, x, y)
				// Quantize alpha first: once the 8-bit alpha rounds to zero the
				// pixel is fully transparent and its unassociated RGB must be
				// zero too, so transparent edges can never carry color fringe.
				if math.Min(1, p.A)*255 < 0.5 {
					img.SetNRGBA(x, top+y, color.NRGBA{})
					continue
				}
				r, g, b, a := p.RGBA8()
				img.SetNRGBA(x, top+y, color.NRGBA{R: r, G: g, B: b, A: a})
			}
		}
	}
	var out bytes.Buffer
	err := png.Encode(&out, img)
	return out.Bytes(), err
}

// sampleAlpha bilinearly interpolates the single-channel alpha tile at
// continuous coordinate (fx,fy). Coordinates outside the tile read zero, so
// the shadow simply ends at the canvas/visible boundary instead of clamping.
func sampleAlpha(a []float32, w, h int, fx, fy float64) float64 {
	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	x1, y1 := x0+1, y0+1
	if x1 < 0 || y1 < 0 || x0 >= w || y0 >= h {
		return 0
	}
	tx := fx - float64(x0)
	ty := fy - float64(y0)
	get := func(px, py int) float64 {
		if px < 0 || py < 0 || px >= w || py >= h {
			return 0
		}
		return float64(a[py*w+px])
	}
	v00, v10 := get(x0, y0), get(x1, y0)
	v01, v11 := get(x0, y1), get(x1, y1)
	top := v00 + (v10-v00)*tx
	bot := v01 + (v11-v01)*tx
	return top + (bot-top)*ty
}

func Render(ctx context.Context, c scene.Compiled) ([]byte, error) {
	if hasEffects(c) {
		return renderEffects(ctx, c)
	}
	return renderLegacy(ctx, c)
}
