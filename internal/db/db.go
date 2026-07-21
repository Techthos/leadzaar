// Package db is the persistence layer for Leadzaar. It is the only package
// that imports bbolt; every other layer goes through the Store, receiving plain
// models, never *bolt.Tx or transaction-scoped byte slices. See
// docs/SPECIFICATIONS.md (Persistence Design) and .claude/rules/db-rules.md.
package db

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

// Bucket names. Defined as package-level []byte constants, never inline literals.
var (
	bucketLeads          = []byte("leads")
	bucketContacts       = []byte("contacts")
	bucketDeals          = []byte("deals")
	bucketCompanies      = []byte("companies")
	bucketOffers         = []byte("offers")
	bucketContactByEmail = []byte("idx_contact_by_email")
	bucketDealByContact  = []byte("idx_deal_by_contact")
	bucketOfferByLead    = []byte("idx_offer_by_lead")
)

// allBuckets is the full set created at startup (see migrate).
var allBuckets = [][]byte{
	bucketLeads,
	bucketContacts,
	bucketDeals,
	bucketCompanies,
	bucketOffers,
	bucketContactByEmail,
	bucketDealByContact,
	bucketOfferByLead,
}

// ErrNotFound is returned when a record with the requested ID does not exist.
// Match it with errors.Is, never by string.
var ErrNotFound = errors.New("not found")

// errEmptyName is the validation failure for a required name/title left blank.
var errEmptyName = errors.New("name must not be empty")

// putJSON marshals v and stores it under key in bucket b.
func putJSON(b *bolt.Bucket, key []byte, v any) error {
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return b.Put(key, encoded)
}

// Store exposes repository operations for all entities. It follows the
// connection-per-operation strategy (see docs/bbolt-concurrent-access-strategy.md):
// it holds only the file path, never a live *bolt.DB, so an idle Store keeps no
// file lock. Each read opens a short-lived read-only handle and each write a
// short-lived read-write handle, which lets the TUI and MCP surfaces run as
// separate processes against the same file. Cross-entity use-cases (lead
// conversion, contact cascade-delete) still run inside one transaction.
type Store struct {
	path string
	now  func() time.Time
}

// Open-retry tuning. The per-attempt Timeout is kept short and retried with
// backoff so a brief cross-process lock collision becomes a sub-second wait
// instead of the hard "timeout" error a long single Timeout would surface.
const (
	openTimeout   = 75 * time.Millisecond  // per-attempt lock-acquire timeout
	openMaxWait   = 3 * time.Second        // total budget across retries
	openBackoffHi = 200 * time.Millisecond // backoff ceiling
)

// Option configures a Store at Open time.
type Option func(*Store)

// WithClock overrides the time source (default time.Now). Intended for tests
// that need deterministic, advanceable timestamps.
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// Open prepares a Store for the bbolt file at path and returns it ready to use.
// It creates the parent directory if missing, then bootstraps by opening
// read-write once to create the file and run the bucket migration, then closing
// — the subsequent read-only opens that reads use cannot create a missing file.
// After Open returns, the Store holds no file lock.
func Open(path string, opts ...Option) (*Store, error) {
	s := &Store{path: path, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db directory %q: %w", dir, err)
		}
	}
	bdb, err := s.open(false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = bdb.Close() }()
	if err := s.migrate(bdb); err != nil {
		return nil, err
	}
	return s, nil
}

// Close releases the Store. In the connection-per-operation model the Store
// holds no open handle between operations, so there is nothing to close; the
// method is kept for API symmetry and lifecycle clarity at call sites.
func (s *Store) Close() error { return nil }

// open acquires a fresh handle for one operation, retrying on a contended lock
// with linear-capped backoff up to openMaxWait. readOnly selects bbolt's shared
// (read) lock vs. its exclusive (write) lock. The retry deadline uses real wall
// time, independent of any injected test clock.
func (s *Store) open(readOnly bool) (*bolt.DB, error) {
	opts := &bolt.Options{Timeout: openTimeout, ReadOnly: readOnly}
	deadline := time.Now().Add(openMaxWait)
	backoff := 10 * time.Millisecond
	for {
		bdb, err := bolt.Open(s.path, 0o600, opts)
		if err == nil {
			return bdb, nil
		}
		// Only a contended lock is retryable; surface anything else immediately.
		if !errors.Is(err, bolterrors.ErrTimeout) || time.Now().After(deadline) {
			return nil, fmt.Errorf("open bbolt at %q (readOnly=%v): %w", s.path, readOnly, err)
		}
		time.Sleep(backoff)
		if backoff < openBackoffHi {
			backoff *= 2
		}
	}
}

// view runs fn in a read-only transaction on its own short-lived handle.
func (s *Store) view(fn func(*bolt.Tx) error) error {
	bdb, err := s.open(true)
	if err != nil {
		return err
	}
	defer func() { _ = bdb.Close() }()
	return bdb.View(fn)
}

// update runs fn in a read-write transaction on its own short-lived handle.
func (s *Store) update(fn func(*bolt.Tx) error) error {
	bdb, err := s.open(false)
	if err != nil {
		return err
	}
	defer func() { _ = bdb.Close() }()
	return bdb.Update(fn)
}

// TxID returns bbolt's latest committed transaction ID. It increases on every
// committed write, so a long-lived reader (e.g. the TUI) can poll it to detect
// that another process has modified the database without scanning any data. See
// docs/bbolt-concurrent-access-strategy.md.
func (s *Store) TxID() (int, error) {
	var id int
	err := s.view(func(tx *bolt.Tx) error {
		id = tx.ID()
		return nil
	})
	return id, err
}

// migrate creates every required top-level bucket idempotently and then runs the
// idempotent data migrations, all in one txn.
func (s *Store) migrate(bdb *bolt.DB) error {
	return bdb.Update(func(tx *bolt.Tx) error {
		for _, name := range allBuckets {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %q: %w", name, err)
			}
		}
		return s.migrateLegacyCompany(tx)
	})
}

// itob encodes a surrogate ID big-endian so that byte-sorted key order matches
// numeric (creation) order.
func itob(id uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, id)
	return b
}

// btoi decodes an 8-byte big-endian ID.
func btoi(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

// normEmail normalizes an email for indexing and comparison: trimmed, lowercased.
func normEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// contactEmailIndexKey builds the composite key for idx_contact_by_email:
// normalized-email + 0x00 + big-endian contactID. The trailing ID disambiguates
// duplicate emails, and the 0x00 separator bounds the email prefix for scans.
func contactEmailIndexKey(email string, id uint64) []byte {
	e := normEmail(email)
	key := make([]byte, 0, len(e)+1+8)
	key = append(key, e...)
	key = append(key, 0x00)
	key = append(key, itob(id)...)
	return key
}

// contactEmailIndexPrefix is the scan prefix matching every contact with email.
func contactEmailIndexPrefix(email string) []byte {
	e := normEmail(email)
	prefix := make([]byte, 0, len(e)+1)
	prefix = append(prefix, e...)
	prefix = append(prefix, 0x00)
	return prefix
}

// dealByContactIndexKey builds the composite key for idx_deal_by_contact:
// big-endian contactID + big-endian dealID. Prefix-scanning by the contactID
// half yields all of that contact's deals in deal-creation order.
func dealByContactIndexKey(contactID, dealID uint64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key[:8], contactID)
	binary.BigEndian.PutUint64(key[8:], dealID)
	return key
}

// offerByLeadIndexKey builds the composite key for idx_offer_by_lead:
// big-endian leadID + big-endian offerID. Prefix-scanning by the leadID half
// yields all of that lead's offers in offer-creation order; it also drives the
// cascade delete when a lead is removed.
func offerByLeadIndexKey(leadID, offerID uint64) []byte {
	key := make([]byte, 16)
	binary.BigEndian.PutUint64(key[:8], leadID)
	binary.BigEndian.PutUint64(key[8:], offerID)
	return key
}
