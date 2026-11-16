package classify

import "testing"

func TestCleanText(t *testing.T) {
	for in, want := range map[string]string{
		"Beach Volleyball!":     "beach volleyball",
		"  Hong   Kong, China ": "hong kong china",
		"sea_waves":             "sea waves",
		"$5 + 3 = <8>":          "5 3 8",
		"don't (stop)":          "dont stop",
		"東京都":                   "東京都",
		"Café, naïve":           "café naïve",
		"":                      "",
	} {
		if got := cleanText(in); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
}
