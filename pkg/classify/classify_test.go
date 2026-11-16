package classify

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// a worker for the tests: this test binary, run again, answering as a worker
func TestMain(m *testing.M) {
	if os.Getenv("CLASSIFY_TEST_WORKER") == "1" {
		fakeWorker()
		return
	}
	os.Exit(m.Run())
}

func fakeWorker() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var req workerRequest
		if dec.Decode(&req) != nil {
			return
		}
		switch {
		case req.Op == "text" && req.Text == "pizza":
			enc.Encode(map[string]any{"embedding": []float32{1, 0}})
		case req.Op == "text":
			enc.Encode(map[string]any{"embedding": []float32{0, 1}})
		case filepath.Base(req.Path) == "bad.jpg":
			enc.Encode(map[string]any{"error": "no good"})
		case filepath.Base(req.Path) == "pizza.jpg":
			enc.Encode(map[string]any{"embedding": []float32{0.9, 0.1}, "labels": []Label{{"pizza", 0.9}}})
		default:
			enc.Encode(map[string]any{"embedding": []float32{0.1, 0.9}})
		}
	}
}

func writeJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, jpeg.Encode(f, img, nil))
}

func TestPrepareShrinks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jpg")
	writeJPEG(t, p, 400, 200)
	data, err := Prepare(p, 100)
	require.NoError(t, err)
	img, _, err := image.Decode(bytesReader(data))
	require.NoError(t, err)
	assert.Equal(t, 100, img.Bounds().Dx())
	assert.Equal(t, 50, img.Bounds().Dy())
}

func TestOllama(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/chat", r.URL.Path)
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{
			"content": `{"caption": "A pizza on a table.", "tags": ["Pizza", "table", "pizza", " "]}`}})
	}))
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "a.jpg")
	writeJPEG(t, p, 64, 64)

	o := &OllamaVLM{Host: srv.URL, Model: "qwen2.5vl:7b"}
	res, err := o.Classify(context.Background(), p)
	require.NoError(t, err)
	assert.Equal(t, "A pizza on a table.", res.Caption)
	assert.Equal(t, []Label{{Name: "pizza"}, {Name: "table"}}, res.Labels, "lowercased, and without repeats or blanks")
	assert.Equal(t, "qwen2.5vl:7b", got["model"])
	assert.Len(t, got["messages"].([]any)[0].(map[string]any)["images"], 1)
}

func TestOllamaErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `model "nope" not found`, http.StatusNotFound)
	}))
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "a.jpg")
	writeJPEG(t, p, 8, 8)
	_, err := (&OllamaVLM{Host: srv.URL, Model: "nope"}).Classify(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	_, err = parseTagged("I think it is a pizza")
	assert.Error(t, err)
}

func TestWorkerRunAndSearch(t *testing.T) {
	t.Setenv("CLASSIFY_TEST_WORKER", "1")
	dir := t.TempDir()
	for _, n := range []string{"pizza.jpg", "beach.jpg", "bad.jpg"} {
		writeJPEG(t, filepath.Join(dir, n), 8, 8)
	}
	files, err := ImageFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 3)

	exe, _ := os.Executable()
	w, err := StartWorker("fake", exe)
	require.NoError(t, err)
	defer w.Close()

	results := filepath.Join(t.TempDir(), "out", "fake.jsonl")
	require.NoError(t, Run(context.Background(), w, files, results, RunOptions{}))
	recs, err := ReadRecords(results)
	require.NoError(t, err)
	require.Len(t, recs, 3)
	bad := 0
	for _, r := range recs {
		if r.Error != "" {
			bad++
			assert.Equal(t, "no good", r.Error)
		}
	}
	assert.Equal(t, 1, bad)

	// a second run does only what failed
	calls := 0
	require.NoError(t, Run(context.Background(), w, files, results, RunOptions{
		Progress: func(done, total int, r Record) { calls++; assert.Equal(t, 1, total) }}))
	assert.Equal(t, 1, calls)

	ix, err := LoadIndex(results)
	require.NoError(t, err)
	assert.Equal(t, 2, ix.Len(), "the failure isn't searchable")
	assert.True(t, ix.HasEmbeddings())

	hits, err := ix.Vector(context.Background(), w, "pizza", 5)
	require.NoError(t, err)
	require.Len(t, hits, 2)
	assert.Equal(t, "pizza.jpg", filepath.Base(hits[0].Path))
	hits, _ = ix.Vector(context.Background(), w, "beach", 1)
	assert.Equal(t, "beach.jpg", filepath.Base(hits[0].Path))
	assert.Len(t, hits, 1)
}

func TestLexical(t *testing.T) {
	ix := &Index{}
	add := func(path, caption string, labels ...string) {
		r := &Result{Caption: caption}
		for _, l := range labels {
			r.Labels = append(r.Labels, Label{Name: l})
		}
		ix.recs = append(ix.recs, Record{Path: path, Result: r})
		ix.norm = append(ix.norm, nil)
	}
	add("a", "A table with food.", "pizza", "table")
	add("b", "A woman eating pizza at a cafe.", "cafe", "woman")
	add("c", "A beach.", "sea", "sand", "red wine")
	add("d", "Rows of pizzas.", "bakery")

	hits := ix.Lexical("pizza", 0)
	var paths []string
	for _, h := range hits {
		paths = append(paths, h.Path)
	}
	assert.Equal(t, []string{"a", "b", "d"}, paths, "a label beats a caption; plurals are the same word")

	assert.Empty(t, ix.Lexical("pizza beach", 0), "every word has to be there")
	paths = nil
	for _, h := range ix.Lexical("red wine", 0) {
		paths = append(paths, h.Path)
	}
	assert.Equal(t, []string{"c"}, paths)
	assert.Len(t, ix.Lexical("pizza", 2), 2)
	assert.Empty(t, ix.Lexical("  ", 0))
}

func TestSpec(t *testing.T) {
	assert.Equal(t, "ollama_qwen2.5vl_7b", Spec("ollama:qwen2.5vl:7b").Name())
	assert.Equal(t, "siglip", Spec("siglip=worker:python3 w.py --model x").Name())
	c, err := Spec("ollama:gemma3:4b@box:11434").Open()
	require.NoError(t, err)
	o := c.(*OllamaVLM)
	assert.Equal(t, "gemma3:4b", o.Model)
	assert.Equal(t, "http://box:11434", o.Host)
	_, err = Spec("carrier-pigeon:x").Open()
	assert.Error(t, err)
}
