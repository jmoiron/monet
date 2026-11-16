// Package classify is a bench for trying out ways of labelling photos so that
// they can be searched ("pizza" finds the photos that have pizza in them),
// without the photos leaving the machine.
//
// There are two shapes of output a model can give, and a backend gives either
// or both in a Result: words (a caption, labels) which are searched like text,
// and an embedding, which is searched by embedding the query with the same
// model and finding the nearest photos. See research.md for the tradeoffs.
//
// Backends are deliberately thin. The ones here talk to something that is
// already good at running models (ollama, or any program that speaks the
// worker protocol), so that trying a new model is a flag, not a new Go
// dependency. A Run of a backend over a directory of photos is saved as one
// JSONL file, and an Index over such a file can be searched, so that several
// backends can be compared on the same photos and queries.
package classify

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"

	xdraw "golang.org/x/image/draw"

	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// A Label is one thing a model says is in a photo.
type Label struct {
	Name  string  `json:"name"`
	Score float64 `json:"score,omitempty"` // whatever the model's scale is; 0 when it has none
}

// A Result is what a backend says about one photo.
type Result struct {
	Labels    []Label   `json:"labels,omitempty"`
	Caption   string    `json:"caption,omitempty"`
	Embedding []float32 `json:"embedding,omitempty"`
}

// A Classifier labels photos.
type Classifier interface {
	// Name says which model and settings made a result, for the record.
	Name() string
	// Classify looks at the image file at path.
	Classify(ctx context.Context, path string) (*Result, error)
	Close() error
}

// A TextEmbedder is a Classifier whose embeddings are of a shared space with
// text, so that a query can be compared with the photos' embeddings.
type TextEmbedder interface {
	EmbedText(ctx context.Context, text string) ([]float32, error)
}

// Prepare reads an image file and returns it as a JPEG whose long edge is at
// most maxEdge (0 for as it is). Models want smaller pictures than cameras
// make, and the time they take depends on it. Orientation is not applied: run
// over the gallery's own thumbnails or web sized copies, which already are.
func Prepare(path string, maxEdge int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	img = shrink(img, maxEdge)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func shrink(img image.Image, edge int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if edge <= 0 || max(w, h) <= edge {
		return img
	}
	if w >= h {
		h = max(1, h*edge/w)
		w = edge
	} else {
		w = max(1, w*edge/h)
		h = edge
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	return dst
}
