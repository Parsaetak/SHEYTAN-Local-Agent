// store.go — retained verified calibration records (spec §4: "retain
// verified profile").
//
// A record is keyed by a MODEL fingerprint (absolute path + size + mtime)
// AND a MACHINE fingerprint (CPU cores + RAM + GPU identity). Retention
// is honest: a retained profile is only reused when BOTH fingerprints
// match, because a different model or a different machine invalidates
// the measurement. The file lives at {DataDir}/calibration.json and is
// written atomically.
package calibration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
)

// Record is one retained verified calibration result.
type Record struct {
	// ModelKey is the model fingerprint (ModelKey()).
	ModelKey string `json:"modelKey"`
	// HWKey is the machine fingerprint (HWKey()).
	HWKey string `json:"hwKey"`
	// ModelPath is the absolute model path at measurement time.
	ModelPath string `json:"modelPath"`
	// Winner is the winning candidate label.
	Winner string `json:"winner"`
	// Profile is the verified winning profile to re-apply.
	Profile recommendation.Recommendation `json:"profile"`
	// GenTokensPerSec / TTFTSeconds are the MEASURED values of the
	// winning run (never fabricated; surfaced as "measured on <date>").
	GenTokensPerSec float64   `json:"genTokensPerSec"`
	TTFTSeconds     float64   `json:"ttftSeconds"`
	MeasuredAt      time.Time `json:"measuredAt"`
}

// storeFile is the on-disk shape.
type storeFile struct {
	Records []Record `json:"records"`
}

// StoreFileName is the canonical file name under the data dir.
const StoreFileName = "calibration.json"

// ModelKey fingerprints a model file: absolute path + size + mtime.
func ModelKey(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("model key: %w", err)
	}

	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("model key: %w", err)
	}

	return fmt.Sprintf("%s|%d|%d", abs, fi.Size(), fi.ModTime().UnixNano()), nil
}

// HWKey fingerprints the machine: CPU cores + total RAM + primary GPU.
func HWKey(hw hardware.Profile) string {
	gpu := ""
	if g := hw.PrimaryGPU(); g != nil {
		gpu = g.Vendor + "/" + g.Name
	}

	return fmt.Sprintf(
		"cpu=%d/%d;ram=%d;gpu=%s",
		hw.CPU.PhysicalCores,
		hw.CPU.LogicalCores,
		hw.RAM.TotalBytes,
		gpu,
	)
}

// Load reads the retained records from {dataDir}/calibration.json.
// A missing file is an empty store, not an error.
func Load(dataDir string) (map[string]Record, error) {
	path := filepath.Join(dataDir, StoreFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Record{}, nil
		}
		return nil, fmt.Errorf("calibration store read: %w", err)
	}

	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("calibration store parse: %w", err)
	}

	out := make(map[string]Record, len(sf.Records))
	for _, r := range sf.Records {
		out[r.ModelKey+"@"+r.HWKey] = r
	}

	return out, nil
}

// Save persists the records atomically (tmp + rename).
func Save(dataDir string, records map[string]Record) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("calibration store dir: %w", err)
	}

	sf := storeFile{Records: make([]Record, 0, len(records))}
	for _, r := range records {
		sf.Records = append(sf.Records, r)
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("calibration store encode: %w", err)
	}

	path := filepath.Join(dataDir, StoreFileName)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("calibration store write: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("calibration store rename: %w", err)
	}

	return nil
}

// Retained looks up a verified record for the model fingerprint on this
// machine. The bool reports a usable hit.
func Retained(dataDir, modelKey, hwKey string) (Record, bool) {
	records, err := Load(dataDir)
	if err != nil {
		return Record{}, false
	}

	r, ok := records[modelKey+"@"+hwKey]
	return r, ok
}

// Retain stores a verified result.
func Retain(dataDir, modelKey, hwKey string, rec Record) error {
	records, err := Load(dataDir)
	if err != nil {
		// A corrupt store is replaced, not trusted.
		records = map[string]Record{}
	}

	rec.ModelKey = modelKey
	rec.HWKey = hwKey
	records[modelKey+"@"+hwKey] = rec

	return Save(dataDir, records)
}
