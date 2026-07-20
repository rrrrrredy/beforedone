package evidence

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
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
	"time"

	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

const producer = "beforedone.check"

func EnsureKey(repo *repository.Repository) error {
	if err := repo.EnsureRuntime(); err != nil {
		return err
	}
	path := filepath.Join(repo.RuntimeDir, "receipt.key")
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != 64 {
			return errors.New("invalid receipt signing key")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	return writeExclusive(path, []byte(hex.EncodeToString(key)), 0o600)
}

func NewID(prefix string) string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("%s-%s-%s", prefix, time.Now().UTC().Format("20060102T150405.000Z"), hex.EncodeToString(raw))
}

func Sign(repo *repository.Repository, receipt *model.Receipt) error {
	if err := validateReceiptContract(receipt); err != nil {
		return err
	}
	key, err := readKey(repo)
	if err != nil {
		return err
	}
	data, err := canonicalReceipt(receipt)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	receipt.Signature = "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
	return nil
}

func VerifySignature(repo *repository.Repository, receipt *model.Receipt) error {
	if err := validateReceiptContract(receipt); err != nil {
		return err
	}
	const prefix = "hmac-sha256:"
	if !strings.HasPrefix(receipt.Signature, prefix) {
		return errors.New("receipt signature is missing")
	}
	want, err := hex.DecodeString(strings.TrimPrefix(receipt.Signature, prefix))
	if err != nil {
		return errors.New("receipt signature is malformed")
	}
	key, err := readKey(repo)
	if err != nil {
		return err
	}
	data, err := canonicalReceipt(receipt)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	if !hmac.Equal(want, mac.Sum(nil)) {
		return errors.New("receipt signature does not match")
	}
	return nil
}

func validateReceiptContract(receipt *model.Receipt) error {
	if receipt == nil || receipt.SchemaVersion != model.SchemaVersion || receipt.Producer != producer || !receipt.Verdict.Valid() {
		return errors.New("receipt contract is invalid")
	}
	if !safeToken(receipt.ID, 256) || !safeToken(receipt.CheckID, 64) {
		return errors.New("receipt id or check_id is invalid")
	}
	if len(receipt.Argv) == 0 || strings.TrimSpace(receipt.Argv[0]) == "" {
		return errors.New("receipt argv is empty")
	}
	for _, arg := range receipt.Argv {
		if strings.ContainsRune(arg, '\x00') {
			return errors.New("receipt argv contains NUL")
		}
	}
	if receipt.StartedAt.IsZero() || receipt.FinishedAt.IsZero() || receipt.FinishedAt.Before(receipt.StartedAt) {
		return errors.New("receipt timestamps are invalid")
	}
	if receipt.Verdict == model.Pass && (receipt.ExitCode != 0 || receipt.Error != "") {
		return errors.New("PASS receipt must have exit_code 0 and no error")
	}
	if receipt.Verdict == model.Fail && receipt.ExitCode == 0 {
		return errors.New("FAIL receipt must have a non-zero exit_code")
	}
	if !validDigest(receipt.RelevantFingerprint, "sha256:") || !validDigest(receipt.LogSHA256, "sha256:") {
		return errors.New("receipt digest is invalid")
	}
	if receipt.RelevantFileCount < 0 || strings.TrimSpace(receipt.BeforeDoneVersion) == "" {
		return errors.New("receipt metadata is invalid")
	}
	if filepath.IsAbs(receipt.WorkingDirectory) || filepath.IsAbs(receipt.LogPath) || receipt.LogPath == "" {
		return errors.New("receipt path is invalid")
	}
	for _, value := range []string{filepath.ToSlash(receipt.WorkingDirectory), filepath.ToSlash(receipt.LogPath)} {
		for _, part := range strings.Split(value, "/") {
			if part == ".." {
				return errors.New("receipt path escapes its root")
			}
		}
	}
	return nil
}

func safeToken(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func validDigest(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(raw) == sha256.Size
}

func canonicalReceipt(receipt *model.Receipt) ([]byte, error) {
	copy := *receipt
	copy.Signature = ""
	copy.Fresh = nil
	copy.StaleReason = ""
	return json.Marshal(copy)
}

func Save(repo *repository.Repository, receipt *model.Receipt) (string, error) {
	if err := Sign(repo, receipt); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	dir := filepath.Join(repo.RuntimeDir, "receipts")
	path := filepath.Join(dir, receipt.ID+".json")
	if err := atomicWrite(path, data, 0o600); err != nil {
		return "", err
	}
	latest := filepath.Join(dir, "latest-"+receipt.CheckID+".json")
	if err := atomicWrite(latest, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func LoadLatest(repo *repository.Repository, checkID string) (*model.Receipt, error) {
	if checkID != "" {
		if !safeToken(checkID, 64) {
			return nil, fmt.Errorf("invalid check id %q", checkID)
		}
		return loadReceipt(filepath.Join(repo.RuntimeDir, "receipts", "latest-"+checkID+".json"))
	}
	entries, err := os.ReadDir(filepath.Join(repo.RuntimeDir, "receipts"))
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "receipt-") && strings.HasSuffix(entry.Name(), ".json") {
			candidates = append(candidates, filepath.Join(repo.RuntimeDir, "receipts", entry.Name()))
		}
	}
	if len(candidates) == 0 {
		return nil, os.ErrNotExist
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, _ := os.Stat(candidates[i])
		b, _ := os.Stat(candidates[j])
		return a.ModTime().After(b.ModTime())
	})
	return loadReceipt(candidates[0])
}

func ListLatest(repo *repository.Repository) ([]*model.Receipt, error) {
	entries, err := os.ReadDir(filepath.Join(repo.RuntimeDir, "receipts"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipts []*model.Receipt
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "latest-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		r, err := loadReceipt(filepath.Join(repo.RuntimeDir, "receipts", entry.Name()))
		if err != nil {
			continue
		}
		receipts = append(receipts, r)
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].CheckID < receipts[j].CheckID })
	return receipts, nil
}

func loadReceipt(path string) (*model.Receipt, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const maxReceiptBytes = 2 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxReceiptBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReceiptBytes {
		return nil, errors.New("receipt exceeds 2 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var receipt model.Receipt
	if err := dec.Decode(&receipt); err != nil {
		return nil, err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("receipt contains trailing JSON data")
		}
		return nil, fmt.Errorf("receipt contains invalid trailing data: %w", err)
	}
	return &receipt, nil
}

func readKey(repo *repository.Repository) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(repo.RuntimeDir, "receipt.key"))
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(string(data))
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid receipt signing key")
	}
	return key, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		// Windows cannot replace an existing destination atomically.
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return err
		}
		return os.Rename(name, path)
	}
	return nil
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
