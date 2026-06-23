package cache

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// errNoParts and errPartMismatch surface finalize-time validation failures.
var (
	errNoParts      = errors.New("no parts uploaded")
	errPartMismatch = errors.New("uploaded part count mismatch")
)

// store holds cache metadata (sqlite) and blob parts (filesystem). It mirrors
// the upload→finalize→download lifecycle of the reference implementation, with a
// simplified schema: the storage location is folded into the cache entry and the
// folder name equals the numeric upload id.
type store struct {
	db   *sql.DB
	root string // filesystem root for blob parts
}

// cacheEntry is a finalized cache record.
type cacheEntry struct {
	ID        string // == folder name (numeric upload id as string)
	Key       string
	PartCount int
}

func openStore(ctx context.Context, dbPath, blobRoot string) (*store, error) {
	if err := os.MkdirAll(blobRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create blob root: %w", err)
	}
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // sqlite: serialize writers to avoid SQLITE_BUSY
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}
	s := &store{db: db, root: blobRoot}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *store) Close() error { return s.db.Close() }

// isNoRows reports whether err is the "upload not found" sentinel.
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func (s *store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS uploads (
			id             INTEGER PRIMARY KEY,
			key            TEXT NOT NULL,
			version        TEXT NOT NULL,
			scope          TEXT NOT NULL,
			repo_id        TEXT NOT NULL,
			started_parts  INTEGER NOT NULL DEFAULT 0,
			finished_parts INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_uploads_lookup ON uploads(key, version, scope, repo_id)`,
		`CREATE TABLE IF NOT EXISTS cache_entries (
			id           TEXT PRIMARY KEY,
			key          TEXT NOT NULL,
			version      TEXT NOT NULL,
			scope        TEXT NOT NULL,
			repo_id      TEXT NOT NULL,
			part_count   INTEGER NOT NULL,
			updated_at   INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_lookup ON cache_entries(key, version, scope, repo_id)`,
	}
	for _, st := range stmts {
		if _, err := s.db.ExecContext(ctx, st); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// Add last_used_at to pre-existing DBs (ignore "duplicate column"), then seed
	// any unset values from updated_at so age-based GC has a sane baseline.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE cache_entries ADD COLUMN last_used_at INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migrate last_used_at: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE cache_entries SET last_used_at=updated_at WHERE last_used_at=0`); err != nil {
		return fmt.Errorf("migrate seed last_used_at: %w", err)
	}
	return nil
}

// touchEntry records that an entry was just served, for LRU/age-based GC.
func (s *store) touchEntry(ctx context.Context, id string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE cache_entries SET last_used_at=? WHERE id=?`, nowMillis(), id)
}

// evict removes finalized entries (DB row + blob folder) that are unused. It
// drops entries last used before olderThan (if >0), then, if the remaining blob
// total still exceeds maxBytes (if >0), removes least-recently-used entries until
// it fits. Returns the number of entries removed.
func (s *store) evict(ctx context.Context, olderThan int64, maxBytes int64) (int, error) {
	removed := 0
	if olderThan > 0 {
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM cache_entries WHERE last_used_at < ?`, olderThan)
		if err != nil {
			return removed, err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return removed, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if err := s.deleteEntry(ctx, id); err != nil {
				return removed, err
			}
			removed++
		}
	}

	if maxBytes > 0 {
		total, err := s.totalBlobSize()
		if err != nil {
			return removed, err
		}
		if total > maxBytes {
			rows, err := s.db.QueryContext(ctx, `SELECT id FROM cache_entries ORDER BY last_used_at ASC`)
			if err != nil {
				return removed, err
			}
			var ids []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return removed, err
				}
				ids = append(ids, id)
			}
			rows.Close()
			for _, id := range ids {
				if total <= maxBytes {
					break
				}
				sz, _ := s.folderSize(id)
				if err := s.deleteEntry(ctx, id); err != nil {
					return removed, err
				}
				total -= sz
				removed++
			}
		}
	}
	return removed, nil
}

// deleteEntry removes one entry's DB row and its blob folder.
func (s *store) deleteEntry(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM cache_entries WHERE id=?`, id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.root, id))
}

// folderSize returns the on-disk size of one entry's blob folder.
func (s *store) folderSize(id string) (int64, error) {
	var total int64
	err := filepath.Walk(filepath.Join(s.root, id), func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// totalBlobSize returns the on-disk size of all blob folders.
func (s *store) totalBlobSize() (int64, error) {
	var total int64
	err := filepath.Walk(s.root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// createUpload reserves an upload. Returns the upload id, or ok=false if an
// upload for the same key+version+scope+repo already exists (dedupe).
func (s *store) createUpload(ctx context.Context, key, version, scope, repoID string) (int64, bool, error) {
	var existing int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM uploads WHERE key=? AND version=? AND scope=? AND repo_id=?`,
		key, version, scope, repoID).Scan(&existing)
	if err == nil {
		return 0, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	id, err := randomID()
	if err != nil {
		return 0, false, err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO uploads (id, key, version, scope, repo_id) VALUES (?,?,?,?,?)`,
		id, key, version, scope, repoID); err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// uploadPart streams one chunk to <root>/<uploadId>/parts/<index>.
func (s *store) uploadPart(ctx context.Context, uploadID int64, partIndex int, r io.Reader) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM uploads WHERE id=?`, uploadID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // unknown upload: ignore, matching reference behavior
		}
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE uploads SET started_parts=started_parts+1 WHERE id=?`, uploadID); err != nil {
		return err
	}

	partPath := s.partPath(strconv.FormatInt(uploadID, 10), partIndex)
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(partPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `UPDATE uploads SET finished_parts=finished_parts+1 WHERE id=?`, uploadID)
	return err
}

// completeUpload validates parts and promotes the upload to a cache entry,
// replacing any prior entry for the same key+version+scope+repo. Returns the
// finalized entry id (the folder name).
func (s *store) completeUpload(ctx context.Context, key, version, scope, repoID string) (string, error) {
	var up struct {
		id           int64
		started, fin int
	}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, started_parts, finished_parts FROM uploads WHERE key=? AND version=? AND scope=? AND repo_id=?`,
		key, version, scope, repoID).Scan(&up.id, &up.started, &up.fin)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", err
	}

	folder := strconv.FormatInt(up.id, 10)
	if up.fin == 0 {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM uploads WHERE id=?`, up.id)
		return "", errNoParts
	}
	if up.started != up.fin {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM uploads WHERE id=?`, up.id)
		return "", fmt.Errorf("%w: %d of %d", errPartMismatch, up.fin, up.started)
	}
	actual, err := s.countParts(folder)
	if err != nil {
		return "", err
	}
	if actual != up.fin {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM uploads WHERE id=?`, up.id)
		return "", fmt.Errorf("%w: expected %d found %d", errPartMismatch, up.fin, actual)
	}
	if err := s.requireContiguousParts(folder, up.fin); err != nil {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM uploads WHERE id=?`, up.id)
		return "", err
	}

	// Replace any existing entry's blob folder before repointing.
	var oldFolder string
	err = s.db.QueryRowContext(ctx,
		`SELECT id FROM cache_entries WHERE key=? AND version=? AND scope=? AND repo_id=?`,
		key, version, scope, repoID).Scan(&oldFolder)
	switch {
	case err == nil:
		if _, err := s.db.ExecContext(ctx,
			`UPDATE cache_entries SET id=?, part_count=?, updated_at=?, last_used_at=? WHERE key=? AND version=? AND scope=? AND repo_id=?`,
			folder, actual, nowMillis(), nowMillis(), key, version, scope, repoID); err != nil {
			return "", err
		}
		if oldFolder != "" && oldFolder != folder {
			_ = os.RemoveAll(filepath.Join(s.root, oldFolder))
		}
	case errors.Is(err, sql.ErrNoRows):
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO cache_entries (id, key, version, scope, repo_id, part_count, updated_at, last_used_at) VALUES (?,?,?,?,?,?,?,?)`,
			folder, key, version, scope, repoID, actual, nowMillis(), nowMillis()); err != nil {
			return "", err
		}
	default:
		return "", err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM uploads WHERE id=?`, up.id); err != nil {
		return "", err
	}
	return folder, nil
}

// matchEntry implements the key/restore-keys/version lookup with strict
// scope+repo isolation, scopes tried in the given priority order.
func (s *store) matchEntry(ctx context.Context, keys []string, version string, scopes []string, repoID string) (*cacheEntry, bool, error) {
	if len(keys) == 0 {
		return nil, false, nil
	}
	primary, restore := keys[0], keys[1:]
	for _, scope := range scopes {
		// 1. exact primary
		if e, ok, err := s.queryExact(ctx, primary, version, scope, repoID); err != nil || ok {
			return s.touched(ctx, e, ok, err)
		}
		// 2. prefix primary (newest)
		if e, ok, err := s.queryPrefix(ctx, primary, version, scope, repoID); err != nil || ok {
			return s.touched(ctx, e, ok, err)
		}
		// 3. restore keys: exact then prefix, in order
		for _, rk := range restore {
			if e, ok, err := s.queryExact(ctx, rk, version, scope, repoID); err != nil || ok {
				return s.touched(ctx, e, ok, err)
			}
			if e, ok, err := s.queryPrefix(ctx, rk, version, scope, repoID); err != nil || ok {
				return s.touched(ctx, e, ok, err)
			}
		}
	}
	return nil, false, nil
}

// touched bumps the entry's last-used time on a successful match, so GC treats a
// restore as recent activity.
func (s *store) touched(ctx context.Context, e *cacheEntry, ok bool, err error) (*cacheEntry, bool, error) {
	if err == nil && ok && e != nil {
		s.touchEntry(ctx, e.ID)
	}
	return e, ok, err
}

func (s *store) queryExact(ctx context.Context, key, version, scope, repoID string) (*cacheEntry, bool, error) {
	var e cacheEntry
	err := s.db.QueryRowContext(ctx,
		`SELECT id, key, part_count FROM cache_entries
		 WHERE key=? AND version=? AND scope=? AND repo_id=?
		 ORDER BY updated_at DESC LIMIT 1`,
		key, version, scope, repoID).Scan(&e.ID, &e.Key, &e.PartCount)
	return scanEntry(e, err)
}

func (s *store) queryPrefix(ctx context.Context, key, version, scope, repoID string) (*cacheEntry, bool, error) {
	var e cacheEntry
	err := s.db.QueryRowContext(ctx,
		`SELECT id, key, part_count FROM cache_entries
		 WHERE key LIKE ? ESCAPE '\' AND version=? AND scope=? AND repo_id=?
		 ORDER BY updated_at DESC LIMIT 1`,
		escapeLike(key)+"%", version, scope, repoID).Scan(&e.ID, &e.Key, &e.PartCount)
	return scanEntry(e, err)
}

func scanEntry(e cacheEntry, err error) (*cacheEntry, bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &e, true, nil
}

// getEntry returns a finalized entry by its id (folder name).
func (s *store) getEntry(ctx context.Context, id string) (*cacheEntry, bool, error) {
	var e cacheEntry
	err := s.db.QueryRowContext(ctx,
		`SELECT id, key, part_count FROM cache_entries WHERE id=?`, id).Scan(&e.ID, &e.Key, &e.PartCount)
	return scanEntry(e, err)
}

// openDownload returns a reader that concatenates the entry's parts in order.
func (s *store) openDownload(ctx context.Context, id string) (io.ReadCloser, error) {
	e, ok, err := s.getEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, os.ErrNotExist
	}
	return &partsReader{root: s.root, folder: id, partCount: e.PartCount}, nil
}

func (s *store) partPath(folder string, index int) string {
	return filepath.Join(s.root, folder, "parts", strconv.Itoa(index))
}

func (s *store) countParts(folder string) (int, error) {
	dir := filepath.Join(s.root, folder, "parts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, ent := range entries {
		if !ent.IsDir() {
			n++
		}
	}
	return n, nil
}

func (s *store) requireContiguousParts(folder string, count int) error {
	for i := 0; i < count; i++ {
		if _, err := os.Stat(s.partPath(folder, i)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: missing part %d", errPartMismatch, i)
			}
			return err
		}
	}
	return nil
}

// partsReader streams parts 0..partCount-1 of a folder as one continuous stream.
type partsReader struct {
	root      string
	folder    string
	partCount int
	idx       int
	cur       *os.File
}

func (p *partsReader) Read(b []byte) (int, error) {
	for {
		if p.cur == nil {
			if p.idx >= p.partCount {
				return 0, io.EOF
			}
			f, err := os.Open(filepath.Join(p.root, p.folder, "parts", strconv.Itoa(p.idx)))
			if err != nil {
				return 0, err
			}
			p.cur = f
		}
		n, err := p.cur.Read(b)
		if errors.Is(err, io.EOF) {
			p.cur.Close()
			p.cur = nil
			p.idx++
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (p *partsReader) Close() error {
	if p.cur != nil {
		err := p.cur.Close()
		p.cur = nil
		return err
	}
	return nil
}

// escapeLike escapes LIKE wildcards using backslash as the escape char.
func escapeLike(v string) string {
	out := make([]byte, 0, len(v))
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c == '\\' || c == '%' || c == '_' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	return string(out)
}

// randomID returns a positive 63-bit id used as the numeric upload/folder id.
func randomID() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return 0, err
	}
	return n.Int64() + 1, nil
}
