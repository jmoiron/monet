package blog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
	"github.com/jmoiron/monet/uploads"
)

const (
	panelListSize = 6
	adminPageSize = 20
)

type Admin struct {
	db       db.DB
	BaseURL  string
	registry vfs.Registry
}

func NewBlogAdmin(db db.DB, registry vfs.Registry) *Admin {
	return &Admin{db: db, registry: registry}
}

func (a *Admin) Bind(r chi.Router) {
	r.Get("/unpublished/", a.unpublishedList)
	r.Get("/unpublished/{page:[0-9]+}", a.unpublishedList)

	r.Get("/posts/", a.postList)
	r.Get("/posts/{page:[0-9]+}", a.postList)
	r.Get("/posts/add/", a.add)
	r.Get("/posts/edit/{slug:[^/]+}", a.edit)

	r.Post("/posts/add/", a.add)
	r.Post("/posts/edit/{slug:[^/]+}", a.save)
	r.Get("/posts/delete/{id:\\d+}", a.delete)
	
	// File attachment endpoints
	r.Post("/posts/{postId:\\d+}/upload", a.uploadFile)
	r.Delete("/posts/{postId:\\d+}/files/{uploadId:\\d+}", a.deleteAttachedFile)
	// r.Post("/posts/preview/", a.preview)
}

// Render a blog admin panel.
func (a *Admin) Panels(r *http.Request) ([]string, error) {
	// published + unpublished panel
	serv := NewPostService(a.db)
	published, err := serv.Select(fmt.Sprintf("WHERE published > 0 ORDER BY created_at DESC LIMIT %d;", panelListSize))
	if err != nil {
		return nil, err
	}
	unpublished, err := serv.Select(fmt.Sprintf("WHERE published = 0 ORDER BY updated_at DESC LIMIT %d;", panelListSize))
	if err != nil {
		return nil, err
	}

	var panels []string

	reg := mtr.RegistryFromContext(r.Context())

	var b bytes.Buffer
	err = reg.Render(&b, "blog/admin/post-panel.html", mtr.Ctx{
		"fullUrl":   "posts/",
		"title":     "Posts",
		"renderAdd": true,
		"posts":     published,
	})
	if err != nil {
		return nil, err
	}
	panels = append(panels, b.String())
	b.Reset()

	err = reg.Render(&b, "blog/admin/post-panel.html", mtr.Ctx{
		"fullUrl": "unpublished/",
		"title":   "Unpublished",
		"posts":   unpublished,
	})
	if err != nil {
		return nil, err
	}
	panels = append(panels, b.String())

	return panels, nil
}

func (a *Admin) unpublishedList(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM post WHERE published = 0;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	paginator := mtr.NewPaginator(adminPageSize, count)
	page := paginator.Page(app.GetIntParam(r, "page", 1))

	// select the posts for the page we're trying to render
	q := fmt.Sprintf(`WHERE published = 0 ORDER BY created_at DESC LIMIT %d OFFSET %d`, adminPageSize, page.StartOffset)

	serv := NewPostService(a.db)
	unpublished, err := serv.Select(q)

	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "blog/admin/post-list.html", mtr.Ctx{
		"unpublished": true,
		"posts":       unpublished,
		"pagination":  paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering list", "err", err)
	}

}

func (a *Admin) postList(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := a.db.Get(&count, "SELECT count(*) FROM post WHERE published > 0;"); err != nil {
		app.Http500("getting count", w, err)
		return
	}

	paginator := mtr.NewPaginator(adminPageSize, count)
	page := paginator.Page(app.GetIntParam(r, "page", 1))

	// select the posts for the page we're trying to render
	q := fmt.Sprintf(`WHERE published > 0 ORDER BY created_at DESC LIMIT %d OFFSET %d`, adminPageSize, page.StartOffset)

	serv := NewPostService(a.db)
	posts, err := serv.Select(q)
	if err != nil {
		slog.Error("looking up post", "error", err)
		app.Http404(w)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	err = reg.RenderWithBase(w, "admin-base", "blog/admin/post-list.html", mtr.Ctx{
		"posts":      posts,
		"pagination": paginator.Render(reg, page),
	})

	if err != nil {
		slog.Error("rendering list", "err", err)
	}
}

func (a *Admin) edit(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	serv := NewPostService(a.db)

	p, err := serv.GetSlug(slug)
	if err != nil {
		app.Http500("getting by slug", w, err)
		return
	}
	a.showEdit(w, r, p)
}

func (a *Admin) showEdit(w http.ResponseWriter, r *http.Request, p *Post) {
	serv := NewPostService(a.db)
	
	// Get attached files for this post
	var attachedFiles []*uploads.Upload
	if p.ID > 0 {
		var err error
		attachedFiles, err = serv.GetAttachedFiles(p.ID)
		if err != nil {
			slog.Error("failed to get attached files", "post_id", p.ID, "err", err)
			attachedFiles = []*uploads.Upload{} // fallback to empty list
		}
		
		// Add file URLs to attached files
		mapper := a.registry.Mapper()
		for _, file := range attachedFiles {
			if urlPath, err := mapper.GetURL(file.FilesystemName, file.Filename); err == nil {
				file.URL = urlPath
			}
		}
	}

	reg := mtr.RegistryFromContext(r.Context())
	err := reg.RenderWithBase(w, "admin-base", "blog/admin/post-edit.html", mtr.Ctx{
		"post":          p,
		"attachedFiles": attachedFiles,
	})
	if err != nil {
		slog.Error("rendering edit", "err", err)
	}
}

func (a *Admin) save(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		app.Http500("parsing form", w, err)
		return
	}

	id, err := strconv.Atoi(r.Form.Get("id"))
	if err != nil {
		app.Http500(fmt.Sprintf("invalid id sent: %s", r.Form.Get("id")), w, err)
		return
	}

	serv := NewPostService(a.db)
	p, err := serv.Get(id)
	if err != nil {
		app.Http500("fetching post", w, err)
		return
	}

	// update p and save
	p.Title = r.Form.Get("title")
	p.Slug = r.Form.Get("slug")
	p.Content = r.Form.Get("content")
	p.OgDescription = r.Form.Get("ogDescription")
	p.OgImage = r.Form.Get("ogImage")

	// if we're changing the published bit, set/unset the published at timestamp
	formPub, _ := strconv.Atoi(r.Form.Get("published"))
	if p.Published != formPub {
		p.Published = formPub
		if p.Published == 0 {
			var t time.Time
			p.PublishedAt = t
		} else {
			p.PublishedAt = time.Now()
		}
	}

	if err := serv.Save(p); err != nil {
		app.Http500("saving post", w, err)
		return
	}

	a.showEdit(w, r, p)

}

func (a *Admin) add(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	if r.Method == http.MethodGet {
		title := r.Form.Get("Title")
		reg := mtr.RegistryFromContext(r.Context())
		// redirect to edit page
		err := reg.RenderWithBase(w, "admin-base", "blog/admin/post-edit.html", mtr.Ctx{
			"post": Post{Title: title},
		})
		if err != nil {
			slog.Error("rendering add", "err", err)
		}
		return
	}

	var p Post
	p.Title = r.Form.Get("title")
	p.Slug = r.Form.Get("slug")
	p.Content = r.Form.Get("content")

	// if we're changing the published bit, set/unset the published at timestamp
	p.Published, _ = strconv.Atoi(r.Form.Get("published"))
	if p.Published > 0 {
		p.PublishedAt = time.Now()
	}

	err := NewPostService(a.db).Save(&p)
	if err != nil {
		app.Http500("saving post", w, err)
		return
	}

	editUrl := fmt.Sprintf("../edit/%s", p.Slug)
	http.Redirect(w, r, editUrl, http.StatusFound)

}

func (a *Admin) delete(w http.ResponseWriter, r *http.Request) {

	referer := r.Header.Get("Referer")
	if len(referer) == 0 {
		referer = "/admin/"
	}

	id := app.GetIntParam(r, "id", -1)
	if id <= 0 {
		slog.Warn("deleted non-existent post", "id", id)
		http.Redirect(w, r, referer, http.StatusFound)
		return
	}

	slog.Info("deleting post", "id", id)
	_, err := a.db.Exec("DELETE FROM post WHERE id=?", id)
	if err != nil {
		app.Http500("deleting post", w, err)
		return
	}
	http.Redirect(w, r, referer, http.StatusFound)

}

// *** Posts ***

/*

// List detail for published posts
func postList(page string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	num := app.PageNumber(page)

	paginator := app.NewPaginator(num, listPageSize)
	paginator.Link = "/admin/posts/"

	var posts []Post
	// do a search, if required, of title and content
	var err error
	var numObjects int

	if len(ctx.Params["Search"]) > 0 {
		term := M{"$regex": ctx.Params["Search"]}
		search := M{"published": 1, "$or": []M{
			M{"title": term},
			M{"content": term},
		}}
		err = db.Latest(&Post{}, search).Skip(paginator.Skip).Limit(listPageSize).All(&posts)
		numObjects, _ = db.Latest(&Post{}, search).Count()
	} else {
		err = db.Latest(&Post{}, M{"published": 1}).Skip(paginator.Skip).
			Limit(listPageSize).All(&posts)
		numObjects, _ = db.Find(&Post{}, M{"published": 1}).Count()
	}

	if err != nil {
		fmt.Println(err)
	}

	return adminBase.Render("blog/admin/post-list.mandira", M{
		"Posts": posts, "Pagination": paginator.Render(numObjects)})

}

func postAdd() string {
	if app.RequireAuthentication(ctx) {
		return ""
	}

	if ctx.Request.Method == "GET" {
		return adminBase.Render("blog/admin/posts-edit.mandira",
			ctx.Params, M{"Published": 0, "IsPublished": false})
	}

	post := new(Post)
	post.FromParams(ctx.Params)
	_, err := db.Upsert(post)
	if err != nil {
		fmt.Println(err)
	}
	ctx.Redirect(302, "/admin/")
	return ""
	//ctx.Redirect(302, "/admin/posts/edit/" + post.Slug + "/")
}

func postEdit(slug string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var post *Post
	err := db.Find(post, M{"slug": slug}).One(&post)
	if err != nil {
		fmt.Println(err)
		ctx.Redirect(302, "/admin/")
		return ""
	}
	if len(ctx.Params) > 1 {
		post.FromParams(ctx.Params)
		db.Upsert(post)
	}

	return adminBase.Render("blog/admin/posts-edit.mandira", post, M{
		"IsPublished": post.Published == 1,
		"IdHex":       post.Id.Hex()})
}

func postPreview() string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var post = new(Post)
	post.FromParams(ctx.Params)
	return RenderPost(post)
}

func postDelete(slug string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	db.Cursor(&Post{}).Remove(M{"slug": slug})
	referer := ctx.Request.Header.Get("referer")
	if len(referer) == 0 {
		referer = "/admin/"
	}
	ctx.Redirect(302, referer)
	return ""
}

// *** Pages ***

func pageAdd() string {
	if app.RequireAuthentication(ctx) {
		return ""
	}

	if ctx.Request.Method == "GET" {
		ctx.Params["Url"] = strings.TrimLeft(ctx.Params["Url"], "/")
		return adminBase.Render("blog/admin/pages-edit.mandira", ctx.Params)
	}

	var page = new(Page)
	page.FromParams(ctx.Params)
	db.Upsert(page)
	ctx.Redirect(302, "/admin/")
	return ""
	//ctx.Redirect(302, "/admin/posts/edit/" + post.Slug + "/")
}

func pageEdit(url string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var page *Page
	err := db.Find(page, M{"url": url}).One(&page)
	if err != nil {
		fmt.Println(err)
		ctx.Redirect(302, "/admin/")
		return ""
	}
	if len(ctx.Params) > 1 {
		page.FromParams(ctx.Params)
		db.Upsert(page)
	}
	return adminBase.Render("blog/admin/pages-edit.mandira", page)
}

func pagePreview() string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	var page = new(Page)
	page.FromParams(ctx.Params)
	return template.RenderMarkdown(page.Content)
}

func pageDelete(url string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}
	db.Cursor(&Page{}).Remove(M{"url": url})
	referer := ctx.Request.Header.Get("referer")
	if len(referer) == 0 {
		referer = "/admin/"
	}
	ctx.Redirect(302, referer)
	return ""
}

func pageList(page string) string {
	if app.RequireAuthentication(ctx) {
		return ""
	}

	num := app.PageNumber(page)
	paginator := app.NewPaginator(num, listPageSize)
	paginator.Link = "/admin/pages/"
	sort := "url"

	var p *Page
	var pages []Page
	// do a search, if required, of title and content
	var err error
	var numObjects int

	if len(ctx.Params["Search"]) > 0 {
		term := M{"$regex": ctx.Params["Search"]}
		search := M{"$or": []M{M{"url": term}, M{"content": term}}}
		err = db.Find(p, search).Sort(sort).Skip(paginator.Skip).Limit(listPageSize).All(&pages)
		numObjects, _ = db.Find(p, search).Count()
	} else {
		err = db.Find(p, nil).Sort(sort).Skip(paginator.Skip).Limit(listPageSize).All(&pages)
		numObjects, _ = db.Cursor(p).Count()
	}

	if err != nil {
		fmt.Println(err)
	}

	return adminBase.Render("blog/admin/page-list.mandira", M{
		"Pages": pages, "Pagination": paginator.Render(numObjects)})
}

// *** Panels ***

type PostPanel struct{}

func (pp *PostPanel) Render() string {
	var posts []Post
	db.Latest(&Post{}, M{"published": 1}).Limit(indexListSize).All(&posts)
	return template.Render("blog/admin/posts-panel.mandira", M{
		"posts": posts,
	})
}

type UnpublishedPanel struct{}

func (up *UnpublishedPanel) Render() string {
	var posts []Post
	db.Latest(&Post{}, M{"published": 0}).Limit(indexListSize).All(&posts)
	return template.Render("blog/admin/unpublished-panel.mandira", M{
		"posts": posts,
	})
}

type PagesPanel struct{}

func (pp *PagesPanel) Render() string {
	var pages []Page
	db.Find(&Page{}, nil).Limit(indexListSize).All(&pages)
	return template.Render("blog/admin/pages-panel.mandira", M{
		"pages": pages,
	})
}

*/

// uploadFile handles file uploads for blog posts
func (a *Admin) uploadFile(w http.ResponseWriter, r *http.Request) {
	postIdStr := chi.URLParam(r, "postId")
	postId, err := strconv.ParseUint(postIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid post ID", http.StatusBadRequest)
		return
	}

	// Use uploads package as library with "blog-files" filesystem
	upload, err := a.handleFileUpload(r, "blog-files")
	if err != nil {
		slog.Error("Failed to upload file", "err", err)
		http.Error(w, "Failed to upload file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Attach file to post
	postService := NewPostService(a.db)
	if err := postService.AttachFile(postId, upload.ID); err != nil {
		slog.Error("Failed to attach file to post", "post_id", postId, "upload_id", upload.ID, "err", err)
		http.Error(w, "Failed to attach file to post", http.StatusInternalServerError)
		return
	}

	// Get file URL for response
	mapper := a.registry.Mapper()
	fileURL := ""
	if urlPath, err := mapper.GetURL("blog-files", upload.Filename); err == nil {
		fileURL = urlPath
	}

	// Return success with the file info
	response := map[string]interface{}{
		"success":    true,
		"id":         upload.ID,
		"filename":   upload.Filename,
		"size":       upload.Size,
		"created_at": upload.CreatedAt,
		"url":        fileURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleFileUpload processes a file upload using uploads package as a library
func (a *Admin) handleFileUpload(r *http.Request, filesystemName string) (*uploads.Upload, error) {
	// Parse the multipart form
	err := r.ParseMultipartForm(64 << 20) // 64MiB max
	if err != nil {
		return nil, fmt.Errorf("failed to parse form: %w", err)
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("failed to get file from form: %w", err)
	}
	defer file.Close()

	// Clean the filename
	filename := filepath.Base(header.Filename)
	if filename == "" || filename == "." {
		return nil, fmt.Errorf("invalid filename")
	}

	// Get the filesystem
	fs, err := a.registry.Get(filesystemName)
	if err != nil {
		return nil, fmt.Errorf("failed to get filesystem: %w", err)
	}

	// Get the base path - the fs.FS type is read-only, to be writable, it needs to be a PathFS
	var basePath string
	if pfs, ok := fs.(vfs.PathFS); ok {
		basePath = pfs.Path()
	} else {
		return nil, fmt.Errorf("filesystem %s is not writable", filesystemName)
	}

	// Create the full path for the new file
	destPath := filepath.Join(basePath, filename)

	// Create the destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer destFile.Close()

	// Copy the uploaded file to the destination and track size
	bytesWritten, err := io.Copy(destFile, file)
	if err != nil {
		return nil, fmt.Errorf("failed to save file: %w", err)
	}

	// Create database record with file size using uploads service
	uploadService := uploads.NewUploadService(a.db)
	upload, err := uploadService.Create(filesystemName, filename, bytesWritten)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload record: %w", err)
	}

	return upload, nil
}

// deleteAttachedFile removes a file attachment from a blog post
func (a *Admin) deleteAttachedFile(w http.ResponseWriter, r *http.Request) {
	postIdStr := chi.URLParam(r, "postId")
	uploadIdStr := chi.URLParam(r, "uploadId")
	
	postId, err := strconv.ParseUint(postIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid post ID", http.StatusBadRequest)
		return
	}
	
	uploadId, err := strconv.ParseUint(uploadIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid upload ID", http.StatusBadRequest)
		return
	}

	// Get upload info first for file deletion
	uploadService := uploads.NewUploadService(a.db)
	upload, err := uploadService.GetByID(uploadId)
	if err != nil {
		slog.Error("Failed to get upload record", "upload_id", uploadId, "err", err)
		http.Error(w, "Upload not found", http.StatusNotFound)
		return
	}

	// Detach file from post first
	postService := NewPostService(a.db)
	if err := postService.DetachFile(postId, uploadId); err != nil {
		slog.Error("Failed to detach file from post", "post_id", postId, "upload_id", uploadId, "err", err)
		http.Error(w, "Failed to detach file", http.StatusInternalServerError)
		return
	}

	// Delete the actual file from filesystem
	if err := a.deleteFile(upload.FilesystemName, upload.Filename); err != nil {
		slog.Warn("Failed to delete file from filesystem", "filesystem", upload.FilesystemName, "filename", upload.Filename, "err", err)
		// Don't fail the request - the detachment succeeded
	}

	// Delete the upload record
	if err := uploadService.Delete(uploadId); err != nil {
		slog.Warn("Failed to delete upload record", "upload_id", uploadId, "err", err)
		// Don't fail the request - the detachment succeeded
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// deleteFile removes a file from the specified filesystem
func (a *Admin) deleteFile(filesystemName, filename string) error {
	// Get the filesystem
	fs, err := a.registry.Get(filesystemName)
	if err != nil {
		return fmt.Errorf("failed to get filesystem: %w", err)
	}

	// Get the base path
	var basePath string
	if pfs, ok := fs.(vfs.PathFS); ok {
		basePath = pfs.Path()
	} else {
		return fmt.Errorf("filesystem %s is not writable", filesystemName)
	}

	// Delete the file
	filePath := filepath.Join(basePath, filename)
	return os.Remove(filePath)
}
