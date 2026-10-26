package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/bright-k/tossinvest-tools/internal/store"
	"strings"
	"time"
)

func StreamSchemas() map[string]Object {
	empty := Object{"type": "object", "properties": Object{}, "additionalProperties": false}
	return map[string]Object{
		"tossinvest_get_stream_status":   empty,
		"tossinvest_set_subscriptions":   {"type": "object", "properties": Object{"subscriptions": Object{"type": "array", "items": Object{"type": "object", "properties": Object{"channel": Object{"type": "string", "enum": []string{"trade:kr", "trade:us", "orderbook:kr", "orderbook:us", "personal:order"}}, "codes": Object{"type": "array", "items": Object{"type": "string"}}}, "required": []string{"channel", "codes"}, "additionalProperties": false}}}, "required": []string{"subscriptions"}, "additionalProperties": false},
		"tossinvest_get_stream_snapshot": {"type": "object", "properties": Object{"channel": Object{"type": "string", "enum": []string{"trade:kr", "trade:us", "orderbook:kr", "orderbook:us"}}, "symbol": Object{"type": "string"}}, "required": []string{"channel", "symbol"}, "additionalProperties": false},
		"tossinvest_list_order_events":   {"type": "object", "properties": Object{"account_alias": Object{"type": "string"}, "cursor": Object{"type": "string"}, "limit": Object{"type": "integer", "minimum": 1, "maximum": 1000}}, "required": []string{"account_alias"}, "additionalProperties": false},
	}
}
func StreamDescription(name string) string {
	switch name {
	case "tossinvest_set_subscriptions":
		return "Replace your complete market watchlist. personal:order codes are allowed account aliases. Server mandatory order monitoring remains active."
	case "tossinvest_get_stream_snapshot":
		return "Read latest observed market snapshot, preserving source timestamp and gap status."
	case "tossinvest_list_order_events":
		return "Read committed received order events for one allowed account. History may contain gaps and retention limits; opaque cursors expire."
	}
	return "Read stream connection, subscriptions, rejections and synchronization readiness."
}

type cursor struct {
	Backend   string `json:"b"`
	Run       string `json:"r"`
	Principal string `json:"p"`
	Revision  int64  `json:"v"`
	Alias     string `json:"a"`
	Seq       int64  `json:"s"`
}

func (s *Service) signCursor(c cursor) string {
	b, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) parseCursor(token string) (cursor, error) {
	var c cursor
	if len(token) > 4096 {
		return c, fault.New("INVALID_ARGUMENT", "invalid cursor")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, fault.New("INVALID_ARGUMENT", "invalid cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return c, fault.New("INVALID_ARGUMENT", "invalid cursor")
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return c, fault.New("INVALID_ARGUMENT", "invalid cursor")
	}
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write(b)
	if !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(b, &c) != nil {
		return c, fault.New("INVALID_ARGUMENT", "invalid cursor MAC")
	}
	return c, nil
}
func (s *Service) Events(ctx context.Context, p Principal, args Object) (Object, error) {
	if err := p.Require("streams:read"); err != nil {
		return nil, err
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	alias := contract.Str(args["account_alias"])
	if _, e := s.allowed(p, alias); e != nil {
		return nil, e
	}
	if e := s.Store.Prune(s.Now(), store.DefaultHistory); e != nil {
		return nil, fault.New("NOT_READY", "event store unavailable")
	}
	rev, e := s.revision()
	if e != nil {
		return nil, e
	}
	var pruned, high int64
	var oldest *int64
	if e = s.Store.DB.QueryRow("SELECT pruned_seq,high_seq FROM sync WHERE alias=?", alias).Scan(&pruned, &high); e != nil {
		return nil, e
	}
	if e = s.Store.DB.QueryRow("SELECT min(received_ns) FROM events WHERE alias=?", alias).Scan(&oldest); e != nil {
		return nil, e
	}
	var retained any
	if oldest != nil {
		retained = time.Unix(0, *oldest).UTC().Format(time.RFC3339Nano)
	}
	expired := func() error {
		e := fault.New("CURSOR_EXPIRED", "restart event listing without cursor and verify current state by REST")
		e.Data = Object{"retained_from_at": retained, "restart_required": true}
		return e
	}
	after := pruned
	if token := contract.Str(args["cursor"]); token != "" {
		c, e := s.parseCursor(token)
		if e != nil {
			return nil, e
		}
		if c.Principal != p.ID || c.Alias != alias || c.Backend != s.Backend {
			return nil, fault.New("INVALID_ARGUMENT", "cursor context mismatch")
		}
		if c.Run != s.RunID || c.Revision != rev {
			return nil, expired()
		}
		if c.Seq < 0 || c.Seq > high {
			return nil, fault.New("INVALID_ARGUMENT", "cursor position invalid")
		}
		if c.Seq < pruned {
			return nil, expired()
		}
		after = c.Seq
	}
	limit := 100
	if n, ok := args["limit"].(json.Number); ok {
		v, _ := n.Int64()
		limit = int(v)
	}
	events, more, e := s.Store.ReadEvents(alias, after, limit)
	if e != nil {
		return nil, e
	}
	out := []any{}
	for _, v := range events {
		var payload any
		if contract.Decode(v.Payload, &payload) != nil {
			return nil, fault.New("UPSTREAM_DATA_INVALID", "stored event invalid")
		}
		out = append(out, Object{"received_at": v.Received, "data": sanitize(payload)})
		after = v.Seq
	}
	if len(events) == 0 && contract.Str(args["cursor"]) == "" {
		after = high
	}
	m := s.meta()
	s.syncMeta(alias, m)
	m["retained_from_at"] = retained
	m["account_alias"] = alias
	return Object{"data": Object{"events": out, "next_cursor": s.signCursor(cursor{s.Backend, s.RunID, p.ID, rev, alias, after}), "has_more": more}, "meta": m}, nil
}
