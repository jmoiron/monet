package classify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// Pull is how the photos of a gallery on another machine get here to be
// classified: the gallery lists its published photos (GET photos.json under
// its base url, a page at a time), and they are saved in a directory, each as
// its id and the extension its image has. What was pulled is written to
// manifest.json in the directory, which is what Export reads to know which
// photo (and which image of it) a file is.

// A Pulled is a photo in a pull directory.
type Pulled struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"` // of the gallery's original, which may not be the file pulled
	File   string `json:"file"`   // relative to the directory
}

// ManifestName is the file in a pull directory that says what is in it.
const ManifestName = "manifest.json"

// ReadManifest reads what a directory was pulled with.
func ReadManifest(dir string) ([]Pulled, error) {
	b, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, err
	}
	var m []Pulled
	return m, json.Unmarshal(b, &m)
}

// PullOptions tune a Pull.
type PullOptions struct {
	Limit    int          // stop after this many photos; 0 for all
	Parallel int          // downloads at a time; 0 for 4
	Client   *http.Client // 0 for the default
	Progress func(done, total int, p Pulled, err error)
}

// A page of the gallery's photos.json
type remoteManifest struct {
	Photos []struct {
		ID     string `json:"id"`
		SHA256 string `json:"sha256"`
		URL    string `json:"url"`
	} `json:"photos"`
	Next string `json:"next"`
}

// Pull saves the published photos of the gallery at base (its url, eg.
// https://example.com/gallery) in dir. Files already there are not fetched
// again, so a pull can be repeated to get what is new.
func Pull(ctx context.Context, base, dir string, opt PullOptions) ([]Pulled, error) {
	if opt.Client == nil {
		opt.Client = http.DefaultClient
	}
	if opt.Parallel <= 0 {
		opt.Parallel = 4
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	list, err := url.Parse(strings.TrimRight(base, "/") + "/photos.json")
	if err != nil {
		return nil, err
	}

	type job struct {
		p   Pulled
		src string
	}
	var jobs []job
	for after := ""; ; {
		q := list.Query()
		q.Set("after", after)
		pageURL := *list
		pageURL.RawQuery = q.Encode()
		var page remoteManifest
		if err := getJSON(ctx, opt.Client, pageURL.String(), &page); err != nil {
			return nil, fmt.Errorf("listing photos: %w", err)
		}
		for _, p := range page.Photos {
			src, err := list.Parse(p.URL)
			if err != nil || p.ID == "" || p.URL == "" || strings.ContainsAny(p.ID, `/\`) || strings.HasPrefix(p.ID, ".") {
				continue
			}
			ext := strings.ToLower(path.Ext(src.Path))
			jobs = append(jobs, job{Pulled{ID: p.ID, SHA256: p.SHA256, File: p.ID + ext}, src.String()})
		}
		if page.Next == "" || (opt.Limit > 0 && len(jobs) >= opt.Limit) {
			break
		}
		after = page.Next
	}
	if opt.Limit > 0 && len(jobs) > opt.Limit {
		jobs = jobs[:opt.Limit]
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		done   int
		failed []error
		got    = make([]Pulled, 0, len(jobs))
		work   = make(chan job)
	)
	for i := 0; i < opt.Parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				err := download(ctx, opt.Client, j.src, filepath.Join(dir, j.p.File))
				mu.Lock()
				done++
				if err != nil {
					failed = append(failed, fmt.Errorf("%s: %w", j.p.ID, err))
				} else {
					got = append(got, j.p)
				}
				if opt.Progress != nil {
					opt.Progress(done, len(jobs), j.p, err)
				}
				mu.Unlock()
			}
		}()
	}
loop:
	for _, j := range jobs {
		select {
		case work <- j:
		case <-ctx.Done():
			break loop
		}
	}
	close(work)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sortPulled(got)
	b, _ := json.MarshalIndent(got, "", " ")
	if err := os.WriteFile(filepath.Join(dir, ManifestName), b, 0o644); err != nil {
		return nil, err
	}
	return got, errors.Join(failed...)
}

func sortPulled(p []Pulled) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && p[j].ID < p[j-1].ID; j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}

func getJSON(ctx context.Context, c *http.Client, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// download saves a url as a file, unless there already is one.
func download(ctx context.Context, c *http.Client, src, dst string) error {
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", src, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s", resp.Status)
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".pull-*")
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), dst)
}

// Items makes the export items of one model's records over a pull directory:
// what the model saw in each photo that it saw anything in. Records are
// matched to photos by their file, so they have to be of the pulled files; the
// last record of a photo wins, as in a Run.
func Items(pulled []Pulled, dir, model string, recs []Record) []Item {
	byFile := map[string]Pulled{}
	for _, p := range pulled {
		abs, err := filepath.Abs(filepath.Join(dir, p.File))
		if err == nil {
			byFile[abs] = p
		}
	}
	last := map[string]Record{}
	for _, r := range recs {
		abs, err := filepath.Abs(r.Path)
		if err != nil {
			continue
		}
		if _, ok := byFile[abs]; ok {
			last[abs] = r
		}
	}
	var items []Item
	for _, p := range pulled {
		abs, _ := filepath.Abs(filepath.Join(dir, p.File))
		r, ok := last[abs]
		if !ok || r.Error != "" || r.Result == nil {
			continue
		}
		it := Item{ID: p.ID, SHA256: p.SHA256, Model: model, Caption: r.Result.Caption, Labels: r.Result.Labels}
		if len(r.Result.Embedding) > 0 {
			it.Embedding = EncodeEmbedding(r.Result.Embedding)
		}
		if it.Caption != "" || len(it.Labels) > 0 || it.Embedding != "" {
			items = append(items, it)
		}
	}
	return items
}
