package gallery

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The instagram data export ("Download your information", as JSON) is a zip
// of a directory tree. What matters here:
//
//	your_instagram_activity/media/posts_N.json   the posts: media, caption, time, some exif
//	logged_information/past_instagram_insights/posts.json   per post counts: likes, comments, ...
//	media/...                                     the files the posts name
//
// The export has no comment text from other people, just counts, and the image
// files have their own EXIF stripped, which is why gps and the time taken come
// from the json. (Stories, reels and profile photos are left alone.)
var (
	igPostFiles    = []string{"your_instagram_activity/media/posts_*.json", "content/posts_*.json"}
	igInsightsFile = "logged_information/past_instagram_insights/posts.json"

	// instagramNS makes the same media id the same photo id wherever it is imported
	instagramNS = uuid.MustParse("6c1e5d57-8f0a-4d7e-9a53-b0e6f0c2a1d4")
)

// An InstagramImporter loads an instagram export into the gallery. It can be
// run on any number of exports, newer or older, of the same account: a photo
// that is already there is found by its instagram media id and is not added
// again; only what instagram has since changed (its counts) is updated.
type InstagramImporter struct {
	svc *PhotoService

	// Published is whether newly imported photos are shown on the site. The
	// export is of public posts, so that's the default.
	Published bool
}

func NewInstagramImporter(svc *PhotoService) *InstagramImporter {
	return &InstagramImporter{svc: svc, Published: true}
}

// An ImportReport says what an import did.
type ImportReport struct {
	Posts     int      `json:"posts"` // posts in the export
	Media     int      `json:"media"` // photos in them
	Created   int      `json:"created"`
	Updated   int      `json:"updated"` // already there, but instagram's counts or words have changed
	Unchanged int      `json:"unchanged"`
	Videos    int      `json:"videos"`  // skipped: the gallery is photos
	Missing   []string `json:"missing"` // named in the export, but not in it
	Failed    []string `json:"failed"`  // "path: why"
}

func (r *ImportReport) String() string {
	s := fmt.Sprintf("%d posts, %d photos: %d new, %d updated, %d unchanged", r.Posts, r.Media, r.Created, r.Updated, r.Unchanged)
	if r.Videos > 0 {
		s += fmt.Sprintf(", %d videos skipped", r.Videos)
	}
	if len(r.Missing) > 0 {
		s += fmt.Sprintf(", %d files missing from the export", len(r.Missing))
	}
	if len(r.Failed) > 0 {
		s += fmt.Sprintf(", %d failed", len(r.Failed))
	}
	return s
}

// ImportPath imports the export at path, which is either the zip file or the
// directory it unzips to.
func (im *InstagramImporter) ImportPath(ctx context.Context, p string) (*ImportReport, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return im.Import(ctx, os.DirFS(p))
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", p, err)
	}
	defer zr.Close()
	return im.Import(ctx, zr)
}

// Import imports the export in fsys.
func (im *InstagramImporter) Import(ctx context.Context, fsys fs.FS) (*ImportReport, error) {
	root, err := exportRoot(fsys)
	if err != nil {
		return nil, err
	}

	posts, err := readPosts(root)
	if err != nil {
		return nil, err
	}
	insights := readInsights(root)

	rep := &ImportReport{Posts: len(posts)}
	for _, post := range posts {
		for i := range post.Media {
			if err := ctx.Err(); err != nil {
				return rep, err
			}
			im.importMedia(root, rep, post, i, insights)
		}
	}
	return rep, nil
}

type igPost struct {
	Title             string    `json:"title"`
	CreationTimestamp int64     `json:"creation_timestamp"`
	Media             []igMedia `json:"media"`
}

type igMedia struct {
	URI               string `json:"uri"`
	CreationTimestamp int64  `json:"creation_timestamp"`
	Title             string `json:"title"`
	MediaMetadata     struct {
		PhotoMetadata *struct {
			ExifData []map[string]any `json:"exif_data"`
		} `json:"photo_metadata"`
	} `json:"media_metadata"`
	CrossPostSource struct {
		SourceApp string `json:"source_app"`
	} `json:"cross_post_source"`
}

// exif is what instagram kept of the photo's exif: where and when.
func (m *igMedia) exif() (lat, lon *float64, taken time.Time, device, software string) {
	if m.MediaMetadata.PhotoMetadata == nil {
		return
	}
	for _, e := range m.MediaMetadata.PhotoMetadata.ExifData {
		la, ok1 := e["latitude"].(float64)
		lo, ok2 := e["longitude"].(float64)
		if ok1 && ok2 && !(la == 0 && lo == 0) {
			lat, lon = &la, &lo
		}
		if s, ok := e["date_time_original"].(string); ok && taken.IsZero() {
			taken = ParseExifTime(s)
		}
		if s, ok := e["device_id"].(string); ok {
			device = s
		}
		if s, ok := e["software"].(string); ok {
			software = s
		}
	}
	return
}

type igStats struct {
	Likes, Comments, Saves, Shares, Impressions, Reach, ProfileVisits, Follows int
}

func (im *InstagramImporter) importMedia(root fs.FS, rep *ImportReport, post igPost, index int, insights map[string]igStats) {
	m := post.Media[index]
	rep.Media++

	ext := strings.ToLower(path.Ext(m.URI))
	if ext == ".mp4" || ext == ".mov" || ext == ".m4v" {
		rep.Videos++
		rep.Media--
		return
	}
	stem := strings.TrimSuffix(path.Base(m.URI), path.Ext(m.URI))
	if stem == "" || stem == "." {
		rep.Failed = append(rep.Failed, fmt.Sprintf("%q: no media id", m.URI))
		return
	}

	caption := fixMojibake(strings.TrimSpace(m.Title))
	if caption == "" {
		caption = fixMojibake(strings.TrimSpace(post.Title))
	}
	posted := m.CreationTimestamp
	if posted == 0 {
		posted = post.CreationTimestamp
	}
	lat, lon, taken, device, software := m.exif()
	st := insights[m.URI]

	meta := Meta{Instagram: &InstagramMeta{
		URI:           m.URI,
		CrossPost:     m.CrossPostSource.SourceApp,
		DeviceID:      device,
		Saves:         st.Saves,
		Shares:        st.Shares,
		Impressions:   st.Impressions,
		Reach:         st.Reach,
		ProfileVisits: st.ProfileVisits,
		Follows:       st.Follows,
	}}
	group := ""
	if len(post.Media) > 1 {
		group = "post:" + strings.TrimSuffix(path.Base(post.Media[0].URI), path.Ext(post.Media[0].URI))
		meta.Instagram.Index = index + 1
	}

	existing, err := im.svc.Find(SourceInstagram, stem)
	if err != nil {
		rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", m.URI, err))
		return
	}

	if existing != nil {
		changed, err := im.refresh(existing, caption, group, lat, lon, st, meta)
		switch {
		case err != nil:
			rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", m.URI, err))
		case changed:
			rep.Updated++
		default:
			rep.Unchanged++
		}
		return
	}

	if !fs.ValidPath(m.URI) {
		rep.Failed = append(rep.Failed, fmt.Sprintf("%q: not a path inside the export", m.URI))
		return
	}
	data, err := fs.ReadFile(root, m.URI)
	if errors.Is(err, fs.ErrNotExist) {
		rep.Missing = append(rep.Missing, m.URI)
		return
	}
	if err != nil {
		rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", m.URI, err))
		return
	}

	var takenAt int64
	if !taken.IsZero() {
		takenAt = taken.Unix()
	}
	p, err := im.svc.Add(NewPhoto{
		ID:          uuid.NewSHA1(instagramNS, []byte(stem)).String(),
		Source:      SourceInstagram,
		SourceID:    stem,
		SourceGroup: group,
		Data:        data,
		Caption:     caption,
		PostedAt:    posted,
		TakenAt:     takenAt,
		Software:    software,
		Latitude:    lat,
		Longitude:   lon,
		Likes:       st.Likes,
		Comments:    st.Comments,
		Meta:        meta,
		Published:   im.Published,
	})
	if err != nil {
		rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", m.URI, err))
		return
	}
	slog.Debug("imported instagram photo", "id", p.ID, "media", stem)
	rep.Created++
}

// refresh brings a photo that has been imported before up to date with an export
// of it. It takes instagram's counts as they are now, but nothing that could
// have been edited here: a caption is only filled in if it's empty, and whether
// the photo is shown is left alone.
func (im *InstagramImporter) refresh(p *Photo, caption, group string, lat, lon *float64, st igStats, meta Meta) (bool, error) {
	before := *p

	p.Likes, p.Comments = st.Likes, st.Comments
	if p.Caption == "" {
		p.Caption = caption
	}
	if p.SourceGroup == "" {
		p.SourceGroup = group
	}
	if !p.HasGPS() && lat != nil && lon != nil {
		p.Latitude, p.Longitude = lat, lon
	}
	if err := p.setMeta(meta); err != nil {
		return false, err
	}
	if p.MetaJSON == before.MetaJSON && p.Likes == before.Likes && p.Comments == before.Comments &&
		p.Caption == before.Caption && p.SourceGroup == before.SourceGroup && p.HasGPS() == before.HasGPS() {
		return false, nil
	}
	return true, im.svc.Save(p)
}

// exportRoot finds the export's root: the zip may hold it directly, or inside
// a single directory (as when it's been unzipped and zipped again).
func exportRoot(fsys fs.FS) (fs.FS, error) {
	if looksLikeExport(fsys) {
		return fsys, nil
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if sub, err := fs.Sub(fsys, e.Name()); err == nil && looksLikeExport(sub) {
				return sub, nil
			}
		}
	}
	return nil, errors.New("this doesn't look like an instagram export (no your_instagram_activity/media/posts_1.json)")
}

func looksLikeExport(fsys fs.FS) bool {
	for _, pattern := range igPostFiles {
		if m, _ := fs.Glob(fsys, pattern); len(m) > 0 {
			return true
		}
	}
	return false
}

func readPosts(root fs.FS) ([]igPost, error) {
	var all []igPost
	for _, pattern := range igPostFiles {
		files, _ := fs.Glob(root, pattern)
		for _, f := range files {
			b, err := fs.ReadFile(root, f)
			if err != nil {
				return nil, err
			}
			var posts []igPost
			if err := json.Unmarshal(b, &posts); err != nil {
				return nil, fmt.Errorf("reading %s: %w", f, err)
			}
			all = append(all, posts...)
		}
	}
	return all, nil
}

// readInsights returns the counts instagram kept for each post, by the path of
// the post's (first) media. They are not in every export, and not for every
// post, so the lack of them isn't an error.
func readInsights(root fs.FS) map[string]igStats {
	out := map[string]igStats{}
	b, err := fs.ReadFile(root, igInsightsFile)
	if err != nil {
		return out
	}
	var doc struct {
		Posts []struct {
			Media   map[string]struct{ URI string }   `json:"media_map_data"`
			Strings map[string]struct{ Value string } `json:"string_map_data"`
		} `json:"organic_insights_posts"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		slog.Warn("couldn't read instagram insights", "err", err)
		return out
	}
	for _, p := range doc.Posts {
		uri := p.Media["Media Thumbnail"].URI
		if uri == "" {
			continue
		}
		n := func(key string) int {
			v, _ := strconv.Atoi(strings.TrimSpace(p.Strings[key].Value))
			return v
		}
		out[uri] = igStats{
			Likes: n("Likes"), Comments: n("Comments"), Saves: n("Saves"), Shares: n("Shares"),
			Impressions: n("Impressions"), Reach: n("Accounts reached"),
			ProfileVisits: n("Profile visits"), Follows: n("Follows"),
		}
	}
	return out
}

// fixMojibake repairs the text of an export, in which instagram has written
// each UTF-8 byte as a character of its own: "CÃ´te" for "Côte". Text that
// isn't like that (plain ascii, or really is latin-1 looking) is returned as it was.
func fixMojibake(s string) string {
	ascii := true
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return s // a real character that can't be a byte: it was already right
		}
		if r >= 0x80 {
			ascii = false
		}
		b = append(b, byte(r))
	}
	if ascii || !utf8.Valid(b) {
		return s
	}
	return string(b)
}
