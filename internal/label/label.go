// Package label reads meaning out of a terminal tab's title.
package label

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Ghostty has no per-tab colour, so the common workaround is to put a coloured
// square or circle in the tab title. That emoji is visible in the tab bar, it
// survives every rename, and unlike the real background colour it can actually
// be read from outside the terminal — which makes it the one honest source for
// what colour a tab "is".

// Colors maps the emoji people use as tab markers onto the pastel background a
// tile is drawn with. Both the square and circle sets are accepted because both
// are in common use.
var Colors = map[rune]string{
	'🟥': "#f7dede", '🔴': "#f7dede",
	'🟧': "#f7ebde", '🟠': "#f7ebde",
	'🟨': "#f7f3de", '🟡': "#f7f3de",
	'🟩': "#def7e2", '🟢': "#def7e2",
	'🟦': "#dee7f7", '🔵': "#dee7f7",
	'🟪': "#e7def7", '🟣': "#e7def7",
	'🟫': "#efe4d8", '🟤': "#efe4d8",
	'⬛': "#cccccc", '⚫': "#cccccc",
	'⬜': "#fafafa", '⚪': "#fafafa",
	'🩷': "#f7deeb", '💗': "#f7deeb",
	'💠': "#def3f7", '🔷': "#def3f7",
}

// Names lets a colour be written out instead of pasted, for anyone who would
// rather not type an emoji into a flag.
var Names = map[string]rune{
	"red": '🟥', "orange": '🟧', "yellow": '🟨', "green": '🟩',
	"blue": '🟦', "purple": '🟪', "brown": '🟫', "black": '⬛',
	"white": '⬜', "pink": '🩷', "cyan": '💠', "teal": '💠',
}

// Color returns the colour marked by the first recognised emoji in a title.
// Only the leading run is considered: an emoji in the middle of a sentence is
// part of the text, not a marker.
func Color(title string) (string, bool) {
	for _, r := range Marker(title) {
		if hex, ok := Colors[r]; ok {
			return hex, true
		}
	}
	return "", false
}

// Marker returns the run of emoji and symbols a title starts with, empty if it
// starts with ordinary text.
func Marker(title string) string {
	end := 0
	for i, r := range title {
		if isMarker(r) || (end > 0 && r == ' ') {
			end = i + len(string(r))
			continue
		}
		break
	}
	return strings.TrimSpace(title[:end])
}

// Text returns a title with its leading emoji removed.
func Text(title string) string {
	return strings.TrimSpace(strings.TrimPrefix(title, Marker(title)))
}

func isMarker(r rune) bool {
	switch {
	case r == 0xFE0F, r == 0xFE0E, r == 0x200D: // variation selectors, ZWJ
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // skin tones
		return true
	case unicode.Is(unicode.So, r): // symbol, other: where the emoji live
		return true
	}
	return false
}

// ParseMarker turns a colour name, or an emoji pasted straight in, into the marker
// to put at the front of a tab title.
func ParseMarker(s string) (string, error) {
	s = strings.TrimSpace(s)
	if r, ok := Names[strings.ToLower(s)]; ok {
		return string(r), nil
	}
	if m := Marker(s); m != "" && m == s {
		return s, nil
	}
	names := make([]string, 0, len(Names))
	for n := range Names {
		names = append(names, n)
	}
	sort.Strings(names)
	return "", fmt.Errorf("%q is not a colour: use one of %s, or paste an emoji",
		s, strings.Join(names, ", "))
}
