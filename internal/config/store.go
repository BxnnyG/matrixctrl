package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gitpkg "github.com/bxnnyg/matrixctrl/internal/git"
	"gopkg.in/yaml.v3"
)

// SliceMeta describes a single config slice file.
type SliceMeta struct {
	Name        string `json:"name"`
	File        string `json:"file"`
	Description string `json:"description,omitempty"`
}

// slicesManifest is what is stored in config-slices.json.
type slicesManifest struct {
	Slices []SliceMeta `json:"slices"`
}

// Slice holds metadata + content for a config slice.
type Slice struct {
	SliceMeta
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store manages YAML config slices stored in a git repository.
type Store struct {
	path string
	git  *gitpkg.Repo
}

// NewStore creates a Store backed by the given git repo.
func NewStore(path string, git *gitpkg.Repo) *Store {
	return &Store{path: path, git: git}
}

// Init seeds the config repo from srcDir if it has not been initialised yet.
// It writes config-slices.json with the default merge order and commits.
func (s *Store) Init(ctx context.Context, srcDir string) error {
	if s.git.HasCommits() {
		return nil
	}

	defaults := []SliceMeta{
		{Name: "values", File: "values.yaml", Description: "Main ESS configuration"},
		{Name: "hostnames", File: "hostnames.yaml", Description: "Ingress hostnames per component"},
		{Name: "rtc", File: "rtc.yaml", Description: "MatrixRTC / LiveKit SFU overrides"},
		{Name: "tls", File: "tls.yaml", Description: "cert-manager TLS configuration"},
	}

	for _, m := range defaults {
		src := filepath.Join(srcDir, m.File)
		dst := filepath.Join(s.path, m.File)
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read %s: %w", src, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
	}

	manifest := slicesManifest{Slices: defaults}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.path, "config-slices.json"), manifestData, 0o644); err != nil {
		return err
	}

	_, err = s.git.CommitAll(
		"matrixctrl: initial config import",
		"MatrixCtrl", "matrixctrl@localhost",
	)
	return err
}


// manifest reads config-slices.json from the repo.
func (s *Store) manifest() (*slicesManifest, error) {
	data, err := os.ReadFile(filepath.Join(s.path, "config-slices.json"))
	if err != nil {
		return nil, fmt.Errorf("config-slices.json missing: %w", err)
	}
	var m slicesManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// List returns all configured slices with their current content.
func (s *Store) List(_ context.Context) ([]Slice, error) {
	m, err := s.manifest()
	if err != nil {
		return nil, err
	}
	out := make([]Slice, 0, len(m.Slices))
	for _, meta := range m.Slices {
		sl, err := s.readSlice(meta)
		if err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, nil
}

// Get returns a single slice by name.
func (s *Store) Get(_ context.Context, name string) (*Slice, error) {
	m, err := s.manifest()
	if err != nil {
		return nil, err
	}
	for _, meta := range m.Slices {
		if meta.Name == name {
			sl, err := s.readSlice(meta)
			if err != nil {
				return nil, err
			}
			return &sl, nil
		}
	}
	return nil, fmt.Errorf("slice %q not found", name)
}

// Put writes new YAML content for a slice (validates YAML syntax, does not commit).
func (s *Store) Put(_ context.Context, name, content string) error {
	m, err := s.manifest()
	if err != nil {
		return err
	}
	var meta *SliceMeta
	for i, sl := range m.Slices {
		if sl.Name == name {
			meta = &m.Slices[i]
			break
		}
	}
	if meta == nil {
		return fmt.Errorf("slice %q not found", name)
	}

	// Validate YAML syntax.
	var v interface{}
	if err := yaml.Unmarshal([]byte(content), &v); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	return os.WriteFile(filepath.Join(s.path, meta.File), []byte(content), 0o644)
}

// Commit stages all changes and creates a git commit.
func (s *Store) Commit(_ context.Context, msg, userID string) (string, error) {
	author := userID
	if author == "" {
		author = "admin"
	}
	return s.git.CommitAll(msg, author, author+"@matrixctrl")
}

// MergedContent returns the ordered slice contents suitable for helm --values.
func (s *Store) MergedContent(_ context.Context) ([]string, error) {
	m, err := s.manifest()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(m.Slices))
	for _, meta := range m.Slices {
		data, err := os.ReadFile(filepath.Join(s.path, meta.File))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out = append(out, string(data))
	}
	return out, nil
}

// Diff returns a working-tree diff vs HEAD.
func (s *Store) Diff() (string, error) { return s.git.Diff() }

func (s *Store) readSlice(meta SliceMeta) (Slice, error) {
	p := filepath.Join(s.path, meta.File)
	data, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return Slice{}, err
	}
	info, _ := os.Stat(p)
	var updatedAt time.Time
	if info != nil {
		updatedAt = info.ModTime()
	}
	return Slice{
		SliceMeta: meta,
		Content:   string(data),
		UpdatedAt: updatedAt,
	}, nil
}
