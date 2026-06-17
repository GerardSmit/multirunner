// Package cache is a self-hosted GitHub Actions cache server implementing the
// v2 (twirp/JSON) CacheService protocol, so actions/cache stores blobs on this
// host instead of round-tripping to GitHub's Azure backend. The protocol and
// storage behavior are ported from falcondev-oss/github-actions-cache-server.
package cache

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GerardSmit/multirunner/internal/config"
)

// Server is the running cache HTTP server.
type Server struct {
	store          *store
	advertiseURL   string
	skipValidation bool
	proxy          *httputil.ReverseProxy
	httpSrv        *http.Server
	logger         *slog.Logger
	gitBundler     func(ctx context.Context, repoSlug string, w io.Writer) error
	gcInterval     time.Duration // 0 disables the GC sweep
	gcMaxAge       time.Duration // 0 disables age-based eviction
	gcMaxBytes     int64         // 0 disables size-based eviction
}

// SetGitBundler enables the dotgit-cache endpoint: GET /gitmirror/{owner}/{repo}
// streams a git bundle of the repo's host mirror.
func (s *Server) SetGitBundler(f func(ctx context.Context, repoSlug string, w io.Writer) error) {
	s.gitBundler = f
}

// New builds the cache server and opens its store.
func New(ctx context.Context, cfg config.Cache, logger *slog.Logger) (*Server, error) {
	if cfg.Storage != "" && cfg.Storage != "filesystem" {
		return nil, fmt.Errorf("cache storage %q not implemented (filesystem only)", cfg.Storage)
	}
	dbPath := filepath.Join(cfg.Path, "cache.db")
	blobRoot := filepath.Join(cfg.Path, "blobs")
	st, err := openStore(ctx, dbPath, blobRoot)
	if err != nil {
		return nil, err
	}

	s := &Server{
		store:          st,
		advertiseURL:   strings.TrimRight(cfg.AdvertiseURL, "/"),
		skipValidation: cfg.SkipTokenValidation,
		logger:         logger.With("component", "cache"),
	}
	if cfg.GCIntervalSec > 0 {
		s.gcInterval = time.Duration(cfg.GCIntervalSec) * time.Second
	}
	if cfg.MaxAgeDays > 0 {
		s.gcMaxAge = time.Duration(cfg.MaxAgeDays) * 24 * time.Hour
	}
	if cfg.MaxSizeGB > 0 {
		s.gcMaxBytes = int64(cfg.MaxSizeGB) << 30
	}

	if cfg.Upstream != "" {
		up, err := url.Parse(cfg.Upstream)
		if err != nil {
			st.Close()
			return nil, fmt.Errorf("cache upstream url: %w", err)
		}
		s.proxy = httputil.NewSingleHostReverseProxy(up)
	}

	s.httpSrv = &http.Server{Addr: cfg.Listen, Handler: s.routes()}
	return s, nil
}

// AdvertiseURL is the base URL runners should use to reach this cache.
func (s *Server) AdvertiseURL() string { return s.advertiseURL }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	const svc = "/twirp/github.actions.results.api.v1.CacheService/"
	mux.HandleFunc("POST "+svc+"CreateCacheEntry", s.handleCreateCacheEntry)
	mux.HandleFunc("POST "+svc+"GetCacheEntryDownloadURL", s.handleGetDownloadURL)
	mux.HandleFunc("POST "+svc+"FinalizeCacheEntryUpload", s.handleFinalize)
	mux.HandleFunc("PUT /devstoreaccount1/upload/{id}", s.handleUploadPut)
	mux.HandleFunc("PUT /upload/{id}", s.handleUploadPut)
	mux.HandleFunc("GET /download/{id}", s.handleDownload)
	mux.HandleFunc("GET /gitmirror/{owner}/{repo}", s.handleGitBundle)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("/", s.handleCatchAll)
	return s.logRequests(mux)
}

// handleGitBundle streams a git bundle of the requested repo's host mirror
// (dotgit-cache). The repo name may carry a ".git" suffix.
func (s *Server) handleGitBundle(w http.ResponseWriter, r *http.Request) {
	if s.gitBundler == nil {
		http.Error(w, "git cache not enabled", http.StatusNotFound)
		return
	}
	slug := r.PathValue("owner") + "/" + strings.TrimSuffix(r.PathValue("repo"), ".git")
	w.Header().Set("Content-Type", "application/octet-stream")
	if err := s.gitBundler(r.Context(), slug, w); err != nil {
		s.logger.Error("git bundle", "repo", slug, "err", err)
		// headers may already be sent; nothing more to do.
	}
}

// logRequests logs each incoming request so cache traffic is observable.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// runGC periodically evicts stale/oversized cache entries until ctx is cancelled.
func (s *Server) runGC(ctx context.Context) {
	if s.gcInterval == 0 || (s.gcMaxAge == 0 && s.gcMaxBytes == 0) {
		return
	}
	t := time.NewTicker(s.gcInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			var olderThan int64
			if s.gcMaxAge > 0 {
				olderThan = time.Now().Add(-s.gcMaxAge).UnixMilli()
			}
			n, err := s.store.evict(ctx, olderThan, s.gcMaxBytes)
			if err != nil {
				s.logger.Warn("cache gc failed", "err", err)
			} else if n > 0 {
				s.logger.Info("cache gc evicted entries", "count", n)
			}
		}
	}
}

// Start runs the server until ctx is cancelled, then shuts it down gracefully.
func (s *Server) Start(ctx context.Context) error {
	go s.runGC(ctx)
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("cache server listening", "addr", s.httpSrv.Addr, "advertise", s.advertiseURL)
		if err := s.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutCtx)
		return s.store.Close()
	case err := <-errCh:
		s.store.Close()
		return err
	}
}

// ---- twirp CacheService handlers ----

func (s *Server) handleCreateCacheEntry(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.scopeOrError(w, r)
	if !ok {
		return
	}
	var body struct {
		Key     string `json:"key"`
		Version string `json:"version"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	write := writeScope(sc.scopes)
	if write == "" {
		httpError(w, http.StatusForbidden, "no scope with write permission")
		return
	}
	id, created, err := s.store.createUpload(r.Context(), body.Key, body.Version, write, sc.repoID)
	if err != nil {
		s.serverError(w, "createUpload", err)
		return
	}
	if !created {
		writeJSON(w, map[string]any{"ok": false})
		return
	}
	writeJSON(w, map[string]any{
		"ok":                true,
		"signed_upload_url": fmt.Sprintf("%s/devstoreaccount1/upload/%d", s.baseURL(r), id),
	})
}

// baseURL returns the scheme+host the client used to reach this server, so the
// data-plane (upload/download) URLs we hand back are reachable by that same
// client — the configured advertise host (e.g. host.docker.internal) is not
// resolvable from a VM (which reaches us via the SLIRP alias) or from a
// container reached via the host gateway. Falls back to the advertise URL.
func (s *Server) baseURL(r *http.Request) string {
	if r.Host == "" {
		return s.advertiseURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleGetDownloadURL(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.scopeOrError(w, r)
	if !ok {
		return
	}
	var body struct {
		Key         string   `json:"key"`
		Version     string   `json:"version"`
		RestoreKeys []string `json:"restore_keys"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	keys := append([]string{body.Key}, body.RestoreKeys...)
	entry, found, err := s.store.matchEntry(r.Context(), keys, body.Version, scopeStringsByPermission(sc.scopes), sc.repoID)
	if err != nil {
		s.serverError(w, "matchEntry", err)
		return
	}
	if !found {
		writeJSON(w, map[string]any{"ok": false})
		return
	}
	writeJSON(w, map[string]any{
		"ok":                  true,
		"signed_download_url": fmt.Sprintf("%s/download/%s", s.baseURL(r), entry.ID),
		"matched_key":         entry.Key,
	})
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.scopeOrError(w, r)
	if !ok {
		return
	}
	var body struct {
		Key     string `json:"key"`
		Version string `json:"version"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	write := writeScope(sc.scopes)
	if write == "" {
		httpError(w, http.StatusForbidden, "no scope with write permission")
		return
	}
	id, err := s.store.completeUpload(r.Context(), body.Key, body.Version, write, sc.repoID)
	switch {
	case errors.Is(err, errNoParts), errors.Is(err, errPartMismatch):
		httpError(w, http.StatusBadRequest, err.Error())
		return
	case isNoRows(err):
		httpError(w, http.StatusNotFound, "upload not found")
		return
	case err != nil:
		s.serverError(w, "completeUpload", err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "entry_id": id})
}

// ---- Azure block upload data plane ----

func (s *Server) handleUploadPut(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid upload id")
		return
	}
	q := r.URL.Query()
	if q.Get("comp") == "blocklist" {
		// Block-list commit: the runner sends the ordered block ids; we track
		// part counts independently, so just acknowledge.
		w.Header().Set("x-ms-request-id", newRequestID())
		w.WriteHeader(http.StatusCreated)
		return
	}

	chunkIndex := 0
	if bid := q.Get("blockid"); bid != "" {
		idx, ok := chunkIndexFromBlockID(bid)
		if !ok {
			httpError(w, http.StatusBadRequest, "invalid block id")
			return
		}
		chunkIndex = idx
	}
	if err := s.store.uploadPart(r.Context(), id, chunkIndex, r.Body); err != nil {
		s.serverError(w, "uploadPart", err)
		return
	}
	w.Header().Set("x-ms-request-id", newRequestID())
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	rc, err := s.store.openDownload(r.Context(), r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		httpError(w, http.StatusNotFound, "cache entry not found")
		return
	}
	if err != nil {
		s.serverError(w, "openDownload", err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleCatchAll(w http.ResponseWriter, r *http.Request) {
	if s.proxy == nil {
		httpError(w, http.StatusNotFound, "not found")
		return
	}
	s.logger.Debug("proxying to upstream", "method", r.Method, "path", r.URL.Path)
	s.proxy.ServeHTTP(w, r)
}

// ---- auth / scopes ----

type scopeInfo struct {
	scopes []scopeEntry
	repoID string
}

type scopeEntry struct {
	Scope      string `json:"Scope"`
	Permission int    `json:"Permission"`
}

// scopeOrError extracts cache scopes from the bearer token. With validation
// skipped (default, since we own the runners) the JWT is decoded but not
// verified. A token without scopes falls back to a permissive default so
// non-Actions clients still work.
func (s *Server) scopeOrError(w http.ResponseWriter, r *http.Request) (scopeInfo, bool) {
	auth := r.Header.Get("Authorization")
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == auth { // no Bearer prefix
		token = ""
	}
	info := parseScopes(token)
	if len(info.scopes) == 0 {
		info.scopes = []scopeEntry{{Scope: "default", Permission: 3}}
	}
	if info.repoID == "" {
		info.repoID = "default"
	}
	return info, true
}

// parseScopes decodes the JWT payload (no signature verification) and reads the
// `ac` (cache scopes) and `repository_id` claims.
func parseScopes(token string) scopeInfo {
	var info scopeInfo
	if token == "" {
		return info
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return info
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return info
	}
	var claims struct {
		AC           string `json:"ac"`
		RepositoryID string `json:"repository_id"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return info
	}
	info.repoID = claims.RepositoryID
	if claims.AC != "" {
		_ = json.Unmarshal([]byte(claims.AC), &info.scopes)
	}
	return info
}

func writeScope(scopes []scopeEntry) string {
	for _, sc := range scopes {
		if sc.Permission >= 2 {
			return sc.Scope
		}
	}
	return ""
}

func scopeStringsByPermission(scopes []scopeEntry) []string {
	sorted := make([]scopeEntry, len(scopes))
	copy(sorted, scopes)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Permission > sorted[j].Permission })
	out := make([]string, len(sorted))
	for i, sc := range sorted {
		out[i] = sc.Scope
	}
	return out
}

// ---- helpers ----

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	http.Error(w, msg, code)
}

func (s *Server) serverError(w http.ResponseWriter, op string, err error) {
	s.logger.Error("cache server error", "op", op, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func newRequestID() string {
	id, err := randomID()
	if err != nil {
		return "00000000"
	}
	return strconv.FormatInt(id, 16)
}

func nowMillis() int64 { return time.Now().UnixMilli() }
