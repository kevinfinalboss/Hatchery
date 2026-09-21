package v1alpha1

import "testing"

func TestResolveImage(t *testing.T) {
	egg := &Egg{Spec: EggSpec{Images: []EggImage{
		{Name: "Java 25", Image: "img:25"},
		{Name: "Java 17", Image: "img:17"},
	}}}
	cases := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"", "img:25", true}, // empty picks the first (the default)
		{"Java 17", "img:17", true},
		{"Java 99", "", false},
	}
	for _, c := range cases {
		got, ok := egg.ResolveImage(c.name)
		if got != c.want || ok != c.wantOK {
			t.Errorf("ResolveImage(%q) = %q, %v; want %q, %v", c.name, got, ok, c.want, c.wantOK)
		}
	}
	if _, ok := (&Egg{}).ResolveImage(""); ok {
		t.Error("an Egg with no images has nothing to resolve")
	}
}
