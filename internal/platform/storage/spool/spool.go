// Package spool implements the durable local accounting recovery store as an
// append-only WAL with segment rotation, checksums, torn-tail recovery, and
// checkpoint/truncate.
package spool

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/finops/accounting"
)

// Config controls segment rotation and fsync behavior.
type Config struct {
	Dir          string // base directory, e.g. /data/spool/accounting
	SegmentBytes int64  // rotate a segment after this many bytes
	FsyncEvery   int    // fsync every N appends; 1 = fsync each append
}

// record is the on-disk JSON shape for one spool event.
type record struct {
	EventID        string           `json:"event_id"`
	RequestID      string           `json:"request_id"`
	TenantID       string           `json:"tenant_id"`
	ProjectID      string           `json:"project_id"`
	EventTS        time.Time        `json:"event_ts"`
	PricingVersion string           `json:"pricing_version"`
	Facts          accounting.Facts `json:"facts"`
	Checksum       string           `json:"checksum"`
}

// decodeRecord parses one WAL line and verifies its checksum.
func decodeRecord(line []byte) (record, error) {
	var rec record
	if err := json.Unmarshal(line, &rec); err != nil {
		return record{}, err
	}
	if rec.Checksum == "" {
		return record{}, errors.New("missing checksum")
	}
	stored := rec.Checksum
	rec.Checksum = ""
	payload, err := json.Marshal(rec)
	if err != nil {
		return record{}, err
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != stored {
		return record{}, fmt.Errorf("checksum mismatch for event %s", rec.EventID)
	}
	return rec, nil
}

type segment struct {
	name           string
	file           *os.File
	bytes          int64
	ids            []string
	tenants        []string
	sizes          []int64
	offsets        []int64
	committedCount int
}

type tenantUsage struct{ events, bytes int64 }

// FileSpool is a thread-safe append-only WAL implementing accounting.Spool.
//
// Usage counters track uncommitted (pending) events only: committed events
// drop out of the counters as the committed prefix advances and are dropped
// from disk once their whole segment is committed.
type FileSpool struct {
	mu            sync.Mutex
	config        Config
	segments      []*segment
	committed     map[string]bool
	tenants       map[string]*tenantUsage
	pendingEvents int64
	pendingBytes  int64
	appends       int
	closed        bool
}

var _ accounting.Spool = (*FileSpool)(nil)

// New opens or creates a WAL at config.Dir.
func New(config Config) (*FileSpool, error) {
	if config.Dir == "" {
		return nil, errors.New("spool directory is required")
	}
	if config.SegmentBytes <= 0 {
		config.SegmentBytes = 16 << 20 // 16 MiB default
	}
	if config.FsyncEvery <= 0 {
		config.FsyncEvery = 1
	}
	if err := os.MkdirAll(config.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("create spool dir: %w", err)
	}
	s := &FileSpool{
		config:    config,
		committed: map[string]bool{},
		tenants:   map[string]*tenantUsage{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileSpool) load() error {
	entries, err := os.ReadDir(s.config.Dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "segment-") && strings.HasSuffix(name, ".wal") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		seg, err := s.loadSegment(name)
		if err != nil {
			return err
		}
		s.segments = append(s.segments, seg)
	}
	s.applyCommittedLog()
	// Every segment is opened in append mode, so the newest survivor accepts
	// new appends directly; rotate only when none survived.
	if len(s.segments) == 0 {
		return s.rotate()
	}
	return nil
}

// loadSegment reads one segment file, verifying every record. A torn tail
// (a crash mid-append leaves a partial final line) is truncated away; any
// other corruption is a hard error.
func (s *FileSpool) loadSegment(name string) (*segment, error) {
	path := filepath.Join(s.config.Dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seg := &segment{name: name}
	rest := data
	offset := int64(0)
	for len(rest) > 0 {
		idx := bytes.IndexByte(rest, '\n')
		var line []byte
		var consumed int64
		last := false
		if idx >= 0 {
			line = rest[:idx]
			consumed = int64(idx) + 1
			rest = rest[consumed:]
		} else {
			line = rest
			consumed = int64(len(rest))
			rest = nil
			last = true
		}
		if len(line) > 0 {
			rec, err := decodeRecord(line)
			if err != nil {
				if !last {
					return nil, fmt.Errorf("corrupt spool segment %s: %w", name, err)
				}
				// Torn tail: drop the partial line, keep everything before it.
				if err := os.Truncate(path, offset); err != nil {
					return nil, err
				}
				break
			}
			seg.ids = append(seg.ids, rec.EventID)
			seg.tenants = append(seg.tenants, rec.TenantID)
			seg.sizes = append(seg.sizes, consumed)
			seg.offsets = append(seg.offsets, offset)
		}
		offset += consumed
	}
	// O_APPEND makes every write land at the end regardless of the read
	// cursor, so a segment can be re-appended after any earlier segment is
	// removed without risk of overwriting from offset zero.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0o640)
	if err != nil {
		return nil, err
	}
	seg.file = f
	seg.bytes = offset
	return seg, nil
}

// applyCommittedLog replays committed.log onto the loaded segments: advances
// each segment's committed prefix, rebuilds the pending counters and tenant
// usage, removes fully-committed segments, and keeps only still-relevant ids
// in the in-memory committed set.
func (s *FileSpool) applyCommittedLog() {
	data, err := os.ReadFile(filepath.Join(s.config.Dir, "committed.log"))
	if errors.Is(err, os.ErrNotExist) {
		s.rebuildStateLocked()
		return
	}
	if err != nil {
		// A damaged committed log degrades to replaying everything (at-least-
		// once ingest is idempotent), which is the safe direction.
		s.rebuildStateLocked()
		return
	}
	known := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			known[line] = true
		}
	}
	for _, seg := range s.segments {
		for seg.committedCount < len(seg.ids) && known[seg.ids[seg.committedCount]] {
			seg.committedCount++
		}
	}
	s.rebuildStateLocked()
}

// rebuildStateLocked recomputes pending counters, tenant usage, and the
// in-memory committed set from the segments' committed prefixes, then removes
// fully-committed segments. Callers must hold s.mu.
func (s *FileSpool) rebuildStateLocked() {
	s.committed = map[string]bool{}
	s.tenants = map[string]*tenantUsage{}
	s.pendingEvents = 0
	s.pendingBytes = 0
	keep := s.segments[:0]
	for _, seg := range s.segments {
		for i := seg.committedCount; i < len(seg.ids); i++ {
			s.pendingEvents++
			s.pendingBytes += seg.sizes[i]
			tenantID := seg.tenants[i]
			u := s.tenants[tenantID]
			if u == nil {
				u = &tenantUsage{}
				s.tenants[tenantID] = u
			}
			u.events++
			u.bytes += seg.sizes[i]
		}
		if len(seg.ids) > 0 && seg.committedCount == len(seg.ids) {
			_ = seg.file.Close()
			_ = os.Remove(filepath.Join(s.config.Dir, seg.name))
			continue
		}
		keep = append(keep, seg)
		for i := 0; i < seg.committedCount; i++ {
			s.committed[seg.ids[i]] = true
		}
	}
	s.segments = keep
}

// Append durably writes one event to the WAL.
func (s *FileSpool) Append(_ context.Context, event accounting.SpoolRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return accounting.ErrSpoolUnavailable
	}
	if len(s.segments) == 0 {
		if err := s.rotate(); err != nil {
			return err
		}
	}
	line, err := s.serialize(event)
	if err != nil {
		return err
	}
	current := s.segments[len(s.segments)-1]
	if current.bytes+int64(len(line)) > s.config.SegmentBytes {
		if err := s.rotate(); err != nil {
			return err
		}
		current = s.segments[len(s.segments)-1]
	}
	if _, err := current.file.Write(line); err != nil {
		return accounting.ErrSpoolUnavailable
	}
	s.appends++
	if s.appends%s.config.FsyncEvery == 0 {
		if err := current.file.Sync(); err != nil {
			return accounting.ErrSpoolUnavailable
		}
	}
	offset := current.bytes
	current.bytes += int64(len(line))
	current.ids = append(current.ids, event.EventID)
	current.tenants = append(current.tenants, event.TenantID)
	current.sizes = append(current.sizes, int64(len(line)))
	current.offsets = append(current.offsets, offset)
	s.pendingEvents++
	s.pendingBytes += int64(len(line))
	u := s.tenants[event.TenantID]
	if u == nil {
		u = &tenantUsage{}
		s.tenants[event.TenantID] = u
	}
	u.events++
	u.bytes += int64(len(line))
	return nil
}

func (s *FileSpool) rotate() error {
	name := fmt.Sprintf("segment-%06d.wal", len(s.segments)+1)
	f, err := os.OpenFile(filepath.Join(s.config.Dir, name), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	s.segments = append(s.segments, &segment{name: name, file: f})
	return nil
}

func (s *FileSpool) serialize(event accounting.SpoolRecord) ([]byte, error) {
	rec := record{
		EventID:        event.EventID,
		RequestID:      event.RequestID,
		TenantID:       event.TenantID,
		ProjectID:      event.ProjectID,
		EventTS:        event.EventTS,
		PricingVersion: event.PricingVersion,
		Facts:          event.Facts,
	}
	payload, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	rec.Checksum = hex.EncodeToString(sum[:])
	final, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	return append(final, '\n'), nil
}

// Pending returns up to limit uncommitted events in append order.
func (s *FileSpool) Pending(_ context.Context, limit int) ([]accounting.SpoolRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		return nil, nil
	}
	var result []accounting.SpoolRecord
	for _, seg := range s.segments {
		if len(result) >= limit {
			break
		}
		if err := s.collectPending(seg, limit-len(result), &result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *FileSpool) collectPending(seg *segment, limit int, out *[]accounting.SpoolRecord) error {
	if seg.committedCount >= len(seg.ids) {
		return nil
	}
	// Skip the committed prefix: only the pending tail is scanned.
	if _, err := seg.file.Seek(seg.offsets[seg.committedCount], 0); err != nil {
		return err
	}
	scanner := bufio.NewScanner(seg.file)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		rec, err := decodeRecord([]byte(line))
		if err != nil {
			return fmt.Errorf("corrupt spool segment %s: %w", seg.name, err)
		}
		if s.committed[rec.EventID] {
			continue
		}
		*out = append(*out, accounting.SpoolRecord{
			EventID:        rec.EventID,
			RequestID:      rec.RequestID,
			TenantID:       rec.TenantID,
			ProjectID:      rec.ProjectID,
			EventTS:        rec.EventTS,
			PricingVersion: rec.PricingVersion,
			Facts:          rec.Facts,
			Checksum:       rec.Checksum,
		})
		if len(*out) >= limit {
			break
		}
	}
	return scanner.Err()
}

// Commit marks events as ingested, advances the committed prefixes, updates
// the pending counters, and truncates fully-committed segments.
func (s *FileSpool) Commit(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(s.config.Dir, "committed.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	var builder strings.Builder
	for _, id := range ids {
		if s.committed[id] {
			continue
		}
		s.committed[id] = true
		builder.WriteString(id)
		builder.WriteByte('\n')
	}
	if builder.Len() > 0 {
		if _, err := f.WriteString(builder.String()); err != nil {
			return err
		}
		if err := f.Sync(); err != nil {
			return err
		}
	}
	for _, seg := range s.segments {
		for seg.committedCount < len(seg.ids) && s.committed[seg.ids[seg.committedCount]] {
			s.pendingEvents--
			s.pendingBytes -= seg.sizes[seg.committedCount]
			if u := s.tenants[seg.tenants[seg.committedCount]]; u != nil {
				u.events--
				u.bytes -= seg.sizes[seg.committedCount]
			}
			seg.committedCount++
		}
	}
	return s.removeFullyCommittedLocked()
}

// removeFullyCommittedLocked deletes segments whose events are all committed
// and rewrites committed.log to cover only the surviving segments, so the log
// does not grow with the process's full history. Callers must hold s.mu.
func (s *FileSpool) removeFullyCommittedLocked() error {
	removed := false
	keep := s.segments[:0]
	for _, seg := range s.segments {
		if len(seg.ids) > 0 && seg.committedCount == len(seg.ids) {
			_ = seg.file.Close()
			_ = os.Remove(filepath.Join(s.config.Dir, seg.name))
			for _, id := range seg.ids {
				delete(s.committed, id)
			}
			removed = true
			continue
		}
		keep = append(keep, seg)
	}
	s.segments = keep
	if !removed {
		return nil
	}
	var builder strings.Builder
	for _, seg := range s.segments {
		for i := 0; i < seg.committedCount; i++ {
			builder.WriteString(seg.ids[i])
			builder.WriteByte('\n')
		}
	}
	return os.WriteFile(filepath.Join(s.config.Dir, "committed.log"), []byte(builder.String()), 0o640)
}

// Stats reports global spool usage (pending backlog) in O(1).
func (s *FileSpool) Stats(context.Context) (accounting.SpoolStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := accounting.SpoolStats{Events: s.pendingEvents, Bytes: s.pendingBytes}
	stats.OldestAge = s.oldestUncommittedLocked()
	return stats, nil
}

// TenantUsage reports uncommitted (pending) events and bytes for one tenant so
// per-tenant quotas track the actual backlog footprint.
func (s *FileSpool) TenantUsage(_ context.Context, tenantID string) (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.tenants[tenantID]
	if u == nil {
		return 0, 0, nil
	}
	return u.events, u.bytes, nil
}

// oldestUncommittedLocked decodes only the single oldest pending line instead
// of scanning the whole WAL. Callers must hold s.mu.
func (s *FileSpool) oldestUncommittedLocked() time.Duration {
	for _, seg := range s.segments {
		if seg.committedCount >= len(seg.ids) {
			continue
		}
		rec, err := s.lineAt(seg, seg.offsets[seg.committedCount])
		if err != nil || rec.EventTS.IsZero() {
			continue
		}
		return time.Since(rec.EventTS)
	}
	return 0
}

func (s *FileSpool) lineAt(seg *segment, offset int64) (record, error) {
	reader := bufio.NewReaderSize(io.NewSectionReader(seg.file, offset, seg.bytes-offset), 64*1024)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return record{}, err
	}
	return decodeRecord([]byte(strings.TrimSuffix(line, "\n")))
}

// Close flushes and closes all segment files.
func (s *FileSpool) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var firstErr error
	for _, seg := range s.segments {
		if err := seg.file.Sync(); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := seg.file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SegmentCount is a test helper returning the current number of segments.
func (s *FileSpool) SegmentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.segments)
}
