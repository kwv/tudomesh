package mesh

import (
	"fmt"
	"image/color"
	"strings"
)

// Theme holds the colors of the live, floorplan and alert maps (PNG and
// SVG). The composite views keep their own per-vacuum colors.
type Theme struct {
	// Name is "light", "dark" or "auto". Auto is light in PNGs; SVGs carry
	// both and follow the viewer's prefers-color-scheme.
	Name        string
	Background  color.NRGBA
	Floor       color.NRGBA // floor without a room, and ignored rooms
	Wall        color.NRGBA
	Text        color.NRGBA // room and vacuum labels, PNG legend
	Halo        color.NRGBA // outline behind label text
	Alert       color.NRGBA // fill of the highlighted vacuum's room
	AlertBorder color.NRGBA
	AlertHatch  color.NRGBA
	Rooms       []color.NRGBA // room fills, assigned in segment id order
	// Dark is the variant an auto theme switches to in SVGs; nil otherwise.
	Dark *Theme
}

// LightTheme is the default.
var LightTheme = Theme{
	Name:        "light",
	Background:  color.NRGBA{240, 240, 240, 255},
	Floor:       color.NRGBA{200, 200, 200, 255},
	Wall:        color.NRGBA{60, 60, 60, 255},
	Text:        color.NRGBA{48, 48, 48, 255},
	Halo:        color.NRGBA{255, 255, 255, 255},
	Alert:       color.NRGBA{244, 163, 163, 255},
	AlertBorder: color.NRGBA{170, 30, 30, 255},
	AlertHatch:  color.NRGBA{214, 96, 96, 255},
	Rooms: []color.NRGBA{
		{207, 224, 240, 255}, // blue
		{214, 234, 210, 255}, // green
		{243, 227, 195, 255}, // sand
		{226, 214, 238, 255}, // lavender
		{210, 236, 234, 255}, // teal
		{239, 232, 200, 255}, // straw
	},
}

// DarkTheme has muted room fills and light walls on a near-black background.
var DarkTheme = Theme{
	Name:        "dark",
	Background:  color.NRGBA{24, 24, 27, 255},
	Floor:       color.NRGBA{68, 68, 72, 255},
	Wall:        color.NRGBA{205, 205, 210, 255},
	Text:        color.NRGBA{235, 235, 235, 255},
	Halo:        color.NRGBA{24, 24, 27, 255},
	Alert:       color.NRGBA{122, 38, 42, 255},
	AlertBorder: color.NRGBA{240, 100, 100, 255},
	AlertHatch:  color.NRGBA{168, 64, 68, 255},
	Rooms: []color.NRGBA{
		{44, 64, 86, 255}, // blue
		{46, 74, 54, 255}, // green
		{86, 72, 46, 255}, // sand
		{66, 54, 86, 255}, // lavender
		{38, 76, 74, 255}, // teal
		{78, 74, 42, 255}, // straw
	},
}

// ThemeColors overrides individual theme colors from config
// (render.colors). Values are "#rrggbb"; empty keeps the theme's color.
type ThemeColors struct {
	Background string   `yaml:"background,omitempty" json:"background,omitempty"`
	Floor      string   `yaml:"floor,omitempty" json:"floor,omitempty"`
	Wall       string   `yaml:"wall,omitempty" json:"wall,omitempty"`
	Text       string   `yaml:"text,omitempty" json:"text,omitempty"`
	Halo       string   `yaml:"halo,omitempty" json:"halo,omitempty"`
	Alert      string   `yaml:"alert,omitempty" json:"alert,omitempty"`
	Rooms      []string `yaml:"rooms,omitempty" json:"rooms,omitempty"`
}

// ThemeByName returns the named theme ("" is light).
func ThemeByName(name string) (Theme, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "light":
		return LightTheme, nil
	case "dark":
		return DarkTheme, nil
	case "auto":
		t, dark := LightTheme, DarkTheme
		t.Name, t.Dark = "auto", &dark
		return t, nil
	}
	return Theme{}, fmt.Errorf("theme must be light, dark or auto, got %q", name)
}

// WithColors returns t with the colors set in c. An auto theme applies
// them to both its light and dark variants.
func (t Theme) WithColors(c ThemeColors) (Theme, error) {
	if t.Dark != nil {
		dark, err := t.Dark.WithColors(c)
		if err != nil {
			return t, err
		}
		t.Dark = &dark
	}
	set := func(dst *color.NRGBA, hex string) error {
		if hex == "" {
			return nil
		}
		v, err := ParseHexColor(hex)
		if err != nil {
			return err
		}
		*dst = v
		return nil
	}
	for _, f := range []struct {
		dst *color.NRGBA
		hex string
	}{
		{&t.Background, c.Background}, {&t.Floor, c.Floor}, {&t.Wall, c.Wall},
		{&t.Text, c.Text}, {&t.Halo, c.Halo}, {&t.Alert, c.Alert},
	} {
		if err := set(f.dst, f.hex); err != nil {
			return t, err
		}
	}
	if len(c.Rooms) > 0 {
		rooms := make([]color.NRGBA, len(c.Rooms))
		for i, hex := range c.Rooms {
			if err := set(&rooms[i], hex); err != nil {
				return t, err
			}
		}
		t.Rooms = rooms
	}
	return t, nil
}

// ParseHexColor parses "#rrggbb" (the "#" is optional).
func ParseHexColor(s string) (color.NRGBA, error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	var r, g, b uint8
	if len(h) != 6 {
		return color.NRGBA{}, fmt.Errorf("color %q is not #rrggbb", s)
	}
	if _, err := fmt.Sscanf(h, "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.NRGBA{}, fmt.Errorf("color %q is not #rrggbb", s)
	}
	return color.NRGBA{r, g, b, 255}, nil
}

// orLight returns t, or LightTheme if t is the zero value.
func (t Theme) orLight() Theme {
	if t.Name == "" && t.Rooms == nil {
		return LightTheme
	}
	return t
}

// roomColor is the fill for the i-th colored room.
func (t Theme) roomColor(i int) color.NRGBA {
	if len(t.Rooms) == 0 {
		return t.Floor
	}
	return t.Rooms[i%len(t.Rooms)]
}

// RoomColors gives each of m's rooms its fill (see roomIndexes).
func (t Theme) RoomColors(m *ValetudoMap, ignore ...string) map[string]color.NRGBA {
	idx := roomIndexes(m, ignore...)
	out := make(map[string]color.NRGBA, len(idx))
	for seg, i := range idx {
		out[seg] = t.roomColor(i)
	}
	return out
}

// cssVars returns the theme as CSS custom properties (--tm-*), with
// --tm-room-0.. for the first n palette slots.
func (t Theme) cssVars(n int) string {
	var b strings.Builder
	for _, v := range []struct {
		name string
		c    color.NRGBA
	}{
		{"bg", t.Background}, {"floor", t.Floor}, {"wall", t.Wall}, {"text", t.Text},
		{"halo", t.Halo}, {"alert", t.Alert}, {"alert-border", t.AlertBorder},
	} {
		fmt.Fprintf(&b, "--tm-%s:%s;", v.name, svgColor(v.c))
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "--tm-room-%d:%s;", i, svgColor(t.roomColor(i)))
	}
	return b.String()
}

// svgThemeCSS sets the --tm-* variables on the svg element, with rooms
// palette slots; an auto theme's Dark variant applies under
// prefers-color-scheme: dark. A page selector that targets the svg element
// beats these rules, so an inline SVG can be restyled by the page.
func svgThemeCSS(t Theme, rooms int) string {
	css := "svg{" + t.cssVars(rooms) + "}\n"
	if t.Dark != nil {
		css += "@media (prefers-color-scheme:dark){svg{" + t.Dark.cssVars(rooms) + "}}\n"
	}
	return css
}
