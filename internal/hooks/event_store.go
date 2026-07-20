package hooks

import (
	"bufio"
	"bytes"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

const eventStoreSchemaVersion = 1

type eventStorePaths struct {
	root         string
	segments     string
	ids          string
	state        string
	eventRoot    *os.Root
	segmentsRoot *os.Root
	idsRoot      *os.Root
}

func (store eventStorePaths) close() {
	if store.idsRoot != nil {
		_ = store.idsRoot.Close()
	}
	if store.segmentsRoot != nil {
		_ = store.segmentsRoot.Close()
	}
	if store.eventRoot != nil {
		_ = store.eventRoot.Close()
	}
}

type eventStoreState struct {
	SchemaVersion int    `json:"schema_version"`
	LastSequence  uint64 `json:"last_sequence"`
	TotalBytes    int64  `json:"total_bytes"`
	EventCount    int64  `json:"event_count"`
	LegacySHA256  string `json:"legacy_sha256,omitempty"`
	LegacyBytes   int64  `json:"legacy_bytes,omitempty"`
}

type eventIDClaim struct {
	SchemaVersion int    `json:"schema_version"`
	EventID       string `json:"event_id"`
	Sequence      uint64 `json:"sequence"`
}

// appendEventBatch stores each batch as a new immutable JSONL segment. Readers
// never lock or open a file that a writer replaces, so a large incident read
// cannot block concurrent Codex hooks. The small state and ID-claim files are
// writer-only and recover a segment committed just before a process crash.
func appendEventBatch(repo *repository.Repository, events []model.Event) error {
	if len(events) == 0 {
		return nil
	}
	var encoded bytes.Buffer
	incoming := make(map[string]struct{}, len(events))
	for _, event := range events {
		if err := ValidateEvent(event); err != nil {
			return err
		}
		if _, exists := incoming[event.ID]; exists {
			return fmt.Errorf("duplicate normalized event id %q in batch", event.ID)
		}
		incoming[event.ID] = struct{}{}
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		encoded.Write(data)
		encoded.WriteByte('\n')
	}
	if int64(encoded.Len()) > maxEventLedgerBytes {
		return fmt.Errorf("event batch exceeds the %d-byte ledger limit", maxEventLedgerBytes)
	}
	if err := repo.EnsureRuntime(); err != nil {
		return err
	}
	release, err := acquireEventLedgerLock(repo)
	if err != nil {
		return err
	}
	defer release()
	store, err := ensureEventStoreDirs(repo)
	if err != nil {
		return err
	}
	defer store.close()
	if err := cleanupEventStoreTemps(store); err != nil {
		return err
	}
	state, err := reconcileEventStore(repo, store)
	if err != nil {
		return err
	}
	if state.TotalBytes+int64(encoded.Len()) > maxEventLedgerBytes {
		return fmt.Errorf("event ledger would exceed %d bytes; archive and remove the complete event store before starting a new ledger", maxEventLedgerBytes)
	}
	for _, event := range events {
		if claim, exists, err := loadEventIDClaim(store, event.ID); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("duplicate normalized event id %q (already committed in segment %d)", event.ID, claim.Sequence)
		}
	}
	sequence := state.LastSequence + 1
	if err := commitEventSegment(store, sequence, encoded.Bytes(), len(events)); err != nil {
		return err
	}
	for _, event := range events {
		if err := ensureEventIDClaim(store, event.ID, sequence); err != nil {
			return err
		}
	}
	state.LastSequence = sequence
	state.TotalBytes += int64(encoded.Len())
	state.EventCount += int64(len(events))
	return saveEventStoreState(store, state)
}

// readEventStore takes a lock-free snapshot of immutable segment names. A
// concurrent writer can add the next complete segment, but can never mutate a
// segment in the snapshot. Legacy pre-v1 events.jsonl is read only when no v1
// segment exists; the first later append imports it without deleting it. When
// both forms exist, the legacy bytes must match the migrated first segment so
// an older concurrent writer cannot add events that readers silently omit.
func readEventStore(repo *repository.Repository) ([]model.Event, error) {
	if err := repo.EnsureRuntime(); err != nil {
		return nil, err
	}
	store, err := ensureEventStoreDirs(repo)
	if err != nil {
		return nil, err
	}
	defer store.close()
	segments, err := listEventSegments(store)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		_, events, exists, err := readLegacyEventLedger(repo)
		if err != nil {
			return nil, err
		}
		if exists {
			return events, nil
		}
		return nil, nil
	}
	state, stateExists, err := loadEventStoreState(store)
	if err != nil {
		return nil, err
	}
	legacyRaw, legacyEvents, legacyExists, err := readLegacyEventLedger(repo)
	if err != nil {
		return nil, err
	}
	if legacyExists {
		if !stateExists {
			return nil, errors.New("event store state is missing while legacy events.jsonl exists; a writer must reconcile the migration before reading")
		}
		digest := contentDigest(legacyRaw)
		first := segments[0]
		if first.size != int64(len(legacyRaw)) || first.events != int64(len(legacyEvents)) || first.sha256 != digest {
			return nil, errors.New("legacy events.jsonl does not match the migrated first segment; stop older BeforeDone writers before continuing")
		}
		if state.LegacySHA256 != "" && (state.LegacyBytes != int64(len(legacyRaw)) || state.LegacySHA256 != digest) {
			return nil, errors.New("legacy events.jsonl changed after migration; stop older BeforeDone writers before continuing")
		}
	}
	var result []model.Event
	seenIDs := make(map[string]struct{})
	var total int64
	for index, segment := range segments {
		expected := uint64(index + 1)
		if segment.sequence != expected {
			return nil, fmt.Errorf("event ledger segment gap: found %d, expected %d", segment.sequence, expected)
		}
		total += segment.size
		if total > maxEventLedgerBytes {
			return nil, fmt.Errorf("event ledger exceeds %d bytes", maxEventLedgerBytes)
		}
		events, err := validateSegmentContent(store, segment)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			if _, exists := seenIDs[event.ID]; exists {
				return nil, fmt.Errorf("duplicate event id %q in event ledger segment %d", event.ID, segment.sequence)
			}
			seenIDs[event.ID] = struct{}{}
			result = append(result, event)
		}
	}
	return result, nil
}

type eventSegment struct {
	sequence uint64
	path     string
	size     int64
	events   int64
	sha256   string
}

func listEventSegments(store eventStorePaths) ([]eventSegment, error) {
	dir, err := store.segmentsRoot.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	segments := make([]eventSegment, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if isEventTempName(name) {
			continue
		}
		sequence, eventCount, encodedBytes, digest, ok := parseEventSegmentName(name)
		if !ok {
			return nil, fmt.Errorf("unexpected entry in event segment directory: %s", name)
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, fmt.Errorf("event segment is not a regular file: %s", name)
		}
		info, err := store.segmentsRoot.Stat(name)
		if err != nil {
			return nil, err
		}
		if info.Size() != encodedBytes {
			return nil, fmt.Errorf("event segment %d size metadata is %d, file is %d", sequence, encodedBytes, info.Size())
		}
		segments = append(segments, eventSegment{sequence: sequence, path: filepath.Join(store.segments, name), size: info.Size(), events: eventCount, sha256: digest})
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].sequence < segments[j].sequence })
	return segments, nil
}

func reconcileEventStore(repo *repository.Repository, store eventStorePaths) (eventStoreState, error) {
	state, exists, err := loadEventStoreState(store)
	if err != nil {
		return eventStoreState{}, err
	}
	if exists && (state.SchemaVersion != eventStoreSchemaVersion || state.TotalBytes < 0 || state.TotalBytes > maxEventLedgerBytes || state.EventCount < 0 || state.LegacyBytes < 0) {
		return eventStoreState{}, errors.New("invalid event store state")
	}
	segments, err := listEventSegments(store)
	if err != nil {
		return eventStoreState{}, err
	}
	legacyRaw, legacyEvents, legacyExists, err := readLegacyEventLedger(repo)
	if err != nil {
		return eventStoreState{}, err
	}
	if len(segments) == 0 && legacyExists && len(legacyRaw) > 0 {
		if err := commitEventSegment(store, 1, legacyRaw, len(legacyEvents)); err != nil {
			return eventStoreState{}, fmt.Errorf("import legacy event ledger: %w", err)
		}
		segments, err = listEventSegments(store)
		if err != nil {
			return eventStoreState{}, err
		}
	}

	authoritative := eventStoreState{SchemaVersion: eventStoreSchemaVersion}
	seenIDs := make(map[string]uint64)
	if legacyExists {
		digest := contentDigest(legacyRaw)
		if state.LegacySHA256 != "" {
			if state.LegacyBytes != int64(len(legacyRaw)) || state.LegacySHA256 != digest {
				return eventStoreState{}, errors.New("legacy events.jsonl changed after migration; stop older BeforeDone writers before continuing")
			}
			authoritative.LegacySHA256 = state.LegacySHA256
			authoritative.LegacyBytes = state.LegacyBytes
		} else if len(segments) == 0 || segments[0].sha256 == digest && segments[0].events == int64(len(legacyEvents)) {
			authoritative.LegacySHA256 = digest
			authoritative.LegacyBytes = int64(len(legacyRaw))
		} else {
			return eventStoreState{}, errors.New("cannot prove that legacy events.jsonl matches the migrated first segment")
		}
	}

	for index, segment := range segments {
		expected := uint64(index + 1)
		if segment.sequence != expected {
			return eventStoreState{}, fmt.Errorf("event ledger segment gap: found %d, expected %d", segment.sequence, expected)
		}
		if err := validateExistingSegment(store, segment); err != nil {
			return eventStoreState{}, err
		}
		authoritative.LastSequence = segment.sequence
		authoritative.TotalBytes += segment.size
		authoritative.EventCount += segment.events
		if authoritative.TotalBytes > maxEventLedgerBytes {
			return eventStoreState{}, fmt.Errorf("event ledger exceeds %d bytes", maxEventLedgerBytes)
		}
		events, err := validateSegmentContent(store, segment)
		if err != nil {
			return eventStoreState{}, err
		}
		for _, event := range events {
			if previous, duplicate := seenIDs[event.ID]; duplicate {
				return eventStoreState{}, fmt.Errorf("duplicate normalized event id %q in segments %d and %d", event.ID, previous, segment.sequence)
			}
			seenIDs[event.ID] = segment.sequence
			if !exists || segment.sequence > state.LastSequence {
				if err := ensureEventIDClaim(store, event.ID, segment.sequence); err != nil {
					return eventStoreState{}, err
				}
			}
			claim, claimExists, err := loadEventIDClaim(store, event.ID)
			if err != nil {
				return eventStoreState{}, err
			}
			if !claimExists || claim.Sequence != segment.sequence {
				return eventStoreState{}, fmt.Errorf("event ID index does not match committed event %q in segment %d", event.ID, segment.sequence)
			}
		}
	}
	claimCount, err := countEventIDClaims(store)
	if err != nil {
		return eventStoreState{}, err
	}
	if claimCount != authoritative.EventCount {
		return eventStoreState{}, fmt.Errorf("event ID index has %d claims for %d committed events", claimCount, authoritative.EventCount)
	}
	if !exists || state != authoritative {
		if err := saveEventStoreState(store, authoritative); err != nil {
			return eventStoreState{}, err
		}
	}
	return authoritative, nil
}

func validateSegmentContent(store eventStorePaths, segment eventSegment) ([]model.Event, error) {
	raw, events, err := readEventJSONLAt(store.segmentsRoot, filepath.Base(segment.path), fmt.Sprintf("segment %d", segment.sequence))
	if err != nil {
		return nil, err
	}
	if int64(len(events)) != segment.events || contentDigest(raw) != segment.sha256 {
		return nil, fmt.Errorf("event segment %d does not match its immutable metadata", segment.sequence)
	}
	return events, nil
}

func countEventIDClaims(store eventStorePaths) (int64, error) {
	dir, err := store.idsRoot.Open(".")
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return 0, err
	}
	var count int64
	for _, entry := range entries {
		if isEventTempName(entry.Name()) {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if len(name) != 64 || !strings.HasSuffix(entry.Name(), ".json") {
			return 0, fmt.Errorf("unexpected entry in event ID directory: %s", entry.Name())
		}
		if _, err := hex.DecodeString(name); err != nil || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return 0, fmt.Errorf("unsafe event ID claim entry: %s", entry.Name())
		}
		count++
	}
	return count, nil
}

func ensureEventStoreDirs(repo *repository.Repository) (eventStorePaths, error) {
	root := filepath.Join(repo.RuntimeDir, "events")
	segments := filepath.Join(root, "segments")
	ids := filepath.Join(root, "ids")
	runtimeRoot, err := openRuntimeRoot(repo)
	if err != nil {
		return eventStorePaths{}, err
	}
	defer runtimeRoot.Close()
	if err := runtimeRoot.MkdirAll("events/segments", 0o700); err != nil {
		return eventStorePaths{}, err
	}
	if err := runtimeRoot.MkdirAll("events/ids", 0o700); err != nil {
		return eventStorePaths{}, err
	}
	if err := syncRoot(runtimeRoot); err != nil {
		return eventStorePaths{}, fmt.Errorf("sync event-store directories: %w", err)
	}
	eventRoot, err := runtimeRoot.OpenRoot("events")
	if err != nil {
		return eventStorePaths{}, err
	}
	if err := syncRoot(eventRoot); err != nil {
		eventRoot.Close()
		return eventStorePaths{}, fmt.Errorf("sync event-store child directories: %w", err)
	}
	segmentsRoot, err := eventRoot.OpenRoot("segments")
	if err != nil {
		eventRoot.Close()
		return eventStorePaths{}, err
	}
	idsRoot, err := eventRoot.OpenRoot("ids")
	if err != nil {
		segmentsRoot.Close()
		eventRoot.Close()
		return eventStorePaths{}, err
	}
	return eventStorePaths{
		root:         root,
		segments:     segments,
		ids:          ids,
		state:        filepath.Join(root, "state.json"),
		eventRoot:    eventRoot,
		segmentsRoot: segmentsRoot,
		idsRoot:      idsRoot,
	}, nil
}

func acquireEventLedgerLock(repo *repository.Repository) (func(), error) {
	runtimeRoot, err := openRuntimeRoot(repo)
	if err != nil {
		return nil, err
	}
	lockFile, err := runtimeRoot.OpenFile("events.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		runtimeRoot.Close()
		return nil, err
	}
	info, err := lockFile.Stat()
	if err != nil || !info.Mode().IsRegular() {
		lockFile.Close()
		runtimeRoot.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("event ledger lock path is not a regular file")
	}
	deadline := time.Now().Add(eventLockWait)
	for {
		locked, err := tryEventFileLock(lockFile)
		if err != nil {
			lockFile.Close()
			runtimeRoot.Close()
			return nil, fmt.Errorf("acquire event ledger lock: %w", err)
		}
		if locked {
			return func() {
				_ = unlockEventFile(lockFile)
				_ = lockFile.Close()
				_ = runtimeRoot.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			lockFile.Close()
			runtimeRoot.Close()
			return nil, errors.New("timed out waiting for the event ledger lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func openRuntimeRoot(repo *repository.Repository) (*os.Root, error) {
	gitRoot, err := os.OpenRoot(repo.GitDir)
	if err != nil {
		return nil, fmt.Errorf("open Git directory root: %w", err)
	}
	defer gitRoot.Close()
	if err := gitRoot.MkdirAll("beforedone", 0o700); err != nil {
		return nil, fmt.Errorf("create BeforeDone runtime below Git directory: %w", err)
	}
	if err := syncRoot(gitRoot); err != nil {
		return nil, fmt.Errorf("sync BeforeDone runtime directory: %w", err)
	}
	runtimeRoot, err := gitRoot.OpenRoot("beforedone")
	if err != nil {
		return nil, fmt.Errorf("open BeforeDone runtime below Git directory: %w", err)
	}
	return runtimeRoot, nil
}

func commitEventSegment(store eventStorePaths, sequence uint64, data []byte, eventCount int) error {
	path := eventSegmentPath(store, sequence, eventCount, int64(len(data)), contentDigest(data))
	name := filepath.Base(path)
	if _, err := store.segmentsRoot.Lstat(name); err == nil {
		return fmt.Errorf("event segment %d already exists", sequence)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := atomicCreateFileAt(store.segmentsRoot, ".event-segment-", name, data); err != nil {
		return fmt.Errorf("commit event ledger segment %d: %w", sequence, err)
	}
	return nil
}

func validateExistingSegment(store eventStorePaths, segment eventSegment) error {
	name := filepath.Base(segment.path)
	info, err := store.segmentsRoot.Lstat(name)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("segment %d is not a regular file", segment.sequence)
	}
	return nil
}

func eventSegmentPath(store eventStorePaths, sequence uint64, eventCount int, encodedBytes int64, digest string) string {
	return filepath.Join(store.segments, fmt.Sprintf("%020d-%010d-%016x-%s.jsonl", sequence, eventCount, encodedBytes, digest))
}

func parseEventSegmentName(name string) (uint64, int64, int64, string, bool) {
	parts := strings.Split(strings.TrimSuffix(name, ".jsonl"), "-")
	if len(parts) != 4 || !strings.HasSuffix(name, ".jsonl") || len(parts[0]) != 20 || len(parts[1]) != 10 || len(parts[2]) != 16 || len(parts[3]) != 64 {
		return 0, 0, 0, "", false
	}
	sequence, sequenceErr := strconv.ParseUint(parts[0], 10, 64)
	eventCount, countErr := strconv.ParseInt(parts[1], 10, 64)
	encodedBytes, sizeErr := strconv.ParseInt(parts[2], 16, 64)
	_, digestErr := hex.DecodeString(parts[3])
	if sequenceErr != nil || countErr != nil || sizeErr != nil || digestErr != nil || sequence == 0 || eventCount <= 0 || encodedBytes <= 0 {
		return 0, 0, 0, "", false
	}
	return sequence, eventCount, encodedBytes, parts[3], true
}

func contentDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func loadEventStoreState(store eventStorePaths) (eventStoreState, bool, error) {
	data, exists, err := readRegularFileAt(store.eventRoot, "state.json", 16<<10)
	if err != nil || !exists {
		return eventStoreState{}, exists, err
	}
	var state eventStoreState
	if err := decodeSingleJSON(data, &state); err != nil {
		return eventStoreState{}, true, fmt.Errorf("decode event store state: %w", err)
	}
	return state, true, nil
}

func saveEventStoreState(store eventStorePaths, state eventStoreState) error {
	state.SchemaVersion = eventStoreSchemaVersion
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicReplaceFileAt(store.eventRoot, ".event-state-", "state.json", data)
}

func loadEventIDClaim(store eventStorePaths, eventID string) (eventIDClaim, bool, error) {
	path := eventIDClaimPath(store, eventID)
	data, exists, err := readRegularFileAt(store.idsRoot, filepath.Base(path), 4<<10)
	if err != nil || !exists {
		return eventIDClaim{}, exists, err
	}
	var claim eventIDClaim
	if err := decodeSingleJSON(data, &claim); err != nil {
		return eventIDClaim{}, true, fmt.Errorf("decode event ID claim: %w", err)
	}
	if claim.SchemaVersion != eventStoreSchemaVersion || claim.EventID == "" || claim.Sequence == 0 {
		return eventIDClaim{}, true, errors.New("invalid event ID claim")
	}
	if claim.EventID != eventID {
		return eventIDClaim{}, true, errors.New("event ID claim hash collision or corruption")
	}
	return claim, true, nil
}

func ensureEventIDClaim(store eventStorePaths, eventID string, sequence uint64) error {
	if claim, exists, err := loadEventIDClaim(store, eventID); err != nil {
		return err
	} else if exists {
		if claim.Sequence == sequence {
			return nil
		}
		return fmt.Errorf("duplicate normalized event id %q (segments %d and %d)", eventID, claim.Sequence, sequence)
	}
	claim := eventIDClaim{SchemaVersion: eventStoreSchemaVersion, EventID: eventID, Sequence: sequence}
	data, err := json.Marshal(claim)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicCreateFileAt(store.idsRoot, ".event-id-", filepath.Base(eventIDClaimPath(store, eventID)), data)
}

func eventIDClaimPath(store eventStorePaths, eventID string) string {
	sum := sha256.Sum256([]byte(eventID))
	return filepath.Join(store.ids, hex.EncodeToString(sum[:])+".json")
}

func readLegacyEventLedger(repo *repository.Repository) ([]byte, []model.Event, bool, error) {
	runtimeRoot, err := openRuntimeRoot(repo)
	if err != nil {
		return nil, nil, false, err
	}
	defer runtimeRoot.Close()
	if _, err := runtimeRoot.Lstat("events.jsonl"); os.IsNotExist(err) {
		return nil, nil, false, nil
	} else if err != nil {
		return nil, nil, false, err
	}
	raw, events, err := readEventJSONLAt(runtimeRoot, "events.jsonl", "legacy events.jsonl")
	return raw, events, true, err
}

func readEventJSONL(root, path, label string) ([]byte, []model.Event, error) {
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, err
	}
	defer rootHandle.Close()
	return readEventJSONLAt(rootHandle, filepath.Base(path), label)
}

func readEventJSONLAt(root *os.Root, name, label string) ([]byte, []model.Event, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("path is not a regular file: %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxEventLedgerBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > maxEventLedgerBytes {
		return nil, nil, fmt.Errorf("event ledger exceeds %d bytes", maxEventLedgerBytes)
	}
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		return nil, nil, errors.New("event ledger has an incomplete final JSONL record (missing newline)")
	}
	var events []model.Event
	seenIDs := make(map[string]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), MaxEventBytes+1)
	line := 0
	for scanner.Scan() {
		line++
		var event model.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, nil, fmt.Errorf("decode event ledger %s line %d: %w", label, line, err)
		}
		if err := ValidateEvent(event); err != nil {
			return nil, nil, fmt.Errorf("invalid event ledger %s line %d: %w", label, line, err)
		}
		if _, exists := seenIDs[event.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate event id %q at event ledger %s line %d", event.ID, label, line)
		}
		seenIDs[event.ID] = struct{}{}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return raw, events, nil
}

func readRegularFileWithin(root, path string, limit int64) ([]byte, bool, error) {
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, false, err
	}
	defer rootHandle.Close()
	return readRegularFileAt(rootHandle, filepath.Base(path), limit)
}

func readRegularFileAt(root *os.Root, name string, limit int64) ([]byte, bool, error) {
	if info, err := root.Lstat(name); os.IsNotExist(err) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, true, fmt.Errorf("path is not a regular file: %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, true, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, true, err
	}
	if int64(len(data)) > limit {
		return nil, true, fmt.Errorf("file exceeds %d bytes: %s", limit, name)
	}
	return data, true, nil
}

func decodeSingleJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func atomicCreateFile(dir, pattern, finalPath string, data []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return atomicCreateFileAt(root, strings.TrimSuffix(pattern, "*.tmp"), filepath.Base(finalPath), data)
}

func atomicReplaceFile(dir, pattern, finalPath string, data []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return atomicReplaceFileAt(root, strings.TrimSuffix(pattern, "*.tmp"), filepath.Base(finalPath), data)
}

func atomicCreateFileAt(root *os.Root, tempPrefix, finalName string, data []byte) error {
	temp, tempName, err := createRootTemp(root, tempPrefix)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = root.Remove(tempName)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := root.Link(tempName, finalName); err != nil {
		return fmt.Errorf("create immutable file %q: %w (the event store filesystem must support same-directory hard links)", finalName, err)
	}
	if err := root.Remove(tempName); err != nil {
		return err
	}
	committed = true
	return syncRoot(root)
}

func atomicReplaceFileAt(root *os.Root, tempPrefix, finalName string, data []byte) error {
	if info, err := root.Lstat(finalName); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("replacement target is not a regular file: %s", finalName)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	temp, tempName, err := createRootTemp(root, tempPrefix)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = root.Remove(tempName)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := root.Rename(tempName, finalName); err != nil {
		return err
	}
	committed = true
	return syncRoot(root)
}

func createRootTemp(root *os.Root, prefix string) (*os.File, string, error) {
	for range 100 {
		var random [16]byte
		if _, err := cryptorand.Read(random[:]); err != nil {
			return nil, "", err
		}
		name := prefix + hex.EncodeToString(random[:]) + ".tmp"
		file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return file, name, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("could not allocate a unique event-store temp file")
}

func cleanupEventStoreTemps(store eventStorePaths) error {
	for _, root := range []*os.Root{store.eventRoot, store.segmentsRoot, store.idsRoot} {
		dir, err := root.Open(".")
		if err != nil {
			return err
		}
		entries, err := dir.ReadDir(-1)
		_ = dir.Close()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !isEventTempName(entry.Name()) {
				continue
			}
			info, err := root.Lstat(entry.Name())
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return fmt.Errorf("refusing to remove unsafe event-store temp path: %s", entry.Name())
			}
			if err := root.Remove(entry.Name()); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func isEventTempName(name string) bool {
	return strings.HasPrefix(name, ".event-segment-") && strings.HasSuffix(name, ".tmp") ||
		strings.HasPrefix(name, ".event-state-") && strings.HasSuffix(name, ".tmp") ||
		strings.HasPrefix(name, ".event-id-") && strings.HasSuffix(name, ".tmp")
}

func syncRoot(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	syncErr := syncDirectoryFile(dir)
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
