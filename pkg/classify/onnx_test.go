//go:build ORT

package classify

import (
	"context"
	"math"
	"os"
	"testing"
)

// ONNX_MODEL is a directory made by scripts/export_text_onnx.py, and
// ONNX_LIB the one with libonnxruntime.so (if not /usr/lib).
func TestONNXEmbedder(t *testing.T) {
	dir := os.Getenv("ONNX_MODEL")
	if dir == "" {
		t.Skip("ONNX_MODEL is not set")
	}
	c, err := Spec("t=onnx:" + dir + "@" + os.Getenv("ONNX_LIB")).Open()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	te := c.(TextEmbedder)
	a, err := te.EmbedText(context.Background(), "Beach Volleyball!")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := te.EmbedText(context.Background(), "beach volleyball")
	if len(a) != 768 {
		t.Fatalf("%d dimensions", len(a))
	}
	var dot, norm float64
	for i := range a {
		dot += float64(a[i] * b[i])
		norm += float64(a[i] * a[i])
	}
	if math.Abs(norm-1) > 1e-3 || dot < 0.9999 {
		t.Errorf("norm %v, cos of the same query written two ways %v", norm, dot)
	}
}
