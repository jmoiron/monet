package gallery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jmoiron/monet/pkg/classify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEmbedder embeds a query as the vector it is told to.
type fakeEmbedder map[string][]float32

func (f fakeEmbedder) EmbedText(_ context.Context, text string) ([]float32, error) {
	if v, ok := f[text]; ok {
		return v, nil
	}
	return []float32{0, 0, 0, 1}, nil
}

// addPhoto adds a distinct image, published or not.
func addPhoto(t *testing.T, svc *PhotoService, n int, published bool) *Photo {
	t.Helper()
	p, err := svc.Add(NewPhoto{Source: SourceUpload, Data: testJPEG(t, 20+n, 20), Published: published})
	require.NoError(t, err)
	return p
}

func item(p *Photo, model, caption string, labels []string, emb []float32) classify.Item {
	it := classify.Item{ID: p.ID, SHA256: p.SHA256, Model: model, Caption: caption}
	for _, l := range labels {
		it.Labels = append(it.Labels, classify.Label{Name: l})
	}
	if emb != nil {
		it.Embedding = classify.EncodeEmbedding(emb)
	}
	return it
}

func TestImportClassifications(t *testing.T) {
	svc := newTestService(t)
	cs := NewClassService(svc.db)
	p := addPhoto(t, svc, 0, true)

	rep, err := cs.Import([]classify.Item{
		item(p, "gemma", "A halved tomato", []string{"tomato", "fruit"}, nil),
		{ID: "nosuchphoto", Model: "gemma", Caption: "x"},                    // unknown
		{ID: p.ID, SHA256: "0000", Model: "gemma", Caption: "another image"}, // not this image
		{ID: p.ID, Model: "gemma"},                                           // says nothing
		{ID: p.ID, Model: "siglip", Embedding: "not base64!"},                // unreadable
	})
	require.NoError(t, err)
	assert.Equal(t, ClassifyReport{Stored: 1, Unknown: 1, Mismatch: 1, Invalid: 2}, rep)

	// importing again replaces, and a second model sits beside the first
	rep, err = cs.Import([]classify.Item{
		item(p, "gemma", "A tomato, cut in half", []string{"tomato"}, nil),
		item(p, "siglip", "", nil, []float32{1, 0, 0, 0}),
	})
	require.NoError(t, err)
	assert.Equal(t, 2, rep.Stored)
	got, err := cs.For(p.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "gemma", got[0].Model)
	assert.Equal(t, "A tomato, cut in half", got[0].Caption)
	assert.Equal(t, []string{"tomato"}, got[0].LabelNames())
	assert.True(t, got[1].HasEmbedding())

	ms, err := cs.Models()
	require.NoError(t, err)
	assert.Equal(t, []ModelCount{{"gemma", 1, 0}, {"siglip", 1, 1}}, ms)

	// deleting a photo takes what was seen in it
	require.NoError(t, svc.Delete(p.ID))
	got, _ = cs.For(p.ID)
	assert.Empty(t, got)
	var n int
	require.NoError(t, svc.db.Get(&n, `SELECT count(*) FROM photo_class_fts`))
	assert.Zero(t, n)
}

func TestSearchWordsAndVectors(t *testing.T) {
	svc := newTestService(t)
	cs := NewClassService(svc.db)
	tomato := addPhoto(t, svc, 0, true)
	band := addPhoto(t, svc, 1, true)
	hidden := addPhoto(t, svc, 2, false) // a tomato, but not published

	_, err := cs.Import([]classify.Item{
		item(tomato, "gemma", "A hand holds a sliced red tomato", []string{"tomato", "fruit"}, nil),
		item(band, "gemma", "A band plays on a stage", []string{"band", "stage", "guitar"}, nil),
		item(hidden, "gemma", "tomatoes on a table", []string{"tomato"}, nil),
		item(tomato, "siglip", "", nil, []float32{1, 0, 0, 0}),
		item(band, "siglip", "", nil, []float32{0, 1, 0, 0}),
		item(hidden, "siglip", "", nil, []float32{0.9, 0.1, 0, 0}),
	})
	require.NoError(t, err)

	s := NewSearcher(svc.db, SearchConfig{})
	s.embModel = "siglip"
	s.embedder = fakeEmbedder{"tomato": {1, 0, 0, 0}, "red fruit": {0.95, 0.05, 0, 0}}
	ctx := context.Background()
	ids := func(r *SearchResult) []string {
		var out []string
		for _, m := range r.Matches {
			out = append(out, m.ID)
		}
		return out
	}

	// a word found both ways comes first, and says so; the plural is the same word
	r, err := s.Search(ctx, "tomatoes", SearchOptions{PublishedOnly: true})
	require.NoError(t, err)
	require.Equal(t, []string{tomato.ID}, ids(r))
	m := r.Matches[0]
	assert.Equal(t, 1, m.Why.WordRank)
	require.Len(t, m.Why.Words, 1)
	assert.Contains(t, m.Why.Words[0].Caption, "[tomato]")
	assert.Contains(t, m.Why.Words[0].Labels, "[tomato]")

	// the admin sees what isn't published too
	r, err = s.Search(ctx, "tomato", SearchOptions{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{tomato.ID, hidden.ID}, ids(r))
	for _, m := range r.Matches {
		if m.ID != tomato.ID {
			continue
		}
		require.NotNil(t, m.Why.Vector)
		assert.InDelta(t, 1.0, m.Why.Vector.Cosine, 1e-3)
		assert.Equal(t, 1, m.Why.VectorRank)
		assert.Contains(t, m.Why.Summary(), "vector #1")
	}

	// words that aren't anywhere in the captions are found by their embeddings
	r, err = s.Search(ctx, "red fruit", SearchOptions{PublishedOnly: true})
	require.NoError(t, err)
	require.Equal(t, []string{tomato.ID}, ids(r)) // "red fruit" is in the caption too, but so is the vector
	assert.NotNil(t, r.Matches[0].Why.Vector)

	// a query that is near nothing, and in no words, finds nothing
	r, err = s.Search(ctx, "dinosaur", SearchOptions{PublishedOnly: true})
	require.NoError(t, err)
	assert.Empty(t, r.Matches)

	// punctuation is not a query, and an fts operator is just a word
	r, err = s.Search(ctx, `"" OR -`, SearchOptions{})
	require.NoError(t, err)
	assert.Empty(t, r.Matches)

	// with no embedder it is the words alone
	w := NewSearcher(svc.db, SearchConfig{})
	r, err = w.Search(ctx, "guitar", SearchOptions{PublishedOnly: true})
	require.NoError(t, err)
	assert.Equal(t, []string{band.ID}, ids(r))
	assert.Nil(t, r.Matches[0].Why.Vector)
}

func TestSearchReloadsEmbeddingsAfterAnImport(t *testing.T) {
	svc := newTestService(t)
	cs := NewClassService(svc.db)
	a, b := addPhoto(t, svc, 0, true), addPhoto(t, svc, 1, true)
	s := NewSearcher(svc.db, SearchConfig{})
	s.embModel = "siglip"
	s.embedder = fakeEmbedder{"x": {1, 0, 0, 0}}

	_, err := cs.Import([]classify.Item{item(a, "siglip", "", nil, []float32{1, 0, 0, 0})})
	require.NoError(t, err)
	r, err := s.Search(context.Background(), "x", SearchOptions{})
	require.NoError(t, err)
	require.Len(t, r.Matches, 1)

	_, err = cs.Import([]classify.Item{item(b, "siglip", "", nil, []float32{1, 0, 0, 0})})
	require.NoError(t, err)
	r, err = s.Search(context.Background(), "x", SearchOptions{})
	require.NoError(t, err)
	assert.Len(t, r.Matches, 2)
}

// a failing embedder is dropped when it fails, and another is not started until it is time
type flakyEmbedder struct{ err error }

func (f *flakyEmbedder) EmbedText(context.Context, string) ([]float32, error) { return nil, f.err }

func TestSearchDropsAFailedEmbedder(t *testing.T) {
	svc := newTestService(t)
	cs := NewClassService(svc.db)
	p := addPhoto(t, svc, 0, true)
	_, err := cs.Import([]classify.Item{item(p, "siglip", "a tomato", nil, []float32{1, 0, 0, 0})})
	require.NoError(t, err)

	s := NewSearcher(svc.db, SearchConfig{Embedder: "siglip=worker:/no/such/program"})
	s.embedder = &flakyEmbedder{errors.New("EOF")}
	r, err := s.Search(context.Background(), "tomato", SearchOptions{})
	require.NoError(t, err)
	require.Len(t, r.Matches, 1) // the words still find it
	require.Len(t, r.Notes, 1)
	assert.Contains(t, r.Notes[0], "EOF")

	// the next search is within the retry time, and says why it isn't trying
	r, err = s.Search(context.Background(), "tomato", SearchOptions{})
	require.NoError(t, err)
	require.Len(t, r.Notes, 1)
	assert.Contains(t, r.Notes[0], "EOF")
	assert.Len(t, r.Matches, 1)

	// once it is time it tries again, which here can't work: there is no such program
	s.mu.Lock()
	s.embRetry = time.Now().Add(-time.Second)
	s.mu.Unlock()
	_, note := s.vector(context.Background(), "tomato", SearchOptions{})
	assert.Contains(t, note, "vector search is off")
	assert.NotContains(t, note, "EOF")
}

// a signalling embedder says when it is asked, as a worker is when its model is in
type askedEmbedder chan string

func (a askedEmbedder) EmbedText(_ context.Context, text string) ([]float32, error) {
	a <- text
	return []float32{1, 0, 0, 0}, nil
}

func TestWarmHasTheEmbedderEmbed(t *testing.T) {
	svc := newTestService(t)
	asked := make(askedEmbedder, 1)
	s := NewSearcher(svc.db, SearchConfig{})
	s.embedder = asked
	s.Warm()
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("warming didn't ask the embedder for anything")
	}
}

type closingEmbedder struct {
	fakeEmbedder
	closed chan struct{}
}

func (c closingEmbedder) Close() error { close(c.closed); return nil }

// an embedder nobody has wanted for Idle is let go, and one that is wanted is kept
func TestIdleEmbedderIsUnloaded(t *testing.T) {
	svc := newTestService(t)
	s := NewSearcher(svc.db, SearchConfig{Idle: 80 * time.Millisecond})
	e := closingEmbedder{fakeEmbedder{"x": {1, 0, 0, 0}}, make(chan struct{})}
	s.embedder = e
	for i := 0; i < 4; i++ { // wanted for longer than Idle, in steps shorter than it
		time.Sleep(40 * time.Millisecond)
		got, _ := s.getEmbedder()
		assert.NotNil(t, got)
	}
	select {
	case <-e.closed:
		t.Fatal("unloaded while it was being used")
	default:
	}
	select {
	case <-e.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("not unloaded when idle")
	}
	got, _ := s.getEmbedder()
	assert.Nil(t, got) // no spec to start another from: the test's embedder was set by hand
}
