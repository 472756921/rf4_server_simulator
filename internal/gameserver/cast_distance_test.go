package gameserver

import (
	"math"
	"testing"
)

func TestCastBaseDistanceProfiles(t *testing.T) {
	cases := []struct {
		name    string
		rigType int64
		want    float32
	}{
		{name: "float", rigType: 1, want: 7.5461488},
		{name: "bottom", rigType: 12, want: 7.5461488},
		{name: "lure", rigType: 5, want: 7.1598907},
	}
	for _, tc := range cases {
		got := math.Float32frombits(castBaseDistanceBits(tc.rigType))
		if math.Abs(float64(got-tc.want)) > 1e-6 {
			t.Fatalf("%s: got %.7f want %.7f", tc.name, got, tc.want)
		}
	}
}
