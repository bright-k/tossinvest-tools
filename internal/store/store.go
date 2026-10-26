package store

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

type Store struct{ DB *sql.DB }

func Open(path, backend string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if st, e := os.Lstat(path); e == nil {
		if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
			return nil, fmt.Errorf("database must be regular private 0600 file")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Store, error) { db.Close(); return nil, err }
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=3000"} {
		if _, err = db.Exec(q); err != nil {
			return fail(err)
		}
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version > 1 {
		return fail(fmt.Errorf("unsupported database version"))
	}
	tx, err := db.Begin()
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
 CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS accounts(alias TEXT PRIMARY KEY,seq TEXT NOT NULL,kind TEXT NOT NULL,last4 TEXT NOT NULL,client TEXT NOT NULL,active INTEGER NOT NULL DEFAULT 1);
 CREATE UNIQUE INDEX IF NOT EXISTS active_account ON accounts(client,seq) WHERE active=1;
 CREATE TABLE IF NOT EXISTS grants(principal TEXT NOT NULL,alias TEXT NOT NULL REFERENCES accounts(alias),PRIMARY KEY(principal,alias));
 CREATE TABLE IF NOT EXISTS relations(client TEXT NOT NULL,kind TEXT NOT NULL,id TEXT NOT NULL,alias TEXT NOT NULL,conflict INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(client,kind,id));
 CREATE TABLE IF NOT EXISTS sync(alias TEXT PRIMARY KEY,gap INTEGER NOT NULL DEFAULT 1,status TEXT NOT NULL DEFAULT 'recovering',last_synced TEXT,pruned_seq INTEGER NOT NULL DEFAULT 0,truncated INTEGER NOT NULL DEFAULT 0,high_seq INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT,alias TEXT NOT NULL,received_ns INTEGER NOT NULL,payload BLOB NOT NULL,size INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS events_account ON events(alias,seq);
 CREATE TABLE IF NOT EXISTS watchlist(principal TEXT NOT NULL,topic TEXT NOT NULL,PRIMARY KEY(principal,topic));
 CREATE TABLE IF NOT EXISTS snapshots(alias TEXT PRIMARY KEY,generation INTEGER NOT NULL,payload BLOB NOT NULL);
 PRAGMA user_version=1;
 INSERT OR IGNORE INTO metadata VALUES('revision','0');
 `)
	if err != nil {
		return fail(err)
	}
	var existing string
	err = tx.QueryRow("SELECT value FROM metadata WHERE key='backend'").Scan(&existing)
	if err == nil && existing != backend {
		return fail(fmt.Errorf("backend database mismatch"))
	}
	if err != nil && err != sql.ErrNoRows {
		return fail(err)
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO metadata VALUES('backend',?)", backend); err != nil {
		return fail(err)
	}
	if _, err = tx.Exec("UPDATE sync SET gap=1,status='recovering'"); err != nil {
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return &Store{DB: db}, nil
}
func (s *Store) Close() error {
	_, _ = s.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return s.DB.Close()
}
func (s *Store) Backup(path string) error {
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return fmt.Errorf("backup destination exists")
	}
	_, err := s.DB.Exec("VACUUM INTO ?", path)
	if err == nil {
		err = os.Chmod(path, 0600)
	}
	return err
}
func (s *Store) Check() error {
	var result string
	err := s.DB.QueryRow("PRAGMA quick_check").Scan(&result)
	if err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("database integrity check failed")
	}
	return nil
}
