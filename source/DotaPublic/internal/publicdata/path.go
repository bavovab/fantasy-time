package publicdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

var releasePattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{12}$`)

type Pointer struct {
	Release string `json:"release"`
}

func SegmentKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ReadReleaseRoot(snapshotRoot string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(snapshotRoot, "current.json"))
	if err != nil {
		return "", err
	}
	var pointer Pointer
	if err := json.Unmarshal(raw, &pointer); err != nil {
		return "", err
	}
	if !releasePattern.MatchString(pointer.Release) {
		return "", errors.New("invalid snapshot release")
	}
	return filepath.Join(snapshotRoot, "releases", pointer.Release), nil
}

func ValidRelease(value string) bool {
	return releasePattern.MatchString(value)
}
