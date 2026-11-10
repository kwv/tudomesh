package mesh

// Test-only helpers.

import "image"

// RenderGreyscale creates a greyscale composite image without color coding or
// legend, with the reference map's rooms shaded (RoomColors), outlined and
// labelled.
func (r *CompositeRenderer) RenderGreyscale() *image.RGBA {
	img := r.renderBase(nil)
	r.drawRoomOverlay(img)
	return img
}

// CreateRotationTranslation creates a combined rotation + translation transform
// Rotation is applied first (around origin), then translation
func CreateRotationTranslation(degrees, tx, ty float64) AffineMatrix {
	rot := RotationDeg(degrees)
	return AffineMatrix{
		A:  rot.A,
		B:  rot.B,
		Tx: tx,
		C:  rot.C,
		D:  rot.D,
		Ty: ty,
	}
}

// hasGlyph reports whether one of f's fonts has a real (non-notdef) glyph for r.
func (f *fallbackFace) hasGlyph(r rune) bool {
	for _, fnt := range f.fonts {
		if idx, err := fnt.GlyphIndex(&f.buf, r); err == nil && idx != 0 {
			return true
		}
	}
	return false
}

// newMQTTClientWithMock creates an MQTTClient with a provided MQTTClientInterface
// This is used for testing with mock clients
func newMQTTClientWithMock(client MQTTClientInterface, config *Config, handler MessageHandler) *MQTTClient {
	return &MQTTClient{
		client:         client,
		config:         config,
		messageHandler: handler,
	}
}

// Scale creates a scaling transform
func Scale(sx, sy float64) AffineMatrix {
	return AffineMatrix{A: sx, B: 0, Tx: 0, C: 0, D: sy, Ty: 0}
}
