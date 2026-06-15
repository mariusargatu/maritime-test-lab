// Package jsonlstore is the onBOARD client's disk queue: an append-only JSONL
// file keyed by client_request_id. Append adds a line and fsyncs; Ack compacts
// the file by rewriting it. A fresh Store over the same path replays the file,
// so the queue survives a crash (cold-reopen) — the offline-first guarantee.
package jsonlstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"

	"maritime-test-lab/clients/onboard-sync/domain"
)

// Store implements domain.Store over a JSONL file.
type Store struct {
	path string
	mu   sync.Mutex
	ops  map[string]domain.Operation
}

// Open loads any existing queue at path (replaying the file) and returns a Store.
func Open(path string) (*Store, error) {
	s := &Store{path: path, ops: map[string]domain.Operation{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil // a fresh queue
	}
	if err != nil {
		return fmt.Errorf("jsonlstore open %s: %w", s.path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var op domain.Operation
		if err := json.Unmarshal(scanner.Bytes(), &op); err != nil {
			return fmt.Errorf("jsonlstore parse %s: %w", s.path, err)
		}
		s.ops[op.ClientRequestID] = op // last write per id wins on replay
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("jsonlstore scan %s: %w", s.path, err)
	}
	return nil
}

// Append upserts the operation and durably appends it to the file.
func (s *Store) Append(op domain.Operation) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops[op.ClientRequestID] = op

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("jsonlstore append open %s: %w", s.path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("jsonlstore append close: %w", cerr)
		}
	}()

	line, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("jsonlstore marshal: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("jsonlstore write: %w", err)
	}
	return f.Sync()
}

// Pending returns the queued operations, ordered by id for determinism.
func (s *Store) Pending() ([]domain.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Operation, 0, len(s.ops))
	for _, op := range s.ops {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClientRequestID < out[j].ClientRequestID })
	return out, nil
}

// Ack removes the operation and compacts the file.
func (s *Store) Ack(clientRequestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ops, clientRequestID)
	return s.compact()
}

// compact rewrites the file with the current operations (atomic via rename).
func (s *Store) compact() (err error) {
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("jsonlstore compact open: %w", err)
	}

	w := bufio.NewWriter(f)
	ids := make([]string, 0, len(s.ops))
	for id := range s.ops {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		line, mErr := json.Marshal(s.ops[id])
		if mErr != nil {
			_ = f.Close()
			return fmt.Errorf("jsonlstore compact marshal: %w", mErr)
		}
		if _, wErr := w.Write(append(line, '\n')); wErr != nil {
			_ = f.Close()
			return fmt.Errorf("jsonlstore compact write: %w", wErr)
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return fmt.Errorf("jsonlstore compact flush: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("jsonlstore compact sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("jsonlstore compact close: %w", err)
	}
	return os.Rename(tmp, s.path)
}
