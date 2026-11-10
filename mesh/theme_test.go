package mesh

import (
	"bytes"
	"image/color"
	"strings"
	"testing"
)

func TestThemeByName(t *testing.T) {
	for name, want := range map[string]string{"": "light", "Light": "light", "dark": "dark", " auto ": "auto"} {
		got, err := ThemeByName(name)
		if err != nil || got.Name != want {
			t.Errorf("ThemeByName(%q) = %q, %v; want %q", name, got.Name, err, want)
		}
	}
	if _, err := ThemeByName("solarized"); err == nil {
		t.Error("unknown theme accepted")
	}
	auto, _ := ThemeByName("auto")
	if auto.Background != LightTheme.Background || auto.Dark == nil || auto.Dark.Background != DarkTheme.Background {
		t.Errorf("auto = %+v, want light with a dark variant", auto)
	}
}

func TestThemeWithColors(t *testing.T) {
	auto, _ := ThemeByName("auto")
	got, err := auto.WithColors(ThemeColors{Wall: "#112233", Rooms: []string{"abcdef"}})
	if err != nil {
		t.Fatal(err)
	}
	want := color.NRGBA{0x11, 0x22, 0x33, 255}
	if got.Wall != want || got.Dark.Wall != want {
		t.Errorf("wall = %v / dark %v, want %v on both", got.Wall, got.Dark.Wall, want)
	}
	if len(got.Rooms) != 1 || got.roomColor(3) != (color.NRGBA{0xab, 0xcd, 0xef, 255}) {
		t.Errorf("rooms = %v", got.Rooms)
	}
	if LightTheme.Wall == want || DarkTheme.Wall == want {
		t.Error("WithColors changed the built-in themes")
	}
	if _, err := LightTheme.WithColors(ThemeColors{Floor: "grey"}); err == nil {
		t.Error("bad color accepted")
	}
}

func TestConfigResolveTheme(t *testing.T) {
	c := &Config{Render: RenderConfig{Theme: "dark", Colors: ThemeColors{Background: "#000000"}}}
	th, err := c.ResolveTheme("")
	if err != nil || th.Name != "dark" || th.Background != (color.NRGBA{0, 0, 0, 255}) {
		t.Errorf("configured = %q %v, %v", th.Name, th.Background, err)
	}
	if th, _ := c.ResolveTheme("light"); th.Name != "light" || th.Background != (color.NRGBA{0, 0, 0, 255}) {
		t.Errorf("override = %q %v; want light with the configured background", th.Name, th.Background)
	}
	if th, err := (*Config)(nil).ResolveTheme(""); err != nil || th.Name != "light" {
		t.Errorf("nil config = %q, %v", th.Name, err)
	}
}

func TestRenderLiveToSVG_Themes(t *testing.T) {
	render := func(name string) string {
		r := liveTestRenderer()
		r.Theme, _ = ThemeByName(name)
		var buf bytes.Buffer
		if err := r.RenderLiveToSVG(&buf, nil); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	dark := render("dark")
	if !strings.Contains(dark, "--tm-bg:"+svgColor(DarkTheme.Background)+";") || strings.Contains(dark, "prefers-color-scheme") {
		t.Errorf("dark theme vars missing or auto media query present")
	}
	auto := render("auto")
	if !strings.Contains(auto, "--tm-bg:"+svgColor(LightTheme.Background)+";") ||
		!strings.Contains(auto, "@media (prefers-color-scheme:dark){svg{--tm-bg:"+svgColor(DarkTheme.Background)+";") {
		t.Errorf("auto theme lacks light vars or dark media query")
	}
	if !strings.Contains(render(""), "--tm-bg:"+svgColor(LightTheme.Background)+";") {
		t.Error("zero theme is not light")
	}
}

func TestRenderLive_PNGDarkTheme(t *testing.T) {
	r := NewCompositeRenderer(cropTestMap(), map[string]AffineMatrix{"ref": Identity()}, "ref")
	r.Theme = DarkTheme
	img := r.RenderLive(nil)
	if got := nrgbaAt(img, 1, 1); got != DarkTheme.Background {
		t.Errorf("background = %v, want %v", got, DarkTheme.Background)
	}
	if got := nrgbaAt(img, 30+40, 30+5); got != DarkTheme.Rooms[0] {
		t.Errorf("kitchen = %v, want %v", got, DarkTheme.Rooms[0])
	}
}
