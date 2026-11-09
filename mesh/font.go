package mesh

import (
	"fmt"
	"image"
	"os"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// PNG text uses Go Regular (Latin, Greek, Cyrillic). SetFontFile adds a font
// that is tried first, for scripts Go Regular lacks (e.g. CJK); runes it
// has no glyph for fall back to Go Regular.

var goRegularFont = sync.OnceValue(func() *opentype.Font {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		panic(fmt.Sprintf("parsing bundled Go Regular font: %v", err))
	}
	return f
})

var (
	fontMu     sync.RWMutex
	customFont *opentype.Font
)

// SetFontFile loads a TrueType/OpenType font (.ttf, .otf, or the first font
// of a .ttc/.otc collection) for PNG text. An empty path restores the
// bundled font only.
func SetFontFile(path string) error {
	if path == "" {
		fontMu.Lock()
		customFont = nil
		fontMu.Unlock()
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading font: %w", err)
	}
	c, err := opentype.ParseCollection(data)
	if err != nil {
		return fmt.Errorf("parsing font %s: %w", path, err)
	}
	f, err := c.Font(0)
	if err != nil {
		return fmt.Errorf("parsing font %s: %w", path, err)
	}
	fontMu.Lock()
	customFont = f
	fontMu.Unlock()
	return nil
}

// newTextFace returns a face of the given pixel size. Faces are not safe for
// concurrent use, so make one per render.
func newTextFace(size float64) *fallbackFace {
	fontMu.RLock()
	fonts := []*opentype.Font{customFont, goRegularFont()}
	fontMu.RUnlock()
	if fonts[0] == nil {
		fonts = fonts[1:]
	}

	ff := &fallbackFace{}
	for _, f := range fonts {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			continue
		}
		ff.fonts = append(ff.fonts, f)
		ff.faces = append(ff.faces, face)
	}
	return ff
}

// fallbackFace draws each rune with the first face whose font has a glyph
// for it; metrics come from the first face.
type fallbackFace struct {
	fonts []*opentype.Font
	faces []font.Face
	buf   sfnt.Buffer
}

func (f *fallbackFace) pick(r rune) font.Face {
	if len(f.faces) == 1 {
		return f.faces[0]
	}
	for i, fnt := range f.fonts {
		if idx, err := fnt.GlyphIndex(&f.buf, r); err == nil && idx != 0 {
			return f.faces[i]
		}
	}
	return f.faces[0]
}

func (f *fallbackFace) Close() error {
	for _, face := range f.faces {
		_ = face.Close()
	}
	return nil
}

func (f *fallbackFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	return f.pick(r).Glyph(dot, r)
}

func (f *fallbackFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return f.pick(r).GlyphBounds(r)
}

func (f *fallbackFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	return f.pick(r).GlyphAdvance(r)
}

func (f *fallbackFace) Kern(r0, r1 rune) fixed.Int26_6 {
	if a := f.pick(r0); a == f.pick(r1) {
		return a.Kern(r0, r1)
	}
	return 0
}

func (f *fallbackFace) Metrics() font.Metrics { return f.faces[0].Metrics() }

// hasGlyph reports whether one of f's fonts has a real (non-notdef) glyph for r.
func (f *fallbackFace) hasGlyph(r rune) bool {
	for _, fnt := range f.fonts {
		if idx, err := fnt.GlyphIndex(&f.buf, r); err == nil && idx != 0 {
			return true
		}
	}
	return false
}
