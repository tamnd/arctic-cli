package arctic

import (
	"encoding/json"
	"os"
)

// saveJSON writes v to path as indented JSON, atomically via a temp file and
// rename so a crash mid-write never leaves a half-written cache.
func saveJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadJSON reads path into v. A missing file is not an error; v is left as-is.
func loadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
