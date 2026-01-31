package mesh

import "testing"

func TestHasDrawableContent(t *testing.T) {
	// Case 1: no maps
	r := &CompositeRenderer{Maps: map[string]*ValetudoMap{}}
	if r.HasDrawableContent() {
		t.Fatalf("expected no drawable content when maps empty")
	}

	// Case 2: map present but layers empty
	m1 := &ValetudoMap{Layers: []MapLayer{}}
	r = &CompositeRenderer{Maps: map[string]*ValetudoMap{"vac1": m1}}
	if r.HasDrawableContent() {
		t.Fatalf("expected no drawable content when layers empty")
	}

	// Case 3: map with a floor layer but zero pixels
	m2 := &ValetudoMap{Layers: []MapLayer{{Type: "floor", Pixels: []int{}}, {Type: "wall", Pixels: []int{}}}}
	r = &CompositeRenderer{Maps: map[string]*ValetudoMap{"vac2": m2}}
	if r.HasDrawableContent() {
		t.Fatalf("expected no drawable content when layers have zero pixels")
	}

	// Case 4: map with a floor layer with pixels
	m3 := &ValetudoMap{Layers: []MapLayer{{Type: "floor", Pixels: []int{1, 2}}}}
	r = &CompositeRenderer{Maps: map[string]*ValetudoMap{"vac3": m3}}
	if !r.HasDrawableContent() {
		t.Fatalf("expected drawable content when a layer contains pixels")
	}
}
