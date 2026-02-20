package mk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const stateDir = ".mk"
const stateFile = ".mk/state.json"

// BuildState tracks build artifacts for content-based staleness detection.
type BuildState struct {
	Targets map[string]*TargetState `json:"targets"`
}

// TargetState records the state of a target at its last successful build.
type TargetState struct {
	RecipeHash  string            `json:"recipe_hash"`
	InputHashes map[string]string `json:"input_hashes"` // prereq path → content hash
	OutputHash  string            `json:"output_hash"`
	Prereqs     []string          `json:"prereqs"`
}

func LoadState() *BuildState {
	s := &BuildState{Targets: make(map[string]*TargetState)}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, s)
	if s.Targets == nil {
		s.Targets = make(map[string]*TargetState)
	}
	return s
}

func (s *BuildState) Save() error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile, data, 0o644)
}

// IsStale determines if a target needs rebuilding.
func (s *BuildState) IsStale(target string, prereqs []string, recipeText string) bool {
	ts := s.Targets[target]
	if ts == nil {
		return true
	}

	// Check if target file exists (for file targets)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return true
	}

	// Check recipe changed
	rh := hashString(recipeText)
	if ts.RecipeHash != rh {
		return true
	}

	// Check prerequisite set changed
	sortedPrereqs := make([]string, len(prereqs))
	copy(sortedPrereqs, prereqs)
	sort.Strings(sortedPrereqs)
	sortedOld := make([]string, len(ts.Prereqs))
	copy(sortedOld, ts.Prereqs)
	sort.Strings(sortedOld)
	if !stringSliceEqual(sortedPrereqs, sortedOld) {
		return true
	}

	// Check input content hashes
	for _, p := range prereqs {
		h, err := hashFile(p)
		if err != nil {
			return true
		}
		if ts.InputHashes[p] != h {
			return true
		}
	}

	return false
}

// Record records a successful build.
func (s *BuildState) Record(target string, prereqs []string, recipeText string) {
	ts := &TargetState{
		RecipeHash:  hashString(recipeText),
		InputHashes: make(map[string]string),
		Prereqs:     prereqs,
	}
	for _, p := range prereqs {
		h, err := hashFile(p)
		if err == nil {
			ts.InputHashes[p] = h
		}
	}
	if h, err := hashFile(target); err == nil {
		ts.OutputHash = h
	}
	s.Targets[target] = ts
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CleanPath normalizes paths for consistent state tracking.
func CleanPath(p string) string {
	return filepath.Clean(p)
}
