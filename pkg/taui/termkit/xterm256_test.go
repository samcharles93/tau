package termkit

import "testing"

func TestXterm256(t *testing.T) {
	cases := []struct {
		idx  uint8
		want Color
	}{
		{0, Color{0, 0, 0}},
		{15, Color{255, 255, 255}},
		{16, Color{0, 0, 0}},
		{209, Color{255, 135, 95}}, // Shell mode accent
		{134, Color{175, 95, 215}}, // Planning mode accent
		{215, Color{255, 175, 95}}, // distinct from 209 - regression guard
		{231, Color{255, 255, 255}},
		{232, Color{8, 8, 8}},
		{255, Color{238, 238, 238}},
	}
	for _, tc := range cases {
		if got := Xterm256(tc.idx); got != tc.want {
			t.Errorf("Xterm256(%d) = %v, want %v", tc.idx, got, tc.want)
		}
	}
}
