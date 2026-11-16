// Command classify runs photo classifiers over a directory of images and
// searches what they said, to find out which one gives the best results on
// your photos. See pkg/classify/research.md.
//
//	classify run     -b ollama:qwen2.5vl:7b -dir ~/photos -limit 200
//	classify search  -b ollama:qwen2.5vl:7b -q pizza
//	classify compare -q pizza,beach -dir ~/photos -b A -b B
//	classify stats   -b A -b B
//
// and to classify the photos of a gallery that is somewhere else, and send back
// what was seen:
//
//	classify pull    -url https://example.com/gallery -dir pulled
//	classify run     -dir pulled -b A -b B
//	classify export  -dir pulled -b A -b B -o classifications.json
//
// which the gallery's admin imports (Gallery, Classify).
//
// Results are kept, one file per backend, in -out (default ./classify-results).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/monet/pkg/classify"
)

type specs []classify.Spec

func (s *specs) String() string { return fmt.Sprint(*s) }
func (s *specs) Set(v string) error {
	*s = append(*s, classify.Spec(v))
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(ctx, os.Args[2:])
	case "search":
		err = cmdSearch(ctx, os.Args[2:])
	case "compare":
		err = cmdCompare(ctx, os.Args[2:])
	case "stats":
		err = cmdStats(os.Args[2:])
	case "pull":
		err = cmdPull(ctx, os.Args[2:])
	case "export":
		err = cmdExport(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "classify:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: classify pull|run|export|search|compare|stats [flags]
  -b SPEC    a backend: ollama:MODEL[@HOST] or worker:COMMAND (prefix with name= to name its results)
  -out DIR   where results are kept (default ./classify-results)
pull:      -url GALLERY_URL -dir DIR -limit N   (the published photos, into DIR)
export:    -dir DIR -b SPEC... -o FILE           (what the backends saw in the pulled photos)
run flags: -dir DIR -limit N
search:    -q QUERY -n N -mode auto|lexical|vector
compare:   -q Q1,Q2 -n N -judge FILE   (FILE has lines of: query<TAB>path of a photo that is right)`)
	os.Exit(2)
}

func results(out string, s classify.Spec) string { return filepath.Join(out, s.Name()+".jsonl") }

func cmdRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var bs specs
	fs.Var(&bs, "b", "backend (repeatable)")
	dir := fs.String("dir", ".", "directory of photos")
	out := fs.String("out", "classify-results", "results directory")
	limit := fs.Int("limit", 0, "only the first N photos")
	fs.Parse(args)
	files, err := classify.ImageFiles(*dir)
	if err != nil {
		return err
	}
	if *limit > 0 && len(files) > *limit {
		files = files[:*limit]
	}
	if len(bs) == 0 || len(files) == 0 {
		return fmt.Errorf("need a backend (-b) and photos in -dir")
	}
	for _, s := range bs {
		c, err := s.Open()
		if err != nil {
			return err
		}
		fmt.Printf("== %s: %d photos\n", s.Name(), len(files))
		err = classify.Run(ctx, c, files, results(*out, s), classify.RunOptions{
			Progress: func(done, total int, r classify.Record) {
				note := produced(r)
				if r.Error != "" {
					note = "ERROR " + r.Error
				}
				fmt.Printf("%4d/%d %6dms  %s  %s\n", done, total, r.Millis, filepath.Base(r.Path), note)
			},
		})
		c.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// produced says what a backend gave back for a photo: its labels, caption and
// embedding, whichever it makes.
func produced(r classify.Record) string {
	if r.Result == nil {
		return "nothing"
	}
	var parts []string
	if n := len(r.Result.Labels); n > 0 {
		parts = append(parts, fmt.Sprintf("%d labels", n))
	}
	if n := len(r.Result.Caption); n > 0 {
		parts = append(parts, fmt.Sprintf("caption %d chars", n))
	}
	if n := len(r.Result.Embedding); n > 0 {
		parts = append(parts, fmt.Sprintf("embedding %d dims", n))
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

func labels(r classify.Record) []classify.Label {
	if r.Result == nil {
		return nil
	}
	return r.Result.Labels
}

// search runs the query against an index, by vector if it can and is wanted.
func search(ctx context.Context, s classify.Spec, ix *classify.Index, q, mode string, n int, open func() (classify.Classifier, error)) ([]classify.Hit, error) {
	if mode == "auto" {
		mode = "lexical"
		if ix.HasEmbeddings() {
			mode = "vector"
		}
	}
	if mode == "lexical" {
		return ix.Lexical(q, n), nil
	}
	c, err := open()
	if err != nil {
		return nil, err
	}
	te, ok := c.(classify.TextEmbedder)
	if !ok {
		return nil, fmt.Errorf("%s can't embed text, so can't search by vector", s.Name())
	}
	return ix.Vector(ctx, te, q, n)
}

func cmdSearch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	var bs specs
	fs.Var(&bs, "b", "backend")
	out := fs.String("out", "classify-results", "results directory")
	q := fs.String("q", "", "query")
	n := fs.Int("n", 12, "how many")
	mode := fs.String("mode", "auto", "auto, lexical or vector")
	fs.Parse(args)
	if len(bs) != 1 || *q == "" {
		return fmt.Errorf("need one backend (-b) and a query (-q)")
	}
	s := bs[0]
	ix, err := classify.LoadIndex(results(*out, s))
	if err != nil {
		return err
	}
	var opened classify.Classifier
	defer func() {
		if opened != nil {
			opened.Close()
		}
	}()
	hits, err := search(ctx, s, ix, *q, *mode, *n, func() (classify.Classifier, error) {
		var err error
		opened, err = s.Open()
		return opened, err
	})
	if err != nil {
		return err
	}
	for _, h := range hits {
		fmt.Printf("%.3f  %s\n", h.Score, h.Path)
	}
	if len(hits) == 0 {
		fmt.Println("nothing")
	}
	return nil
}

func cmdCompare(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	var bs specs
	fs.Var(&bs, "b", "backend (repeatable)")
	out := fs.String("out", "classify-results", "results directory")
	qs := fs.String("q", "", "comma separated queries")
	n := fs.Int("n", 8, "how many")
	judge := fs.String("judge", "", "file of query<TAB>right path, to score the backends")
	mode := fs.String("mode", "auto", "auto, lexical or vector")
	fs.Parse(args)
	var queries []string
	for _, q := range strings.Split(*qs, ",") {
		if q = strings.TrimSpace(q); q != "" {
			queries = append(queries, q)
		}
	}
	right := map[string]map[string]bool{}
	if *judge != "" {
		data, err := os.ReadFile(*judge)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if q, p, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok {
				if right[q] == nil {
					right[q] = map[string]bool{}
					if *qs == "" {
						queries = append(queries, q)
					}
				}
				right[q][p] = true
			}
		}
	}
	if len(bs) == 0 || len(queries) == 0 {
		return fmt.Errorf("need backends (-b) and queries (-q or -judge)")
	}
	type tally struct {
		p, r float64
		n    int
	}
	scores := map[string]*tally{}
	for _, s := range bs {
		ix, err := classify.LoadIndex(results(*out, s))
		if err != nil {
			return err
		}
		var opened classify.Classifier
		open := func() (classify.Classifier, error) {
			if opened != nil {
				return opened, nil
			}
			var err error
			opened, err = s.Open()
			return opened, err
		}
		fmt.Printf("== %s (%d photos)\n", s.Name(), ix.Len())
		sc := &tally{}
		scores[s.Name()] = sc
		for _, q := range queries {
			hits, err := search(ctx, s, ix, q, *mode, *n, open)
			if err != nil {
				return err
			}
			fmt.Printf("  %q: %d hits\n", q, len(hits))
			found := 0
			for _, h := range hits {
				mark := " "
				if want := right[q]; want != nil {
					mark = "-"
					if want[h.Path] {
						mark, found = "+", found+1
					}
				}
				fmt.Printf("    %s %.3f  %s\n", mark, h.Score, h.Path)
			}
			if want := right[q]; want != nil && len(hits) > 0 {
				sc.p += float64(found) / float64(len(hits))
				sc.r += float64(found) / float64(len(want))
				sc.n++
			}
		}
		if opened != nil {
			opened.Close()
		}
	}
	if len(right) > 0 {
		fmt.Printf("\n== scores, mean over queries, top %d\n", *n)
		var names []string
		for name := range scores {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if t := scores[name]; t.n > 0 {
				fmt.Printf("  %-40s precision %.2f  recall %.2f\n", name, t.p/float64(t.n), t.r/float64(t.n))
			}
		}
	}
	return nil
}

func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	var bs specs
	fs.Var(&bs, "b", "backend (repeatable)")
	out := fs.String("out", "classify-results", "results directory")
	fs.Parse(args)
	if len(bs) == 0 {
		return fmt.Errorf("need backends (-b)")
	}
	for _, s := range bs {
		file := results(*out, s)
		recs, err := classify.ReadRecords(file)
		if err != nil {
			return err
		}
		var ms []float64
		var errs, nlabels, ncap, dims int
		for _, r := range recs {
			if r.Error != "" {
				errs++
				continue
			}
			ms = append(ms, float64(r.Millis))
			nlabels += len(r.Result.Labels)
			ncap += len(r.Result.Caption)
			dims = max(dims, len(r.Result.Embedding))
		}
		st, _ := os.Stat(file)
		fmt.Printf("== %s\n", s.Name())
		if len(ms) == 0 {
			fmt.Printf("  %d photos, %d errors\n", len(recs), errs)
			continue
		}
		sort.Float64s(ms)
		pct := func(p float64) float64 { return ms[int(math.Min(float64(len(ms)-1), p*float64(len(ms))))] }
		n := float64(len(ms))
		fmt.Printf("  photos %d  errors %d\n", len(ms), errs)
		fmt.Printf("  time per photo: median %.0fms  p95 %.0fms  (%.1f photos/min)\n", pct(.5), pct(.95), 60000/(sum(ms)/n))
		fmt.Printf("  per photo: %.1f labels, %.0f caption chars, %d embedding dims\n", float64(nlabels)/n, float64(ncap)/n, dims)
		fmt.Printf("  results file: %.1f KB per photo as json\n", float64(st.Size())/n/1024)
	}
	return nil
}

func sum(xs []float64) float64 {
	var t float64
	for _, x := range xs {
		t += x
	}
	return t
}

func cmdPull(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	base := fs.String("url", "", "the gallery's url, eg. https://example.com/gallery")
	dir := fs.String("dir", "pulled", "where the photos go")
	limit := fs.Int("limit", 0, "only the first N photos")
	fs.Parse(args)
	if *base == "" {
		return fmt.Errorf("need -url")
	}
	got, err := classify.Pull(ctx, *base, *dir, classify.PullOptions{
		Limit: *limit,
		Progress: func(done, total int, p classify.Pulled, err error) {
			if err != nil {
				fmt.Printf("%4d/%d  %s  ERROR %v\n", done, total, p.ID, err)
			} else if done%25 == 0 || done == total {
				fmt.Printf("%4d/%d\n", done, total)
			}
		},
	})
	fmt.Printf("%d photos in %s\n", len(got), *dir)
	return err
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	var bs specs
	fs.Var(&bs, "b", "backend (repeatable)")
	dir := fs.String("dir", "pulled", "the directory the photos were pulled into")
	out := fs.String("out", "classify-results", "results directory")
	file := fs.String("o", "classifications.json", "the export to write")
	fs.Parse(args)
	if len(bs) == 0 {
		return fmt.Errorf("need a backend (-b)")
	}
	pulled, err := classify.ReadManifest(*dir)
	if err != nil {
		return fmt.Errorf("%s was not made by pull: %w", *dir, err)
	}
	ex := classify.Export{Version: classify.ExportVersion, Created: time.Now().UTC()}
	for _, s := range bs {
		recs, err := classify.ReadRecords(results(*out, s))
		if err != nil {
			return err
		}
		items := classify.Items(pulled, *dir, s.Name(), recs)
		fmt.Printf("%s: %d of %d photos\n", s.Name(), len(items), len(pulled))
		ex.Items = append(ex.Items, items...)
	}
	b, err := json.Marshal(ex)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*file, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d items, %.1f MB\n", *file, len(ex.Items), float64(len(b))/1e6)
	return nil
}
