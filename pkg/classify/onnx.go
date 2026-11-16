//go:build ORT

package classify

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/options"
	"github.com/knights-analytics/hugot/pipelines"
)

// An ONNXEmbedder embeds text with an ONNX export of a model's text tower (see
// scripts/export_text_onnx.py), through hugot and ONNX Runtime: no Python, and
// no GPU. It only embeds text, as the embeddings of the photos are made by the
// model's own worker when they are classified.
type ONNXEmbedder struct {
	name     string
	mu       sync.Mutex
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
}

// openONNX loads the model in dir. libDir is the directory with
// libonnxruntime.so; "" for hugot's default, /usr/lib.
func openONNX(name, dir, libDir string) (Classifier, error) {
	if dir == "" {
		return nil, errors.New("onnx: no model directory")
	}
	var opts []options.WithOption
	if libDir != "" {
		opts = append(opts, options.WithOnnxLibraryPath(libDir))
	}
	// its memory arena only holds on to what one query needed
	opts = append(opts, options.WithCPUMemArena(false), options.WithMemPattern(false))
	ctx := context.Background()
	session, err := hugot.NewORTSession(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("onnx runtime: %w", err)
	}
	p, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
		ModelPath: dir,
		Name:      name,
		Options:   []hugot.FeatureExtractionOption{pipelines.WithOutputName("embedding")},
	})
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("onnx model %s: %w", dir, err)
	}
	return &ONNXEmbedder{name: name, session: session, pipeline: p}, nil
}

func (e *ONNXEmbedder) Name() string { return e.name }

func (e *ONNXEmbedder) Classify(ctx context.Context, path string) (*Result, error) {
	return nil, errors.New("onnx: only embeds text")
}

// EmbedText is the normalized embedding of a query.
func (e *ONNXEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out, err := e.pipeline.RunRaw(ctx, []string{cleanText(text)})
	if err != nil {
		return nil, err
	}
	if len(out.Embeddings) != 1 {
		return nil, fmt.Errorf("onnx: %d embeddings for one text", len(out.Embeddings))
	}
	return out.Embeddings[0], nil
}

func (e *ONNXEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.session.Destroy()
}
