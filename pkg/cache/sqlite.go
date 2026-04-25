package cache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS blobs (
  sha256      TEXT PRIMARY KEY,
  size        INTEGER NOT NULL,
  created_at  INTEGER NOT NULL,
  last_used   INTEGER NOT NULL,
  ref_count   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS urls (
  url           TEXT PRIMARY KEY,
  sha256        TEXT NOT NULL REFERENCES blobs(sha256),
  etag          TEXT,
  last_modified TEXT,
  fetched_at    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS urls_by_hash ON urls(sha256);
`

type dbStmts struct {
	getByURL   *sql.Stmt
	insertBlob *sql.Stmt
	insertURL  *sql.Stmt
	touchBlob  *sql.Stmt
	deleteURLs *sql.Stmt
	deleteBlob *sql.Stmt
	totalSize  *sql.Stmt
	listLRU    *sql.Stmt
}

func openDB(dir string) (*sql.DB, *dbStmts, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create cache dir: %w", err)
	}

	dbPath := filepath.Join(dir, "index.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite WAL handles readers; one writer is sufficient

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("apply schema: %w", err)
	}

	s, err := prepareStmts(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	return db, s, nil
}

func prepareStmts(db *sql.DB) (*dbStmts, error) {
	var s dbStmts
	type named struct {
		dest **sql.Stmt
		sql  string
	}
	stmts := []named{
		{&s.getByURL, `SELECT b.sha256, b.size FROM urls u JOIN blobs b ON b.sha256 = u.sha256 WHERE u.url = ?`},
		{&s.insertBlob, `INSERT OR IGNORE INTO blobs(sha256, size, created_at, last_used, ref_count) VALUES(?,?,?,?,0)`},
		{&s.insertURL, `INSERT OR REPLACE INTO urls(url, sha256, etag, last_modified, fetched_at) VALUES(?,?,?,?,?)`},
		{&s.touchBlob, `UPDATE blobs SET last_used = ? WHERE sha256 = ?`},
		{&s.deleteURLs, `DELETE FROM urls WHERE sha256 = ?`},
		{&s.deleteBlob, `DELETE FROM blobs WHERE sha256 = ?`},
		{&s.totalSize, `SELECT COALESCE(SUM(size), 0) FROM blobs`},
		{&s.listLRU, `SELECT sha256, size, last_used FROM blobs ORDER BY last_used ASC`},
	}
	for _, n := range stmts {
		stmt, err := db.Prepare(n.sql)
		if err != nil {
			s.close()
			return nil, fmt.Errorf("prepare %q: %w", n.sql[:20], err)
		}
		*n.dest = stmt
	}
	return &s, nil
}

func (s *dbStmts) close() {
	for _, stmt := range []*sql.Stmt{
		s.getByURL, s.insertBlob, s.insertURL,
		s.touchBlob, s.deleteURLs, s.deleteBlob,
		s.totalSize, s.listLRU,
	} {
		if stmt != nil {
			_ = stmt.Close()
		}
	}
}
