package label

import "testing"

func TestMarkerAndText(t *testing.T) {
	cases := []struct{ title, marker, text string }{
		{"🟨 PROJECT1", "🟨", "PROJECT1"},
		{"🟨🟨🟨⚡⚡PROJECT1", "🟨🟨🟨⚡⚡", "PROJECT1"},
		{"🔵 ritza/bryntum", "🔵", "ritza/bryntum"},
		{"BRYNTUM", "", "BRYNTUM"},
		{"", "", ""},
		{"✳ Review GitHub PR #384", "✳", "Review GitHub PR #384"},
		// An emoji inside the text is part of the text, not a marker.
		{"deploy 🚀 now", "", "deploy 🚀 now"},
	}
	for _, tc := range cases {
		if got := Marker(tc.title); got != tc.marker {
			t.Errorf("Marker(%q) = %q, want %q", tc.title, got, tc.marker)
		}
		if got := Text(tc.title); got != tc.text {
			t.Errorf("Text(%q) = %q, want %q", tc.title, got, tc.text)
		}
	}
}

func TestColor(t *testing.T) {
	cases := map[string]string{
		"🟨 PROJECT1":    "#f7f3de",
		"🟦 BRYNTUM":     "#dee7f7",
		"🔴 broken":      "#f7dede",
		"⚡🟩 deploying":  "#def7e2", // the first *recognised* marker wins
		"🟨🟨🟨⚡⚡PROJECT1": "#f7f3de",
	}
	for title, want := range cases {
		got, ok := Color(title)
		if !ok || got != want {
			t.Errorf("Color(%q) = %q, %v; want %q, true", title, got, ok, want)
		}
	}
	for _, title := range []string{"BRYNTUM", "", "deploy 🚀 now", "⚡ building"} {
		if got, ok := Color(title); ok {
			t.Errorf("Color(%q) = %q, true; want no colour", title, got)
		}
	}
}
