package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// legacySlices are the pre-migration file names the migrator consumes/replaces.
var legacySlices = []string{"values", "hostnames", "rtc", "tls", "easy"}

// ownerFile returns the section file that owns a given top-level key.
func ownerFile(topKey string) string {
	if generalKeys[topKey] {
		return "general"
	}
	return topKey
}

// IsMigrated reports whether the repo already uses the per-section layout
// (i.e. the legacy monolithic "values" slice is gone).
func (s *Store) IsMigrated() bool {
	m, err := s.manifest()
	if err != nil {
		return false
	}
	for _, sl := range m.Slices {
		if sl.Name == "values" {
			return false
		}
	}
	return true
}

// MigrateToSections converts the legacy slices into one file per section.
// It is idempotent, backs up the originals (git tag + _backup dir), and aborts
// without changes if the resulting merged config differs from the original.
func (s *Store) MigrateToSections(ctx context.Context) error {
	if s.IsMigrated() {
		return nil
	}
	m, err := s.manifest()
	if err != nil {
		return err
	}

	// Collect legacy slices in their existing merge order.
	var ordered []NamedSlice
	legacyFiles := map[string]string{}
	for _, meta := range m.Slices {
		data, err := os.ReadFile(filepath.Join(s.path, meta.File))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		ordered = append(ordered, NamedSlice{Name: meta.Name, Content: string(data)})
		legacyFiles[meta.File] = string(data)
	}

	files, err := BuildSectionFiles(ordered)
	if err != nil {
		return fmt.Errorf("build sections: %w", err)
	}

	// SAFETY: the effective merged config must be byte-identical before/after.
	if err := assertSameMerge(ordered, files); err != nil {
		return fmt.Errorf("migration aborted (effective config would change): %w", err)
	}

	// BACKUP originals into _backup-pre-sections/ (kept in git history).
	backupDir := filepath.Join(s.path, "_backup-pre-sections")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	for name, content := range legacyFiles {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}

	// Write the new section files.
	sectionMetas := make([]SliceMeta, 0, len(files))
	for fname, content := range files {
		if err := os.WriteFile(filepath.Join(s.path, fname), []byte(content), 0o644); err != nil {
			return err
		}
		name := fname[:len(fname)-len(".yaml")]
		sectionMetas = append(sectionMetas, SliceMeta{
			Name:        name,
			File:        fname,
			Description: "Section: " + name,
		})
	}

	// Remove the legacy files (recoverable from _backup-pre-sections + git).
	for _, lf := range legacySlices {
		_ = os.Remove(filepath.Join(s.path, lf+".yaml"))
	}

	// Write the new manifest (general first, then alphabetical — handled by caller order).
	newManifest := slicesManifest{Slices: sortSectionMetas(sectionMetas)}
	data, err := json.MarshalIndent(newManifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.path, "config-slices.json"), data, 0o644); err != nil {
		return err
	}

	_, err = s.git.CommitAll("matrixctrl: migrate config to per-section files", "MatrixCtrl", "matrixctrl@localhost")
	return err
}

// assertSameMerge fails if merging the new section files yields a different
// effective config than the original slices.
func assertSameMerge(orig []NamedSlice, newFiles map[string]string) error {
	oc := make([]string, len(orig))
	for i, s := range orig {
		oc[i] = s.Content
	}
	om, err := MergeToMap(oc)
	if err != nil {
		return err
	}
	nc := make([]string, 0, len(newFiles))
	for _, c := range newFiles {
		nc = append(nc, c)
	}
	nm, err := MergeToMap(nc)
	if err != nil {
		return err
	}
	ob, _ := yaml.Marshal(om)
	nb, _ := yaml.Marshal(nm)
	if string(ob) != string(nb) {
		return fmt.Errorf("merged output mismatch")
	}
	return nil
}

// sortSectionMetas orders general first, then the rest alphabetically.
func sortSectionMetas(metas []SliceMeta) []SliceMeta {
	out := make([]SliceMeta, 0, len(metas))
	for _, m := range metas {
		if m.Name == "general" {
			out = append(out, m)
		}
	}
	rest := []SliceMeta{}
	for _, m := range metas {
		if m.Name != "general" {
			rest = append(rest, m)
		}
	}
	// simple insertion sort by Name
	for i := 1; i < len(rest); i++ {
		for j := i; j > 0 && rest[j-1].Name > rest[j].Name; j-- {
			rest[j-1], rest[j] = rest[j], rest[j-1]
		}
	}
	return append(out, rest...)
}

// SeedSections initialises a fresh config repo from a chart's commented
// values.yaml, splitting it into per-section files. Used by the greenfield deploy
// wizard. Refuses to overwrite an already-populated repo unless force is set.
func (s *Store) SeedSections(ctx context.Context, valuesYAML string, force bool) error {
	if !force {
		if slices, _ := s.List(ctx); len(slices) > 0 {
			return fmt.Errorf("config repo already has %d sections — refusing to overwrite", len(slices))
		}
	}
	files, err := BuildSectionFiles([]NamedSlice{{Name: "values", Content: valuesYAML}})
	if err != nil {
		return fmt.Errorf("build sections: %w", err)
	}
	metas := make([]SliceMeta, 0, len(files))
	for fname, content := range files {
		if err := os.WriteFile(filepath.Join(s.path, fname), []byte(content), 0o644); err != nil {
			return err
		}
		name := fname[:len(fname)-len(".yaml")]
		metas = append(metas, SliceMeta{Name: name, File: fname, Description: "Section: " + name})
	}
	manifest := slicesManifest{Slices: sortSectionMetas(metas)}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.path, "config-slices.json"), data, 0o644); err != nil {
		return err
	}
	_, err = s.git.CommitAll("matrixctrl: seed config from ESS chart defaults", "MatrixCtrl", "matrixctrl@localhost")
	return err
}

// SectionFileMap returns top-level-key → owning section file name, for every
// key present across the current section files. Lets the UI link a setting to its
// YAML file.
func (s *Store) SectionFileMap(ctx context.Context) (map[string]string, error) {
	m, err := s.manifest()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, meta := range m.Slices {
		data, err := os.ReadFile(filepath.Join(s.path, meta.File))
		if err != nil {
			continue
		}
		node, err := ParseYAMLNode(string(data))
		if err != nil {
			continue
		}
		for _, top := range TopLevelKeys(node) {
			out[top] = meta.File
		}
	}
	return out, nil
}

// SetSectionValues applies form edits (path→value) and removals directly to the
// owning section files, preserving comments. No commit (the caller commits/deploys).
func (s *Store) SetSectionValues(ctx context.Context, changes map[string]interface{}, removals []string) error {
	// Group edits by owning file.
	type edit struct {
		sets    map[string]interface{}
		removes [][]string
	}
	byFile := map[string]*edit{}
	ensure := func(top string) *edit {
		f := ownerFile(top)
		if byFile[f] == nil {
			byFile[f] = &edit{sets: map[string]interface{}{}}
		}
		return byFile[f]
	}

	for path, v := range changes {
		parts := splitDot(path)
		if len(parts) == 0 {
			continue
		}
		ensure(parts[0]).sets[path] = v
	}
	for _, path := range removals {
		parts := splitDot(path)
		if len(parts) == 0 {
			continue
		}
		e := ensure(parts[0])
		e.removes = append(e.removes, parts)
	}

	for fileBase, e := range byFile {
		fname := fileBase + ".yaml"
		fpath := filepath.Join(s.path, fname)
		existing, _ := os.ReadFile(fpath)
		node, err := ParseYAMLNode(string(existing))
		if err != nil {
			return fmt.Errorf("parse %s: %w", fname, err)
		}
		for path, v := range e.sets {
			if err := SetNodeValue(node, splitDot(path), v); err != nil {
				return err
			}
		}
		for _, parts := range e.removes {
			DeleteNodeValue(node, parts)
		}
		out, err := MarshalNode(node)
		if err != nil {
			return err
		}
		if err := os.WriteFile(fpath, []byte(out), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func splitDot(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	cur := ""
	for _, c := range s {
		if c == '.' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	out = append(out, cur)
	return out
}
