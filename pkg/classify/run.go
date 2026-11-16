package classify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A Record is what a Run saves for one photo: one line of the results file.
type Record struct {
	Path   string  `json:"path"`
	Millis int64   `json:"ms"`
	Error  string  `json:"error,omitempty"`
	Result *Result `json:"result,omitempty"`
}

var stderr io.Writer = os.Stderr

// ImageFiles lists the images in dir, recursively, in path order.
func ImageFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp":
			files = append(files, p)
		}
		return nil
	})
	slices.Sort(files)
	return files, err
}

// ReadRecords reads a results file; a file that isn't there has no records.
func ReadRecords(file string) ([]Record, error) {
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var recs []Record
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var rec Record
			if jerr := json.Unmarshal(line, &rec); jerr != nil {
				return nil, fmt.Errorf("%s: %w", file, jerr)
			}
			recs = append(recs, rec)
		}
		if err == io.EOF {
			return recs, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// RunOptions tune a Run.
type RunOptions struct {
	// Progress is told of each photo as it is done (may be nil).
	Progress func(done, total int, rec Record)
}

// Run classifies the files and appends a Record for each to the results file.
// Photos that are already in it without an error are skipped, so that a run
// can be stopped and resumed, and grown by running it over more photos.
// (Photos are done one at a time: a model is usually the one thing using the
// machine, and a second at once only slows the first.)
func Run(ctx context.Context, c Classifier, files []string, results string, opt RunOptions) error {
	prev, err := ReadRecords(results)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, r := range prev {
		if r.Error == "" {
			done[r.Path] = true
		}
	}
	var todo []string
	for _, f := range files {
		if !done[f] {
			todo = append(todo, f)
		}
	}
	if err := os.MkdirAll(filepath.Dir(results), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(results, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := json.NewEncoder(out)
	for i, f := range todo {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		start := time.Now()
		res, err := c.Classify(ctx, f)
		rec := Record{Path: f, Millis: time.Since(start).Milliseconds(), Result: res}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rec.Error, rec.Result = err.Error(), nil
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
		if opt.Progress != nil {
			opt.Progress(i+1, len(todo), rec)
		}
	}
	return nil
}
