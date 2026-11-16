package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// a gallery with n published photos, listed two to a page
func fakeGallery(t *testing.T, n int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/gallery/photos.json", func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		var page remoteManifest
		for i := 1; i <= n; i++ {
			id := fmt.Sprintf("p%02d", i)
			if id <= after {
				continue
			}
			if len(page.Photos) == 2 {
				page.Next = page.Photos[1].ID
				break
			}
			page.Photos = append(page.Photos, struct {
				ID     string `json:"id"`
				SHA256 string `json:"sha256"`
				URL    string `json:"url"`
			}{id, "sha-" + id, "/i/photos/" + id + ".JPG"})
		}
		json.NewEncoder(w).Encode(page)
	})
	mux.HandleFunc("/i/photos/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "image of ", filepath.Base(r.URL.Path))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestPullAndItems(t *testing.T) {
	srv := fakeGallery(t, 5)
	dir := t.TempDir()
	got, err := Pull(context.Background(), srv.URL+"/gallery/", dir, PullOptions{})
	if err != nil || len(got) != 5 {
		t.Fatalf("pull: %v, %d photos", err, len(got))
	}
	b, err := os.ReadFile(filepath.Join(dir, "p03.jpg"))
	if err != nil || string(b) != "image of p03.JPG" {
		t.Fatalf("p03.jpg is %q, %v", b, err)
	}
	m, err := ReadManifest(dir)
	if err != nil || len(m) != 5 || m[0] != (Pulled{"p01", "sha-p01", "p01.jpg"}) {
		t.Fatalf("manifest %v, %v", m, err)
	}

	// pulling again fetches nothing that is there, and -limit stops early
	os.Remove(filepath.Join(dir, "p05.jpg"))
	if _, err := Pull(context.Background(), srv.URL+"/gallery", dir, PullOptions{Limit: 3}); err != nil {
		t.Fatal(err)
	}
	if m, _ = ReadManifest(dir); len(m) != 3 {
		t.Fatalf("a limit of 3 gave %d photos", len(m))
	}

	// what a model saw becomes items, by file; failures and the unseen are left out
	recs := []Record{
		{Path: filepath.Join(dir, "p01.jpg"), Error: "timeout"},
		{Path: filepath.Join(dir, "p01.jpg"), Result: &Result{Caption: "a tomato", Labels: []Label{{Name: "tomato"}}}},
		{Path: filepath.Join(dir, "p02.jpg"), Error: "timeout"},
		{Path: filepath.Join(dir, "elsewhere.jpg"), Result: &Result{Caption: "not ours"}},
	}
	items := Items(m, dir, "gemma", recs)
	if len(items) != 1 || items[0].ID != "p01" || items[0].SHA256 != "sha-p01" || items[0].Model != "gemma" || items[0].Caption != "a tomato" {
		t.Fatalf("items %+v", items)
	}
	recs = append(recs, Record{Path: filepath.Join(dir, "p03.jpg"), Result: &Result{Embedding: []float32{1, 0}}})
	items = Items(m, dir, "siglip", recs)
	if len(items) != 2 || items[1].Embedding == "" {
		t.Fatalf("embedding items %+v", items)
	}
}
