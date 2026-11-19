// Package vector exports the compiled contour representation without raw markup.
package vector

import (
	"bytes"
	"fmt"
	"math"
	"prismplate/internal/geom"
	"prismplate/internal/scene"
	"strings"
)

func number(x float64) string { return fmt.Sprintf("%.9g", x) }
func path(p geom.Path) string {
	var b strings.Builder
	for _, ring := range p {
		for i, v := range ring {
			verb := "L"
			if i == 0 {
				verb = "M"
			}
			fmt.Fprintf(&b, "%s%s %s", verb, number(v.X), number(v.Y))
		}
		b.WriteString("Z")
	}
	return b.String()
}
func color(c scene.Color) string {
	return fmt.Sprintf("rgb(%s%%,%s%%,%s%%)", number(c.R*100), number(c.G*100), number(c.B*100))
}

// writeFilter emits a self-contained filter matching the raster pipeline:
// optional Gaussian blur of the whole layer, then a premultiplied drop
// shadow derived from the layer alpha, merged back over the layer.
func writeFilter(b *bytes.Buffer, i int, e *scene.LayerEffects, dw, dh float64) {
	reach := 0.0
	if e.Blur != nil {
		reach = 3 * e.Blur.Radius
	}
	if e.Shadow != nil {
		r := 3*e.Shadow.Radius + math.Abs(e.Shadow.Offset.X)
		if r > reach {
			reach = r
		}
		r = 3*e.Shadow.Radius + math.Abs(e.Shadow.Offset.Y)
		if r > reach {
			reach = r
		}
	}
	fmt.Fprintf(b, `<filter id="f%d" x="-%.6g" y="-%.6g" width="%.6g" height="%.6g" filterUnits="userSpaceOnUse" color-interpolation-filters="linearRGB">`, i, reach, reach, dw+2*reach, dh+2*reach)
	body := "SourceGraphic"
	if e.Blur != nil {
		// Whole-layer blur; this is the graphic placed over the shadow.
		fmt.Fprintf(b, `<feGaussianBlur in="SourceGraphic" stdDeviation="%.9g" result="blur"/>`, e.Blur.Radius)
		body = "blur"
	}
	if e.Shadow != nil {
		// The shadow is derived from the layer alpha silhouette (SourceAlpha),
		// matching the raster BlurAlpha pass, never from colored pixels.
		in := "SourceAlpha"
		if e.Shadow.Radius > 0 {
			fmt.Fprintf(b, `<feGaussianBlur in="SourceAlpha" stdDeviation="%.9g" result="shadowBlur"/>`, e.Shadow.Radius)
			in = "shadowBlur"
		}
		fmt.Fprintf(b, `<feOffset in="%s" dx="%.9g" dy="%.9g" result="shadowOffset"/>`, in, e.Shadow.Offset.X, e.Shadow.Offset.Y)
		fmt.Fprintf(b, `<feFlood flood-color="%s" flood-opacity="%s" result="shadowColor"/>`, color(e.Shadow.Color), number(e.Shadow.Color.A))
		b.WriteString(`<feComposite in="shadowColor" in2="shadowOffset" operator="in" result="shadow"/>`)
		fmt.Fprintf(b, `<feMerge><feMergeNode in="shadow"/><feMergeNode in="%s"/></feMerge>`, body)
	} else {
		fmt.Fprintf(b, `<feMerge><feMergeNode in="%s"/></feMerge>`, body)
	}
	b.WriteString(`</filter>`)
}

// SVG uses the same flattened geometry. Browser gradient alpha interpolation and
// pixel coverage may differ from the canonical PNG renderer; SVG is a vector proof.
func Export(c scene.Compiled) []byte {
	var b bytes.Buffer
	d := c.Document
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" color-interpolation="linearRGB"><defs>`, d.Width, d.Height, d.Width, d.Height)
	for i, l := range c.Layers {
		if len(l.Clip) > 0 {
			fmt.Fprintf(&b, `<clipPath id="c%d" clipPathUnits="userSpaceOnUse"><path d="%s"/></clipPath>`, i, path(l.Clip))
		}
		if len(l.Mask) > 0 {
			fmt.Fprintf(&b, `<clipPath id="m%d" clipPathUnits="userSpaceOnUse"><path d="%s"/></clipPath>`, i, path(l.Mask))
		}
		if l.Layer.Effects != nil {
			writeFilter(&b, i, l.Layer.Effects, float64(d.Width), float64(d.Height))
		}
		for j, s := range l.Shapes {
			br := s.Shape.Brush
			if br.Kind == "linear" {
				fmt.Fprintf(&b, `<linearGradient id="b%d_%d" gradientUnits="userSpaceOnUse" x1="%s" y1="%s" x2="%s" y2="%s">`, i, j, number(br.From.X), number(br.From.Y), number(br.To.X), number(br.To.Y))
				for _, stop := range br.Stops {
					fmt.Fprintf(&b, `<stop offset="%s" stop-color="%s" stop-opacity="%s"/>`, number(stop.At), color(stop.Color), number(stop.Color.A))
				}
				b.WriteString(`</linearGradient>`)
			}
		}
	}
	b.WriteString(`</defs>`)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" fill="%s" fill-opacity="%s"/>`, color(d.Background), number(d.Background.A))
	for i, l := range c.Layers {
		fmt.Fprintf(&b, `<g opacity="%s" style="isolation:isolate;mix-blend-mode:%s"`, number(l.Layer.Opacity), map[string]string{"normal": "normal", "multiply": "multiply", "screen": "screen"}[l.Layer.Blend])
		if len(l.Clip) > 0 {
			fmt.Fprintf(&b, ` clip-path="url(#c%d)"`, i)
		}
		if l.Layer.Effects != nil {
			fmt.Fprintf(&b, ` filter="url(#f%d)"`, i)
		}
		b.WriteString(`><g`)
		if len(l.Mask) > 0 {
			fmt.Fprintf(&b, ` clip-path="url(#m%d)"`, i)
		}
		b.WriteString(`>`)
		for j, s := range l.Shapes {
			m := s.Shape.Transform
			br := s.Shape.Brush
			fill := color(br.Color)
			alpha := br.Color.A
			if br.Kind == "linear" {
				fill = fmt.Sprintf("url(#b%d_%d)", i, j)
				alpha = 1
			}
			fmt.Fprintf(&b, `<path d="%s" fill="%s" fill-opacity="%s" fill-rule="%s" transform="matrix(%s %s %s %s %s %s)"/>`, path(s.Path), fill, number(alpha), s.Shape.FillRule, number(m[0]), number(m[1]), number(m[2]), number(m[3]), number(m[4]), number(m[5]))
		}
		b.WriteString(`</g></g>`)
	}
	b.WriteString(`</svg>`)
	return b.Bytes()
}
