package store

import (
	"database/sql"
	"fmt"
	"time"
)

type HistoryPolicy struct {
	Age        time.Duration
	PerAccount int
	Bytes      int64
}

var DefaultHistory = HistoryPolicy{7 * 24 * time.Hour, 100000, 256 << 20}

type Event struct {
	Seq      int64  `json:"-"`
	Received string `json:"received_at"`
	Payload  []byte `json:"-"`
}

func (s *Store) AppendEvent(alias string, at time.Time, payload []byte, policy HistoryPolicy) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	res, e := tx.Exec("INSERT INTO events(alias,received_ns,payload,size) VALUES(?,?,?,?)", alias, at.UnixNano(), payload, len(payload))
	if e != nil {
		return e
	}
	seq, e := res.LastInsertId()
	if e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO sync(alias,high_seq) VALUES(?,?) ON CONFLICT(alias) DO UPDATE SET high_seq=excluded.high_seq", alias, seq); e != nil {
		return e
	}
	if e = prune(tx, at, policy); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Prune(at time.Time, policy HistoryPolicy) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = prune(tx, at, policy); e != nil {
		return e
	}
	return tx.Commit()
}
func prune(tx *sql.Tx, at time.Time, p HistoryPolicy) error {
	if p.Age <= 0 || p.PerAccount <= 0 || p.Bytes <= 0 {
		return fmt.Errorf("invalid retention policy")
	}
	rows, e := tx.Query("SELECT DISTINCT alias FROM events")
	if e != nil {
		return e
	}
	aliases := []string{}
	for rows.Next() {
		var a string
		if e = rows.Scan(&a); e != nil {
			rows.Close()
			return e
		}
		aliases = append(aliases, a)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	for _, alias := range aliases {
		var boundary int64
		e = tx.QueryRow(`SELECT coalesce(max(seq),0) FROM events WHERE alias=? AND received_ns<=? AND seq<coalesce((SELECT min(seq) FROM events WHERE alias=? AND received_ns>?),9223372036854775807)`, alias, at.Add(-p.Age).UnixNano(), alias, at.Add(-p.Age).UnixNano()).Scan(&boundary)
		if e != nil {
			return e
		}
		var countBoundary int64
		e = tx.QueryRow("SELECT seq FROM events WHERE alias=? ORDER BY seq DESC LIMIT 1 OFFSET ?", alias, p.PerAccount).Scan(&countBoundary)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if countBoundary > boundary {
			boundary = countBoundary
		}
		if e = pruneThrough(tx, alias, boundary); e != nil {
			return e
		}
	}
	var total int64
	if e = tx.QueryRow("SELECT coalesce(sum(size),0) FROM events").Scan(&total); e != nil {
		return e
	}
	if total > p.Bytes {
		var cutoff int64
		e = tx.QueryRow(`SELECT seq FROM (SELECT seq,sum(size) OVER(ORDER BY seq) AS running FROM events) WHERE running>=? ORDER BY seq LIMIT 1`, total-p.Bytes).Scan(&cutoff)
		if e != nil {
			return e
		}
		rows, e := tx.Query("SELECT alias,max(seq) FROM events WHERE seq<=? GROUP BY alias", cutoff)
		if e != nil {
			return e
		}
		bounds := map[string]int64{}
		for rows.Next() {
			var a string
			var n int64
			if e = rows.Scan(&a, &n); e != nil {
				rows.Close()
				return e
			}
			bounds[a] = n
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return e
		}
		rows.Close()
		for a, n := range bounds {
			if e = pruneThrough(tx, a, n); e != nil {
				return e
			}
		}
	}
	return nil
}
func pruneThrough(tx *sql.Tx, alias string, n int64) error {
	if n == 0 {
		return nil
	}
	if _, e := tx.Exec("DELETE FROM events WHERE alias=? AND seq<=?", alias, n); e != nil {
		return e
	}
	_, e := tx.Exec("UPDATE sync SET pruned_seq=max(pruned_seq,?),truncated=1 WHERE alias=?", n, alias)
	return e
}
func (s *Store) ReadEvents(alias string, after int64, limit int) ([]Event, bool, error) {
	rows, e := s.DB.Query("SELECT seq,received_ns,payload FROM events WHERE alias=? AND seq>? ORDER BY seq LIMIT ?", alias, after, limit+1)
	if e != nil {
		return nil, false, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		var n int64
		if e = rows.Scan(&v.Seq, &n, &v.Payload); e != nil {
			return nil, false, e
		}
		v.Received = time.Unix(0, n).UTC().Format(time.RFC3339Nano)
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, false, e
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, nil
}
