package gallery

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/pkg/classify"
)

// SearchConfig says how photos are searched by what is in them.
type SearchConfig struct {
	// Embedder is a classify spec (a worker, eg. "siglip2=worker:python3 ...")
	// that embeds the queries, so that they can be compared with the photos'
	// embeddings. It has to be the model that made them, and its name is the
	// model whose embeddings are searched. Without one only the words (captions
	// and labels) are searched.
	Embedder string

	// MinVector is the least cosine a photo's embedding may have with the
	// query's to be a match: the vector search always finds the nearest photos,
	// even when none are near.
	MinVector float64

	// Idle is how long the embedder is kept after it was last wanted; 0 keeps
	// it for good. A model is memory a server may not have to spare for what is
	// rarely searched, and loading one takes about a second (see Warm).
	Idle time.Duration
}

// DefaultMinVector suits SigLIP 2, whose cosines run around 0.13 for a good
// match and 0.04 for a bad one. Looking at searches on a few hundred photos,
// below 0.09 a photo with no word match was nearly always unrelated (a beach
// volleyball for "tennis", skylines for "hong kong"); a few related photos are
// lost to keep two or three times as many unrelated ones out.
const DefaultMinVector = 0.09

// how long after the embedder fails before it is started again
const embedderRetry = 30 * time.Second

// placeBoost is added to the score of a photo whose place matched the query.
// Where a photo was taken is known, not guessed, and someone searching for a
// place wants the photos from it, so it is more than the best that a photo can
// get from words and vectors together (2/61), and a photo taken in Tokyo comes
// before one that only looks as Tokyo might.
const placeBoost = 0.05

// rrfK damps how much being first counts, in reciprocal rank fusion
const rrfK = 60

// A Searcher finds photos by what the models saw in them: words, and nearness
// of embeddings, fused. Every match says why.
type Searcher struct {
	db  db.DB
	cfg SearchConfig

	mu       sync.Mutex
	embedder classify.TextEmbedder // from cfg, opened on first need; settable by tests
	embModel string
	embErr   error     // why it isn't running, until embRetry
	embRetry time.Time // when to try starting it again
	ready    bool      // the embedder has embedded something: its model is in
	lastUse  time.Time // of the embedder
	idle     *time.Timer
	vecs     map[string]*vecIndex
}

// NewSearcher makes a searcher; the embedder, if any, is not started until a search needs it.
func NewSearcher(d db.DB, cfg SearchConfig) *Searcher {
	if cfg.MinVector == 0 {
		cfg.MinVector = DefaultMinVector
	}
	s := &Searcher{db: d, cfg: cfg, vecs: map[string]*vecIndex{}}
	if cfg.Embedder != "" {
		s.embModel = classify.Spec(cfg.Embedder).Name()
	}
	return s
}

// A Match is a photo that matches a query, and why.
type Match struct {
	ID    string
	Score float64 // the fused score: only means anything next to the others'
	Why   Why
}

// Why is how a match came to be.
type Why struct {
	Words      []WordWhy  // where the query's words are, per model that has some
	WordRank   int        // 1 is best; 0 when the words did not match
	Vector     *VectorWhy // nil when there is no vector match
	VectorRank int
	Place      bool // the query matched where the photo was taken, which counts for more
}

// A WordWhy is a model's words that match, with the matching ones in [brackets].
// The model "place" is where the photo was taken.
type WordWhy struct {
	Model   string
	Caption string
	Labels  string
	Place   string
	Rank    float64 // bm25: lower is better, and negative
}

// Text is what matched, as one line: the caption, the labels and the place,
// whichever there are.
func (w WordWhy) Text() string {
	var parts []string
	for _, s := range []string{w.Caption, w.Labels, w.Place} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// A VectorWhy says how near a photo's embedding is to the query's.
type VectorWhy struct {
	Model  string
	Cosine float64
}

// Summary is Why in a line.
func (w Why) Summary() string {
	var parts []string
	if w.WordRank > 0 {
		parts = append(parts, fmt.Sprintf("words #%d", w.WordRank))
	}
	if w.Vector != nil {
		parts = append(parts, fmt.Sprintf("vector #%d, cosine %.3f", w.VectorRank, w.Vector.Cosine))
	}
	if w.Place {
		parts = append(parts, "place")
	}
	return strings.Join(parts, " · ")
}

// A SearchResult is the matches, best first, and anything the search could not do.
type SearchResult struct {
	Matches []Match
	Notes   []string
}

// SearchOptions narrows a search.
type SearchOptions struct {
	PublishedOnly bool
	Limit         int // 0 for 200
}

var queryWord = regexp.MustCompile(`[\p{L}\p{N}]+`)

// ftsQuery is the words of a query as an fts5 query that needs all of them,
// quoted so that no word is an operator.
func ftsQuery(q string) string {
	var parts []string
	for _, w := range queryWord.FindAllString(q, -1) {
		// a word is a whole word of what a model said, or the start of a word of
		// where the photo was: "tokyo" finds "tokyoto", and "東京" finds "東京都"
		w = strings.ToLower(w)
		parts = append(parts, `("`+w+`" OR place : "`+w+`" *)`)
	}
	return strings.Join(parts, " AND ")
}

// Search finds the photos for a query.
func (s *Searcher) Search(ctx context.Context, query string, o SearchOptions) (*SearchResult, error) {
	if o.Limit <= 0 {
		o.Limit = 200
	}
	res := &SearchResult{}
	if ftsQuery(query) == "" {
		return res, nil
	}
	words, err := s.words(query, o)
	if err != nil {
		return nil, err
	}
	vector, note := s.vector(ctx, query, o)
	if note != "" {
		res.Notes = append(res.Notes, note)
	}

	byID := map[string]*Match{}
	get := func(id string) *Match {
		m := byID[id]
		if m == nil {
			m = &Match{ID: id}
			byID[id] = m
		}
		return m
	}
	for i, w := range words {
		m := get(w.id)
		m.Why.Words, m.Why.WordRank = w.why, i+1
		m.Score += 1 / float64(rrfK+i+1)
	}
	for i, v := range vector {
		m := get(v.id)
		m.Why.Vector, m.Why.VectorRank = &VectorWhy{Model: s.embModel, Cosine: v.cos}, i+1
		m.Score += 1 / float64(rrfK+i+1)
	}
	for _, m := range byID {
		for _, w := range m.Why.Words {
			if strings.Contains(w.Place, "[") {
				m.Why.Place = true
				m.Score += placeBoost
				break
			}
		}
		res.Matches = append(res.Matches, *m)
	}
	sort.Slice(res.Matches, func(i, j int) bool {
		a, b := res.Matches[i], res.Matches[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.ID < b.ID
	})
	if len(res.Matches) > o.Limit {
		res.Matches = res.Matches[:o.Limit]
	}
	return res, nil
}

type wordHit struct {
	id  string
	why []WordWhy
}

// words finds the photos that have every word of the query in what some model
// said of them, best first. A photo is as good as its best model.
func (s *Searcher) words(query string, o SearchOptions) ([]wordHit, error) {
	q := `SELECT photo_class_fts.photo_id AS photo_id, photo_class_fts.model AS model,
			bm25(photo_class_fts, 0, 0, 1.0, 2.0, 2.0) AS rank,
			highlight(photo_class_fts, 2, '[', ']') AS caption,
			highlight(photo_class_fts, 3, '[', ']') AS labels,
			highlight(photo_class_fts, 4, '[', ']') AS place
		FROM photo_class_fts JOIN photo p ON p.id = photo_class_fts.photo_id
		WHERE photo_class_fts MATCH ? `
	if o.PublishedOnly {
		q += `AND p.published > 0 `
	}
	q += `ORDER BY rank`
	var rows []struct {
		PhotoID string  `db:"photo_id"`
		Model   string  `db:"model"`
		Rank    float64 `db:"rank"`
		Caption string  `db:"caption"`
		Labels  string  `db:"labels"`
		Place   string  `db:"place"`
	}
	if err := s.db.Select(&rows, q, ftsQuery(query)); err != nil {
		return nil, fmt.Errorf("searching words: %w", err)
	}
	var hits []wordHit
	at := map[string]int{}
	for _, r := range rows { // best first, so a photo's first row is its best
		// a model's row that matched in the place only says what the row of the
		// place already says
		if r.Model != "place" && !strings.Contains(r.Caption+r.Labels, "[") {
			continue
		}
		w := WordWhy{Model: r.Model, Caption: r.Caption, Labels: strings.ReplaceAll(r.Labels, "\n", ", "),
			Place: strings.ReplaceAll(r.Place, "\n", ", "), Rank: r.Rank}
		if i, ok := at[r.PhotoID]; ok {
			hits[i].why = append(hits[i].why, w)
			continue
		}
		at[r.PhotoID] = len(hits)
		hits = append(hits, wordHit{id: r.PhotoID, why: []WordWhy{w}})
	}
	return hits, nil
}

type vecHit struct {
	id  string
	cos float64
}

// vector finds the photos whose embeddings are near the query's, nearest first.
// When it can't, it says why in the note, and the words are all there is.
func (s *Searcher) vector(ctx context.Context, query string, o SearchOptions) ([]vecHit, string) {
	emb, err := s.getEmbedder()
	if err != nil {
		return nil, "vector search is off: " + err.Error()
	}
	if emb == nil {
		return nil, ""
	}
	ix, err := s.index(s.embModel)
	if err != nil {
		slog.Error("loading embeddings", "err", err)
		return nil, "vector search failed: " + err.Error()
	}
	if len(ix.ids) == 0 {
		return nil, ""
	}
	qv, err := emb.EmbedText(ctx, query)
	if err != nil {
		slog.Error("embedding a query", "err", err)
		s.embedderFailed(emb, err)
		return nil, "vector search failed: " + err.Error()
	}
	q := classify.Normalize(qv)
	var published map[string]bool
	if o.PublishedOnly {
		var ids []string
		if err := s.db.Select(&ids, `SELECT id FROM photo WHERE published > 0`); err != nil {
			return nil, "vector search failed: " + err.Error()
		}
		published = make(map[string]bool, len(ids))
		for _, id := range ids {
			published[id] = true
		}
	}
	var hits []vecHit
	for i, v := range ix.vecs {
		if len(v) != len(q) || (published != nil && !published[ix.ids[i]]) {
			continue
		}
		var dot float64
		for j := range v {
			dot += float64(v[j]) * float64(q[j])
		}
		if dot >= s.cfg.MinVector {
			hits = append(hits, vecHit{ix.ids[i], dot})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].cos > hits[j].cos })
	if len(hits) > o.Limit {
		hits = hits[:o.Limit]
	}
	return hits, ""
}

// getEmbedder starts the embedder the first time it is wanted, and again some
// time after it has failed (so that a worker that died doesn't leave the
// search without vectors until a restart, and a broken one isn't started on
// every search).
func (s *Searcher) getEmbedder() (classify.TextEmbedder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Embedder == "" && s.embedder == nil {
		return nil, nil
	}
	if s.embedder != nil {
		s.used()
		return s.embedder, nil
	}
	if s.embErr != nil && time.Now().Before(s.embRetry) {
		return nil, s.embErr
	}
	c, err := classify.Spec(s.cfg.Embedder).Open()
	if err == nil {
		te, ok := c.(classify.TextEmbedder)
		if !ok {
			c.Close()
			err = fmt.Errorf("%q can't embed text", s.cfg.Embedder)
		} else {
			s.embedder, s.embErr = te, nil
			s.used()
			return te, nil
		}
	}
	slog.Error("starting the search embedder", "err", err)
	s.embErr, s.embRetry = err, time.Now().Add(embedderRetry)
	return nil, err
}

// used says that the embedder was wanted just now, for as long as Idle. The
// caller has s.mu.
func (s *Searcher) used() {
	if s.cfg.Idle <= 0 {
		return
	}
	s.lastUse = time.Now()
	if s.idle == nil {
		s.idle = time.AfterFunc(s.cfg.Idle, s.unloadIdle)
	} else {
		s.idle.Reset(s.cfg.Idle)
	}
}

// unloadIdle lets go of the embedder, if it hasn't been wanted for Idle; the
// next search starts it again.
func (s *Searcher) unloadIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.embedder == nil {
		return
	}
	if left := s.cfg.Idle - time.Since(s.lastUse); left > 0 {
		s.idle.Reset(left)
		return
	}
	if c, ok := s.embedder.(interface{ Close() error }); ok {
		c.Close()
	}
	s.embedder, s.ready = nil, false
	slog.Info("search embedder unloaded", "idle", s.cfg.Idle)
}

// Warm starts the embedder in the background, and has it embed something, so
// that the first search doesn't wait for the model to load: starting a worker
// is only starting its process, and it answers when the model is in. A failure
// is logged and left for a search to try again. If it is already running this
// only says that it is wanted, so a page that leads to a search can call it
// (an embedder that is let go when idle is back by the time the search is).
func (s *Searcher) Warm() {
	go func() {
		start := time.Now()
		s.mu.Lock()
		running := s.ready && s.embedder != nil
		s.mu.Unlock()
		te, _ := s.getEmbedder()
		if te == nil || running {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if _, err := te.EmbedText(ctx, "a photo"); err != nil {
			slog.Error("warming the search embedder", "err", err)
			s.embedderFailed(te, err)
			return
		}
		s.mu.Lock()
		s.ready = s.embedder == te
		s.mu.Unlock()
		slog.Info("search embedder ready", "took", time.Since(start).Round(time.Millisecond))
	}()
}

// embedderFailed drops an embedder that has failed, to be started again later.
func (s *Searcher) embedderFailed(te classify.TextEmbedder, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.embedder != te { // someone already has
		return
	}
	if c, ok := te.(interface{ Close() error }); ok {
		c.Close()
	}
	s.embedder, s.ready, s.embErr, s.embRetry = nil, false, err, time.Now().Add(embedderRetry)
}

// A vecIndex is a model's embeddings, unit length, in memory.
type vecIndex struct {
	stamp string
	ids   []string
	vecs  [][]float32
}

// index is the model's embeddings, loaded again when the table has changed.
func (s *Searcher) index(model string) (*vecIndex, error) {
	var st struct {
		N   int   `db:"n"`
		Max int64 `db:"m"`
	}
	if err := s.db.Get(&st, `SELECT count(*) AS n, coalesce(max(rowid), 0) AS m FROM photo_class
		WHERE model=? AND embedding IS NOT NULL`, model); err != nil {
		return nil, err
	}
	stamp := fmt.Sprintf("%d/%d", st.N, st.Max)
	s.mu.Lock()
	ix := s.vecs[model]
	s.mu.Unlock()
	if ix != nil && ix.stamp == stamp {
		return ix, nil
	}
	var rows []struct {
		ID  string `db:"photo_id"`
		Emb []byte `db:"embedding"`
	}
	if err := s.db.Select(&rows, `SELECT photo_id, embedding FROM photo_class
		WHERE model=? AND embedding IS NOT NULL`, model); err != nil {
		return nil, err
	}
	ix = &vecIndex{stamp: stamp}
	for _, r := range rows {
		v, err := classify.DecodeHalves(r.Emb)
		if err != nil {
			continue
		}
		ix.ids = append(ix.ids, r.ID)
		ix.vecs = append(ix.vecs, classify.Normalize(v))
	}
	s.mu.Lock()
	s.vecs[model] = ix
	s.mu.Unlock()
	return ix, nil
}
