// Package scene defines the portable prism-plate-v1 document contract.
package scene

import "prismplate/internal/geom"

type Color struct {
	R float64 `json:"r"`
	G float64 `json:"g"`
	B float64 `json:"b"`
	A float64 `json:"a"`
}
type Stop struct {
	At    float64 `json:"at"`
	Color Color   `json:"color"`
}
type Brush struct {
	Kind  string     `json:"kind"`
	Color Color      `json:"color"`
	From  geom.Point `json:"from"`
	To    geom.Point `json:"to"`
	Stops []Stop     `json:"stops,omitempty"`
}
type Shape struct {
	ID        string      `json:"id"`
	Path      []geom.Op   `json:"path"`
	FillRule  string      `json:"fill_rule"`
	Brush     Brush       `json:"brush"`
	Transform geom.Matrix `json:"transform"`
}

// BlurEffect is a Gaussian layer blur. Radius is the standard deviation sigma
// in canvas pixels. The blur operates on the whole composited layer result,
// never on individual shapes, so intersections never expose internal edges.
type BlurEffect struct {
	Radius float64 `json:"radius"`
}

// ShadowEffect is a soft drop shadow derived from the composited layer alpha.
// Radius is the Gaussian sigma in canvas pixels; Offset is in canvas pixels.
// Shadow strength is the color alpha; zero alpha is rejected at admission.
type ShadowEffect struct {
	Color  Color      `json:"color"`
	Offset geom.Point `json:"offset"`
	Radius float64    `json:"radius"`
}

// LayerEffects groups the optional per-layer post-compositing effects.
type LayerEffects struct {
	Blur   *BlurEffect   `json:"blur,omitempty"`
	Shadow *ShadowEffect `json:"shadow,omitempty"`
}

type Layer struct {
	ID      string        `json:"id"`
	Opacity float64       `json:"opacity"`
	Blend   string        `json:"blend"`
	Clip    []geom.Op     `json:"clip,omitempty"`
	Mask    []geom.Op     `json:"mask,omitempty"`
	Shapes  []Shape       `json:"shapes"`
	Effects *LayerEffects `json:"effects,omitempty"`
}
type Document struct {
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Background Color   `json:"background"`
	Layers     []Layer `json:"layers"`
}
type CompiledShape struct {
	Shape   Shape
	Path    geom.Path
	Inverse geom.Matrix
}
type CompiledLayer struct {
	Layer  Layer
	Clip   geom.Path
	Mask   geom.Path
	Shapes []CompiledShape
}
type Compiled struct {
	Document   Document
	Layers     []CompiledLayer
	Segments   int
	EffectTaps int64
}
