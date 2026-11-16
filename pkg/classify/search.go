package classify

import (
	"context"
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
)

// An Index is the results of a Run, searchable.
type Index struct {
	recs []Record // those that worked
	norm [][]float32
}

// A Hit is a photo that matches a query.
type Hit struct {
	Path  string
	Score float64
}

// LoadIndex reads a results file.
func LoadIndex(file string) (*Index, error) {
	recs, err := ReadRecords(file)
	if err != nil {
		return nil, err
	}
	ix := &Index{}
	for _, r := range recs {
		if r.Error != "" || r.Result == nil {
			continue
		}
		ix.recs = append(ix.recs, r)
		ix.norm = append(ix.norm, normalized(r.Result.Embedding))
	}
	return ix, nil
}

// Len is the number of photos in the index.
func (ix *Index) Len() int { return len(ix.recs) }

// HasEmbeddings is whether the photos can be searched by vector.
func (ix *Index) HasEmbeddings() bool {
	for _, n := range ix.norm {
		if n != nil {
			return true
		}
	}
	return false
}

var word = regexp.MustCompile(`[\p{L}\p{N}]+`)

// stem is a crude singular: "pizzas" and "pizza", "berries" and "berry"
// are the same word for searching. It only has to agree with itself.
func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 3 && strings.HasSuffix(w, "es") && strings.HasSuffix(w[:len(w)-2], "s"):
		return w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		return w[:len(w)-1]
	}
	return w
}

func tokens(s string) []string {
	var out []string
	for _, w := range word.FindAllString(strings.ToLower(s), -1) {
		out = append(out, stem(w))
	}
	return out
}

// Lexical searches the labels and captions as words. A photo has to have every
// word of the query somewhere (in a label, or the caption); labels count for
// more than the caption, and a label that is the query's words exactly most.
func (ix *Index) Lexical(query string, n int) []Hit {
	q := tokens(query)
	if len(q) == 0 {
		return nil
	}
	whole := strings.Join(q, " ")
	var hits []Hit
	for _, r := range ix.recs {
		var score float64
		all := true
		capWords := map[string]bool{}
		for _, t := range tokens(r.Result.Caption) {
			capWords[t] = true
		}
		labelWords := map[string]float64{}
		for _, l := range r.Result.Labels {
			w := 1.0
			if l.Score > 0 {
				w = l.Score
			}
			lt := tokens(l.Name)
			if strings.Join(lt, " ") == whole {
				score += 2 * w
			}
			for _, t := range lt {
				labelWords[t] = max(labelWords[t], w)
			}
		}
		for _, t := range q {
			switch {
			case labelWords[t] > 0:
				score += labelWords[t]
			case capWords[t]:
				score += 0.5
			default:
				all = false
			}
		}
		if all {
			hits = append(hits, Hit{r.Path, score})
		}
	}
	return top(hits, n)
}

// Vector searches by the embeddings: the query is embedded by te, which has to
// be the model that made them, and the nearest photos are the hits.
func (ix *Index) Vector(ctx context.Context, te TextEmbedder, query string, n int) ([]Hit, error) {
	if !ix.HasEmbeddings() {
		return nil, errors.New("the results have no embeddings")
	}
	q, err := te.EmbedText(ctx, query)
	if err != nil {
		return nil, err
	}
	q = normalized(q)
	var hits []Hit
	for i, v := range ix.norm {
		if len(v) != len(q) || v == nil {
			continue
		}
		var dot float64
		for j := range v {
			dot += float64(v[j]) * float64(q[j])
		}
		hits = append(hits, Hit{ix.recs[i].Path, dot})
	}
	return top(hits, n), nil
}

func top(hits []Hit, n int) []Hit {
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if n > 0 && len(hits) > n {
		hits = hits[:n]
	}
	return hits
}

func normalized(v []float32) []float32 {
	if len(v) == 0 {
		return nil
	}
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return nil
	}
	inv := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}
