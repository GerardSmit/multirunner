package winvm

import (
	"encoding/json"
	"os"
)

// metaPath is the sidecar path for a golden image.
func metaPath(golden string) string { return golden + ".meta.json" }

// LoadMeta reads the golden image's metadata sidecar (or a zero value if absent).
func LoadMeta(golden string) (GoldenMeta, error) {
	b, err := os.ReadFile(metaPath(golden))
	if err != nil {
		if os.IsNotExist(err) {
			return GoldenMeta{}, nil
		}
		return GoldenMeta{}, err
	}
	var m GoldenMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return GoldenMeta{}, err
	}
	return m, nil
}

// SaveMeta writes the golden image's metadata sidecar.
func SaveMeta(golden string, m GoldenMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(golden), b, 0o644)
}
