package classify

import (
	"math"
	"math/rand"
	"testing"
)

func TestEmbeddingRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(rng.NormFloat64() * 0.05)
	}
	v[0], v[1], v[2] = 0, 1e-7, -0.5 // zero, a subnormal half, and a plain one
	got, err := DecodeEmbedding(EncodeEmbedding(v))
	if err != nil || len(got) != len(v) {
		t.Fatalf("decode: %v, %d values", err, len(got))
	}
	for i := range v {
		if d := math.Abs(float64(got[i] - v[i])); d > 1e-3*math.Abs(float64(v[i]))+1e-7 {
			t.Fatalf("value %d: %v became %v", i, v[i], got[i])
		}
	}
	// what matters is that the nearness of vectors survives
	a, b := normalized(v), normalized(got)
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	if dot < 0.99999 {
		t.Errorf("a vector and its round trip have cosine %v", dot)
	}
}

func TestFloat16Exact(t *testing.T) {
	for _, x := range []float32{0, 1, -1, 0.5, 2, 65504, 6.1035156e-05, -0.0999755859375} {
		if got := float32From16(float16(x)); got != x {
			t.Errorf("%v became %v", x, got)
		}
	}
	if got := float32From16(float16(1e9)); !math.IsInf(float64(got), 1) {
		t.Errorf("1e9 became %v, not +Inf", got)
	}
	if _, err := DecodeHalves([]byte{1, 2, 3}); err == nil {
		t.Error("an odd number of bytes decoded")
	}
}
