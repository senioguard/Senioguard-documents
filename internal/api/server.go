package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"senioguard-documents/internal/config"
	"senioguard-documents/internal/extractor"
	"senioguard-documents/internal/model"
	"senioguard-documents/internal/module"
	"senioguard-documents/internal/processor"
	"senioguard-documents/internal/rag"
	"senioguard-documents/internal/repository"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Server struct {
	cfg       config.Config
	repos     *repository.Repositories
	storage   module.Storage
	processor *processor.Processor
	rag       rag.Service
	vectorDB  module.VectorDB
	sources   map[string]module.SourceConnector
	templates *template.Template
	mux       *http.ServeMux
}

func New(cfg config.Config, repos *repository.Repositories, storage module.Storage, processor *processor.Processor, rag rag.Service, vectorDB module.VectorDB, sources []module.SourceConnector, templates *template.Template) *Server {
	sourceMap := map[string]module.SourceConnector{}
	for _, connector := range sources {
		sourceMap[connector.Name()] = connector
	}
	s := &Server{cfg: cfg, repos: repos, storage: storage, processor: processor, rag: rag, vectorDB: vectorDB, sources: sourceMap, templates: templates, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", s.health)
	s.mux.HandleFunc("/login", s.login)
	s.mux.HandleFunc("/logout", s.logout)
	s.mux.HandleFunc("/", s.auth(s.app))
	s.mux.HandleFunc("/ui/", s.auth(s.ui))
	s.mux.HandleFunc("/api/", s.auth(s.api))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) app(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/app" {
		http.NotFound(w, r)
		return
	}
	s.render(w, "app.html", nil)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.render(w, "login.html", nil)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.FormValue("username") != s.cfg.AuthUser || r.FormValue("password") != s.cfg.AuthPassword {
		s.render(w, "login.html", map[string]string{"Error": "Invalid credentials"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "docmanager_session", Value: s.signSession(s.cfg.AuthUser), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(24 * time.Hour)})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "docmanager_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || r.URL.Path == "/healthz" {
			next(w, r)
			return
		}
		cookie, err := r.Cookie("docmanager_session")
		if err != nil || !s.validSession(cookie.Value) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (s *Server) ui(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/ui/")
	switch {
	case path == "empty":
		s.render(w, "empty.html", nil)
	case path == "rag-panel":
		s.render(w, "rag-panel.html", nil)
	case path == "tree":
		s.renderTree(w, r)
	case strings.HasPrefix(path, "documents/"):
		id, err := primitive.ObjectIDFromHex(strings.TrimPrefix(path, "documents/"))
		if err != nil {
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}
		doc, err := s.repos.Documents.Get(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.render(w, "document.html", map[string]any{"Document": doc})
	case path == "rag":
		question := r.FormValue("question")
		topK, _ := strconv.Atoi(r.FormValue("topK"))
		documentIDs := parseObjectIDList(r.FormValue("documentIds"))
		if docID := parseOptionalObjectID(r.FormValue("documentId")); docID != nil {
			documentIDs = append(documentIDs, *docID)
		}
		response, err := s.rag.Query(r.Context(), question, nil, documentIDs, topK)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.render(w, "rag-answer.html", response)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) renderTree(w http.ResponseWriter, r *http.Request) {
	collections, err := s.repos.Collections.ListAll(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	docs, err := s.repos.Documents.ListAll(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type treeNode struct {
		model.Collection
		Children  []*treeNode
		Documents []model.Document
	}
	root := &treeNode{}
	nodes := map[string]*treeNode{}
	for _, collection := range collections {
		collection := collection
		nodes[collection.ID.Hex()] = &treeNode{Collection: collection}
	}
	for _, collection := range collections {
		node := nodes[collection.ID.Hex()]
		if collection.ParentID == nil {
			root.Children = append(root.Children, node)
			continue
		}
		parent := nodes[collection.ParentID.Hex()]
		if parent == nil {
			root.Children = append(root.Children, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}
	for _, doc := range docs {
		if doc.CollectionID == nil {
			root.Documents = append(root.Documents, doc)
			continue
		}
		node := nodes[doc.CollectionID.Hex()]
		if node == nil {
			root.Documents = append(root.Documents, doc)
			continue
		}
		node.Documents = append(node.Documents, doc)
	}
	s.render(w, "tree.html", map[string]any{"Root": root})
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}
	switch {
	case len(parts) == 1 && parts[0] == "collections":
		s.collections(w, r)
	case len(parts) >= 2 && parts[0] == "collections":
		s.collectionRoutes(w, r, parts)
	case len(parts) >= 2 && parts[0] == "documents":
		s.documentRoutes(w, r, parts)
	case len(parts) == 2 && parts[0] == "rag" && parts[1] == "query":
		s.ragQuery(w, r)
	case len(parts) == 2 && parts[0] == "rag" && parts[1] == "save":
		s.saveRAGAnswer(w, r)
	case len(parts) == 3 && parts[0] == "sources" && parts[2] == "sync":
		s.syncSource(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "search":
		s.search(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) syncSource(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	connector, ok := s.sources[name]
	if !ok {
		if acceptsHTML(r) {
			s.render(w, "source-sync-error.html", map[string]string{
				"Source":  name,
				"Message": "This source connector is not configured. Set GITHUB_ENABLED=true and GITHUB_REPOS in .env, then restart the app.",
			})
			return
		}
		http.Error(w, "source connector is not configured", http.StatusNotFound)
		return
	}
	selection := strings.TrimSpace(r.FormValue("selection"))
	if selection == "" {
		selection = strings.TrimSpace(r.FormValue("repo"))
	}
	var result module.SourceSyncResult
	var err error
	if selection != "" {
		selective, ok := connector.(module.SelectiveSourceConnector)
		if !ok {
			err = fmt.Errorf("%s does not support manual selections", name)
		} else {
			result, err = selective.SyncSelection(r.Context(), selection)
		}
	} else {
		result, err = connector.Sync(r.Context())
	}
	if acceptsHTML(r) {
		if err != nil {
			s.render(w, "source-sync-error.html", map[string]string{"Source": name, "Message": err.Error()})
			return
		}
		s.render(w, "source-sync.html", result)
		return
	}
	respond(w, result, err)
}

func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.repos.Collections.ListChildren(r.Context(), nil)
		respond(w, items, err)
	case http.MethodPost:
		name := formOrJSON(r, "name")
		parent := parseOptionalObjectID(formOrJSON(r, "parentId"))
		item, err := s.repos.Collections.Create(r.Context(), name, parent)
		if acceptsHTML(r) {
			s.renderTree(w, r)
			return
		}
		respond(w, item, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) collectionRoutes(w http.ResponseWriter, r *http.Request, parts []string) {
	id := parseRouteID(parts[1])
	if id == nil && parts[1] != "root" {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if len(parts) == 2 {
		if id == nil {
			items, err := s.repos.Collections.ListChildren(r.Context(), nil)
			respond(w, items, err)
			return
		}
		switch r.Method {
		case http.MethodGet:
			item, err := s.repos.Collections.Get(r.Context(), *id)
			respond(w, item, err)
		case http.MethodPut:
			item, err := s.repos.Collections.Update(r.Context(), *id, formOrJSON(r, "name"))
			if acceptsHTML(r) {
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				s.renderTree(w, r)
				return
			}
			respond(w, item, err)
		case http.MethodDelete:
			err := s.repos.Collections.Delete(r.Context(), *id)
			if acceptsHTML(r) {
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				s.renderTree(w, r)
				return
			}
			respond(w, map[string]string{"status": "deleted"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	switch parts[2] {
	case "children":
		items, err := s.repos.Collections.ListChildren(r.Context(), id)
		respond(w, items, err)
	case "documents":
		if len(parts) == 4 && parts[3] == "zip" {
			s.uploadZip(w, r, id)
			return
		}
		if r.Method == http.MethodPost {
			s.uploadDocuments(w, r, id)
			return
		}
		docs, err := s.repos.Documents.ListByCollection(r.Context(), id)
		respond(w, docs, err)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) documentRoutes(w http.ResponseWriter, r *http.Request, parts []string) {
	id, err := primitive.ObjectIDFromHex(parts[1])
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			doc, err := s.repos.Documents.Get(r.Context(), id)
			respond(w, doc, err)
		case http.MethodDelete:
			doc, err := s.repos.Documents.Get(r.Context(), id)
			if err == nil {
				_ = s.storage.Delete(r.Context(), doc.StorageKey)
				_ = s.vectorDB.DeleteDocument(r.Context(), id)
				_ = s.repos.Documents.Delete(r.Context(), id)
			}
			if acceptsHTML(r) {
				s.render(w, "empty.html", nil)
				return
			}
			respond(w, map[string]string{"status": "deleted"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	switch parts[2] {
	case "content":
		doc, err := s.repos.Documents.Get(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(doc.Text))
	case "process":
		s.processor.Enqueue(id)
		if acceptsHTML(r) {
			doc, _ := s.repos.Documents.Get(r.Context(), id)
			s.render(w, "document.html", map[string]any{"Document": doc})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
	case "move":
		collectionID := parseOptionalObjectID(formOrJSON(r, "collectionId"))
		respond(w, map[string]string{"status": "moved"}, s.repos.Documents.Move(r.Context(), id, collectionID))
	case "metadata":
		s.documentMetadata(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) documentMetadata(w http.ResponseWriter, r *http.Request, id primitive.ObjectID) {
	key := formOrJSON(r, "key")
	var err error
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		err = s.repos.Documents.SetMetadata(r.Context(), id, key, formOrJSON(r, "value"))
	case http.MethodDelete:
		err = s.repos.Documents.DeleteMetadata(r.Context(), id, key)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	doc, err := s.repos.Documents.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if acceptsHTML(r) {
		s.render(w, "document.html", map[string]any{"Document": doc})
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) uploadDocuments(w http.ResponseWriter, r *http.Request, collectionID *primitive.ObjectID) {
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	files := r.MultipartForm.File["files"]
	created := make([]model.Document, 0, len(files))
	for _, fh := range files {
		doc, err := s.saveUpload(r.Context(), collectionID, fh.Filename, fh.Header.Get("Content-Type"), fh.Size, func() (io.ReadCloser, error) { return fh.Open() })
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		created = append(created, doc)
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) uploadZip(w http.ResponseWriter, r *http.Request, collectionID *primitive.ObjectID) {
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, headers := range r.MultipartForm.File {
		for _, fh := range headers {
			file, err := fh.Open()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			data, err := io.ReadAll(file)
			file.Close()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var created []model.Document
			for _, zf := range zr.File {
				if zf.FileInfo().IsDir() {
					continue
				}
				rc, err := zf.Open()
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				content, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				doc, err := s.saveUpload(r.Context(), collectionID, zf.Name, "", int64(len(content)), func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(content)), nil })
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				created = append(created, doc)
			}
			writeJSON(w, http.StatusCreated, created)
			return
		}
	}
	http.Error(w, "zip file is required", http.StatusBadRequest)
}

func (s *Server) saveUpload(ctx context.Context, collectionID *primitive.ObjectID, filename, declaredMIME string, size int64, open func() (io.ReadCloser, error)) (model.Document, error) {
	id := primitive.NewObjectID()
	targetCollection, name, err := s.collectionForPath(ctx, collectionID, filename)
	if err != nil {
		return model.Document{}, err
	}
	mime := extractor.DetectMIME(name, declaredMIME)
	key := fmt.Sprintf("documents/%s/%s", id.Hex(), name)
	rc, err := open()
	if err != nil {
		return model.Document{}, err
	}
	defer rc.Close()
	if err := s.storage.Upload(ctx, key, rc, size, mime); err != nil {
		return model.Document{}, err
	}
	doc, err := s.repos.Documents.Create(ctx, model.Document{ID: id, CollectionID: targetCollection, DisplayName: name, StorageKey: key, MIME: mime, Size: size})
	if err != nil {
		return model.Document{}, err
	}
	s.processor.Enqueue(doc.ID)
	return doc, nil
}

func (s *Server) collectionForPath(ctx context.Context, root *primitive.ObjectID, filename string) (*primitive.ObjectID, string, error) {
	cleaned := strings.Trim(filepath.ToSlash(filepath.Clean(filename)), "/")
	parts := strings.Split(cleaned, "/")
	if len(parts) == 0 {
		return root, "document", nil
	}
	name := parts[len(parts)-1]
	parent := root
	for _, folder := range parts[:len(parts)-1] {
		if folder == "" || folder == "." {
			continue
		}
		existing, err := s.repos.Collections.FindChild(ctx, parent, folder)
		if err != nil {
			created, createErr := s.repos.Collections.Create(ctx, folder, parent)
			if createErr != nil {
				return nil, "", createErr
			}
			existing = created
		}
		id := existing.ID
		parent = &id
	}
	return parent, filepath.Base(name), nil
}

func (s *Server) ragQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question     string   `json:"question"`
		CollectionID string   `json:"collectionId"`
		DocumentIDs  []string `json:"documentIds"`
		TopK         int      `json:"topK"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	documentIDs := make([]primitive.ObjectID, 0, len(req.DocumentIDs))
	for _, value := range req.DocumentIDs {
		if id := parseOptionalObjectID(value); id != nil {
			documentIDs = append(documentIDs, *id)
		}
	}
	response, err := s.rag.Query(r.Context(), req.Question, parseOptionalObjectID(req.CollectionID), documentIDs, req.TopK)
	respond(w, response, err)
}

func (s *Server) saveRAGAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	title := strings.TrimSpace(formOrJSON(r, "title"))
	if title == "" {
		title = "RAG answer"
	}
	if !strings.HasSuffix(strings.ToLower(title), ".md") {
		title += ".md"
	}
	content := strings.TrimSpace(formOrJSON(r, "content"))
	if content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	id := primitive.NewObjectID()
	key := fmt.Sprintf("generated/rag/%s/%s", id.Hex(), title)
	if err := s.storage.Upload(r.Context(), key, strings.NewReader(content), int64(len(content)), "text/markdown"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	doc, err := s.repos.Documents.Create(r.Context(), model.Document{
		ID:          id,
		DisplayName: title,
		StorageKey:  key,
		MIME:        "text/markdown",
		Size:        int64(len(content)),
		Source:      "rag",
		SourceType:  "answer",
		Metadata: map[string]string{
			"generatedBy": "RAG",
		},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.processor.Enqueue(doc.ID)
	if acceptsHTML(r) {
		s.render(w, "rag-save.html", map[string]any{"Document": doc})
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	collectionID := parseOptionalObjectID(r.URL.Query().Get("collectionId"))
	docs, err := s.repos.Documents.Search(r.Context(), r.URL.Query().Get("q"), collectionID, 25)
	respond(w, docs, err)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) signSession(user string) string {
	exp := strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10)
	payload := user + "|" + exp
	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
}

func (s *Server) validSession(value string) bool {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return false
	}
	parts := strings.Split(string(data), "|")
	if len(parts) != 3 || parts[0] != s.cfg.AuthUser {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	payload := parts[0] + "|" + parts[1]
	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(parts[2]))
}

func respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func formOrJSON(r *http.Request, key string) string {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) == nil {
			return fmt.Sprint(body[key])
		}
	}
	_ = r.ParseForm()
	return r.FormValue(key)
}

func parseRouteID(value string) *primitive.ObjectID {
	if value == "root" || value == "" {
		return nil
	}
	id, err := primitive.ObjectIDFromHex(value)
	if err != nil {
		return nil
	}
	return &id
}

func parseOptionalObjectID(value string) *primitive.ObjectID {
	value = strings.TrimSpace(value)
	if value == "" || value == "root" || value == "<nil>" {
		return nil
	}
	id, err := primitive.ObjectIDFromHex(value)
	if err != nil {
		return nil
	}
	return &id
}

func parseObjectIDList(value string) []primitive.ObjectID {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	ids := make([]primitive.ObjectID, 0, len(parts))
	seen := map[primitive.ObjectID]bool{}
	for _, part := range parts {
		id := parseOptionalObjectID(part)
		if id == nil || seen[*id] {
			continue
		}
		seen[*id] = true
		ids = append(ids, *id)
	}
	return ids
}

func acceptsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html") || strings.Contains(r.Header.Get("HX-Request"), "true")
}
