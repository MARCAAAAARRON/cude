package tokens

import "testing"

func TestEstimateBasic(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"hi", 1},
		{"hello world", 2},
		{"the quick brown fox", 4},
		{"一二三", 3},
		{"aaaaaaaaaa", 2},
	}
	for _, c := range cases {
		if got := Estimate(c.in); got != c.want {
			t.Errorf("Estimate(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestEstimateBounds(t *testing.T) {
	for _, s := range []string{"x", "ab", "abc"} {
		if got := Estimate(s); got < 1 {
			t.Errorf("want >=1 for %q, got %d", s, got)
		}
	}
}

func TestPrintableRatio(t *testing.T) {
	if printableASCIIRatio("hello") != 1.0 {
		t.Fatal("pure ascii should be 1.0")
	}
}
