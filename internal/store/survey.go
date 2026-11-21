package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/glueops/fyisp/internal/model"
)

// The path survey's data (schema v10, see surveyDDL): the address each
// host's probes used (ObserveAddr, from the probe runner) and each target's
// hourly survey (ObserveSurvey, from the tracer). Both only touch memory;
// Flush writes them in its transaction, like the trace data.
//
// target_addrs keeps one row per period a host used one address. Its
// last_ms is rewritten at most every HopSeenInterval while the address
// stays, and once more when it changes, so a period ends at most that long
// before the next one starts. A restart starts a new period.
//
// survey_hops rows are model.SurveyHour as zstd-compressed JSON. The
// tracer reports a target's whole hour so far after every round; the
// first write of an hour after a start adds the rounds an earlier run
// stored for it.

// AddrPeriod is one row of target_addrs.
type AddrPeriod struct {
	Host        string
	IP          netip.Addr
	Answers     int // addresses DNS returned at the last lookup
	First, Last time.Time
}

type addrKey struct {
	host  string
	ip    netip.Addr
	first int64
}

type pendingAddr struct {
	p   AddrPeriod
	seq int64
}

type surveyKey struct {
	hour   int64
	target string
}

type pendingSurvey struct {
	h   model.SurveyHour
	seq int64
}

// surveyState is the SQLite's survey memory, guarded by SQLite.mu.
type surveyState struct {
	addrs   map[string]addrCur // the current period per host
	addrQ   map[addrKey]pendingAddr
	surveyQ map[surveyKey]pendingSurvey
}

type addrCur struct {
	p       AddrPeriod
	written time.Time // Last as last queued
}

func newSurveyState() surveyState {
	return surveyState{addrs: map[string]addrCur{}, addrQ: map[addrKey]pendingAddr{}, surveyQ: map[surveyKey]pendingSurvey{}}
}

// ObserveAddr records that host's probes use ip after a lookup that
// returned answers addresses. It never blocks on I/O. The caller passes
// public addresses only (the runner's of a --config target may be on the
// LAN).
func (s *SQLite) ObserveAddr(host string, ip netip.Addr, answers int) {
	ip = ip.Unmap()
	if !ip.IsValid() {
		return
	}
	now := s.o.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.lock == nil {
		return
	}
	sv := &s.sv
	c, ok := sv.addrs[host]
	if ok && c.p.IP == ip {
		c.p.Last, c.p.Answers = now, answers
		if now.Sub(c.written) < HopSeenInterval {
			sv.addrs[host] = c
			return
		}
	} else {
		if ok {
			s.queueAddrLocked(c.p) // its final Last
		}
		c = addrCur{p: AddrPeriod{Host: host, IP: ip, Answers: answers, First: now, Last: now}}
	}
	c.written = now
	sv.addrs[host] = c
	s.queueAddrLocked(c.p)
}

func (s *SQLite) queueAddrLocked(p AddrPeriod) {
	s.tr.seq++
	s.sv.addrQ[addrKey{p.Host, p.IP, p.First.UnixMilli()}] = pendingAddr{p, s.tr.seq}
}

// ObserveSurvey records a target's survey of an hour so far (it replaces
// an earlier report of the same hour). It never blocks on I/O.
func (s *SQLite) ObserveSurvey(h model.SurveyHour) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.lock == nil {
		return
	}
	s.tr.seq++
	s.sv.surveyQ[surveyKey{h.Hour, h.Target}] = pendingSurvey{h, s.tr.seq}
}

// surveySnap is what one Flush writes of the survey.
type surveySnap struct {
	addrs  []pendingAddr
	survey []pendingSurvey
}

func (sv *surveyState) snapshotLocked() surveySnap {
	var sn surveySnap
	for _, a := range sv.addrQ {
		sn.addrs = append(sn.addrs, a)
	}
	for _, h := range sv.surveyQ {
		sn.survey = append(sn.survey, h)
	}
	return sn
}

func (sv *surveyState) committedLocked(sn surveySnap) {
	for _, a := range sn.addrs {
		k := addrKey{a.p.Host, a.p.IP, a.p.First.UnixMilli()}
		if p, ok := sv.addrQ[k]; ok && p.seq == a.seq {
			delete(sv.addrQ, k)
		}
	}
	for _, h := range sn.survey {
		k := surveyKey{h.h.Hour, h.h.Target}
		if p, ok := sv.surveyQ[k]; ok && p.seq == h.seq {
			delete(sv.surveyQ, k)
		}
	}
}

func (sn surveySnap) empty() bool { return len(sn.addrs)+len(sn.survey) == 0 }

// writeSurvey writes sn in tx. base (Flush's, guarded by flushMu) holds
// the rounds an earlier run stored for an hour this run reports: read at
// the first write of that hour, then added to every write (pruneBase
// drops old hours once a write succeeded).
func writeSurvey(ctx context.Context, tx *sql.Tx, sn surveySnap, base map[surveyKey]*model.SurveyHour) error {
	for _, a := range sn.addrs {
		p := a.p
		if _, err := tx.ExecContext(ctx, `INSERT INTO target_addrs(host, ip, answers, first_ms, last_ms) VALUES(?,?,?,?,?)
			ON CONFLICT(host, ip, first_ms) DO UPDATE SET answers = excluded.answers, last_ms = max(last_ms, excluded.last_ms)`,
			p.Host, p.IP.String(), p.Answers, p.First.UnixMilli(), p.Last.UnixMilli()); err != nil {
			return fmt.Errorf("store: target_addrs: %w", err)
		}
	}
	for _, ps := range sn.survey {
		k := surveyKey{ps.h.Hour, ps.h.Target}
		b, ok := base[k]
		if !ok {
			var data []byte
			switch err := tx.QueryRowContext(ctx, `SELECT data FROM survey_hops WHERE hour = ? AND target = ?`, k.hour, k.target).Scan(&data); {
			case err == sql.ErrNoRows:
			case err != nil:
				return fmt.Errorf("store: survey_hops: %w", err)
			default:
				old, err := decodeSurvey(data)
				if err != nil {
					return fmt.Errorf("store: survey_hops of %q: %w", k.target, err)
				}
				b = &old
			}
			base[k] = b
		}
		h := ps.h
		if b != nil {
			m := b.Clone()
			m.Merge(h)
			m.Host, m.Port, m.HopsVia = h.Host, h.Port, h.HopsVia
			h = m
		}
		data, err := encodeSurvey(h)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO survey_hops(hour, target, data) VALUES(?,?,?)`, k.hour, k.target, data); err != nil {
			return fmt.Errorf("store: survey_hops: %w", err)
		}
	}
	return nil
}

// pruneBase drops the hours before the previous one from base: the
// tracer no longer reports them.
func pruneBase(base map[surveyKey]*model.SurveyHour, now time.Time) {
	cur := now.Unix() / 3600
	for k := range base {
		if k.hour < cur-1 {
			delete(base, k)
		}
	}
}

var (
	surveyEnc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression), zstd.WithEncoderConcurrency(1), zstd.WithLowerEncoderMem(true))
	surveyDec, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxMemory(16<<20))
)

func encodeSurvey(h model.SurveyHour) ([]byte, error) {
	raw, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("store: survey of %q: %w", h.Target, err)
	}
	return surveyEnc.EncodeAll(raw, nil), nil
}

func decodeSurvey(data []byte) (model.SurveyHour, error) {
	var h model.SurveyHour
	raw, err := surveyDec.DecodeAll(data, nil)
	if err != nil {
		return h, err
	}
	err = json.Unmarshal(raw, &h)
	return h, err
}

// pruneSurvey deletes survey data before ms (in prune's transaction).
func pruneSurvey(ctx context.Context, tx *sql.Tx, ms int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM target_addrs WHERE last_ms < ?`, ms); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM survey_hops WHERE hour < ?`, ms/hourMs)
	return err
}

// AddrPeriods calls fn for every address period that overlaps [from, to],
// by host and start (`fyisp export --addrs`).
func (s *SQLite) AddrPeriods(ctx context.Context, from, to time.Time, fn func(AddrPeriod) error) error {
	rows, err := s.db.r.QueryContext(ctx, `SELECT host, ip, answers, first_ms, last_ms FROM target_addrs
		WHERE last_ms >= ? AND first_ms <= ? ORDER BY host, first_ms`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p AddrPeriod
		var ip string
		var first, last int64
		if err := rows.Scan(&p.Host, &ip, &p.Answers, &first, &last); err != nil {
			return err
		}
		if p.IP, err = netip.ParseAddr(ip); err != nil {
			return fmt.Errorf("store: target_addrs of %q: %w", p.Host, err)
		}
		p.First, p.Last = time.UnixMilli(first).UTC(), time.UnixMilli(last).UTC()
		if err := fn(p); err != nil {
			return err
		}
	}
	return rows.Err()
}

// SurveyHours calls fn for every target's survey of the hours that
// overlap [from, to], by hour and target (`fyisp export --survey`).
func (s *SQLite) SurveyHours(ctx context.Context, from, to time.Time, fn func(model.SurveyHour) error) error {
	rows, err := s.db.r.QueryContext(ctx, `SELECT hour, target, data FROM survey_hops WHERE hour >= ? AND hour <= ? ORDER BY hour, target`,
		from.Unix()/3600, to.Unix()/3600)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var hour int64
		var target string
		var data []byte
		if err := rows.Scan(&hour, &target, &data); err != nil {
			return err
		}
		h, err := decodeSurvey(data)
		if err != nil {
			return fmt.Errorf("store: survey_hops of %q: %w", target, err)
		}
		h.Hour, h.Target = hour, target
		if err := fn(h); err != nil {
			return err
		}
	}
	return rows.Err()
}
