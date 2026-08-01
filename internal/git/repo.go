package git

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type CommitInfo struct {
	SHA     string    `json:"sha"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Time    time.Time `json:"time"`
}

type Repo struct {
	path string
	r    *gogit.Repository
}

// OpenOrInit opens an existing repo at path or initialises a new one.
func OpenOrInit(path string) (*Repo, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", path, err)
	}
	r, err := gogit.PlainOpen(path)
	if errors.Is(err, gogit.ErrRepositoryNotExists) {
		r, err = gogit.PlainInit(path, false)
	}
	if err != nil {
		return nil, fmt.Errorf("open/init repo: %w", err)
	}
	return &Repo{path: path, r: r}, nil
}

// Path returns the filesystem path of the repo working tree.
func (repo *Repo) Path() string { return repo.path }

// HasCommits returns true if the repo has at least one commit.
func (repo *Repo) HasCommits() bool {
	ref, err := repo.r.Head()
	return err == nil && ref != nil
}

// CommitAll stages every change in the working tree and creates a commit.
// Returns the short SHA of the new commit.
func (repo *Repo) CommitAll(msg, authorName, authorEmail string) (string, error) {
	wt, err := repo.r.Worktree()
	if err != nil {
		return "", err
	}
	if err := wt.AddGlob("."); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	hash, err := wt.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  authorName,
			Email: authorEmail,
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	return hash.String()[:8], nil
}

// Log returns up to limit recent commits.
func (repo *Repo) Log(limit int) ([]CommitInfo, error) {
	if !repo.HasCommits() {
		return nil, nil
	}
	iter, err := repo.r.Log(&gogit.LogOptions{})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var out []CommitInfo
	for range limit {
		c, err := iter.Next()
		if err != nil {
			break
		}
		out = append(out, CommitInfo{
			SHA:     c.Hash.String()[:8],
			Message: c.Message,
			Author:  c.Author.Name,
			Time:    c.Author.When,
		})
	}
	return out, nil
}

// GetFileAtCommit returns the content of a file at a specific commit SHA (short or full).
func (repo *Repo) GetFileAtCommit(sha, filePath string) (string, error) {
	hash, err := repo.resolveShortSHA(sha)
	if err != nil {
		return "", err
	}
	commit, err := repo.r.CommitObject(hash)
	if err != nil {
		return "", fmt.Errorf("commit %s: %w", sha, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", err
	}
	return fileContentFromTree(tree, filePath)
}

// DiffAtCommit returns a unified diff introduced by a specific commit vs its parent.
func (repo *Repo) DiffAtCommit(sha string) (string, error) {
	hash, err := repo.resolveShortSHA(sha)
	if err != nil {
		return "", err
	}
	commit, err := repo.r.CommitObject(hash)
	if err != nil {
		return "", err
	}
	if commit.NumParents() == 0 {
		return "(initial commit — no parent to diff against)", nil
	}
	parent, err := commit.Parent(0)
	if err != nil {
		return "", err
	}
	parentTree, err := parent.Tree()
	if err != nil {
		return "", err
	}
	commitTree, err := commit.Tree()
	if err != nil {
		return "", err
	}
	changes, err := parentTree.Diff(commitTree)
	if err != nil {
		return "", err
	}
	var result string
	for _, change := range changes {
		patch, err := change.Patch()
		if err != nil {
			continue
		}
		result += patch.String()
	}
	return result, nil
}

// ResetToCommit hard-resets the working tree to the given commit SHA.
// All uncommitted changes are discarded.
func (repo *Repo) ResetToCommit(sha string) error {
	hash, err := repo.resolveShortSHA(sha)
	if err != nil {
		return err
	}
	wt, err := repo.r.Worktree()
	if err != nil {
		return err
	}
	return wt.Reset(&gogit.ResetOptions{
		Commit: hash,
		Mode:   gogit.HardReset,
	})
}

func (repo *Repo) resolveShortSHA(sha string) (plumbing.Hash, error) {
	// Try exact match first.
	hash := plumbing.NewHash(sha)
	if _, err := repo.r.CommitObject(hash); err == nil {
		return hash, nil
	}
	// Walk log to find a commit whose SHA starts with sha.
	iter, err := repo.r.Log(&gogit.LogOptions{})
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer iter.Close()
	for {
		c, err := iter.Next()
		if err != nil {
			break
		}
		if len(sha) >= 6 && c.Hash.String()[:len(sha)] == sha {
			return c.Hash, nil
		}
	}
	return plumbing.ZeroHash, fmt.Errorf("commit %q not found", sha)
}

// Diff returns a unified diff of working-tree changes vs HEAD.
// Returns empty string if nothing is modified or no commits exist.
func (repo *Repo) Diff() (string, error) {
	if !repo.HasCommits() {
		return "", nil
	}
	ref, err := repo.r.Head()
	if err != nil {
		return "", err
	}
	commit, err := repo.r.CommitObject(ref.Hash())
	if err != nil {
		return "", err
	}
	headTree, err := commit.Tree()
	if err != nil {
		return "", err
	}
	wt, err := repo.r.Worktree()
	if err != nil {
		return "", err
	}
	status, err := wt.Status()
	if err != nil {
		return "", err
	}
	if status.IsClean() {
		return "", nil
	}
	// Build a simple unified diff by comparing file-by-file.
	var result string
	for path := range status {
		old, _ := fileContentFromTree(headTree, path)
		newBytes, _ := os.ReadFile(repo.path + "/" + path)
		if string(newBytes) != old {
			result += fmt.Sprintf("--- a/%s\n+++ b/%s\n", path, path)
			result += unifiedDiff(old, string(newBytes))
		}
	}
	return result, nil
}

func fileContentFromTree(tree *object.Tree, path string) (string, error) {
	f, err := tree.File(path)
	if err != nil {
		return "", err
	}
	return f.Contents()
}

const diffContextLines = 3

// unifiedDiff produces a real unified diff with @@ hunk headers.
//
// The previous implementation compared line i of old against line i of new,
// which meant inserting a single line near the top marked every following line
// as changed — and it emitted no @@ headers at all, so consumers that parse
// hunks (the UI diff viewer) saw an empty diff.
func unifiedDiff(old, newContent string) string {
	oldLines := splitLines(old)
	newLines := splitLines(newContent)
	ops := diffOps(oldLines, newLines)

	var out strings.Builder
	for _, h := range buildHunks(ops, oldLines, newLines) {
		out.WriteString(h)
	}
	return out.String()
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

type diffOp struct {
	kind   byte // ' ' equal, '-' delete, '+' insert
	oldIdx int  // index into oldLines (-1 for inserts)
	newIdx int  // index into newLines (-1 for deletes)
}

// diffOps computes a line-level diff using go-diff's Myers implementation. Lines
// are mapped to runes so the character-oriented algorithm operates per line.
func diffOps(oldLines, newLines []string) []diffOp {
	dmp := diffmatchpatch.New()
	// Every line must be newline-terminated: DiffLinesToRunes tokenises on "\n"
	// and would otherwise treat a final "b" and "b\n" as two different lines,
	// reporting a spurious delete+insert when text is merely appended.
	oldEnc, newEnc, _ := dmp.DiffLinesToRunes(joinLines(oldLines), joinLines(newLines))
	diffs := dmp.DiffMainRunes(oldEnc, newEnc, false)

	var ops []diffOp
	oi, ni := 0, 0
	for _, d := range diffs {
		n := len([]rune(d.Text))
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			for i := 0; i < n; i++ {
				ops = append(ops, diffOp{' ', oi, ni})
				oi++
				ni++
			}
		case diffmatchpatch.DiffDelete:
			for i := 0; i < n; i++ {
				ops = append(ops, diffOp{'-', oi, -1})
				oi++
			}
		case diffmatchpatch.DiffInsert:
			for i := 0; i < n; i++ {
				ops = append(ops, diffOp{'+', -1, ni})
				ni++
			}
		}
	}
	return ops
}

// buildHunks groups changed ops into @@ hunks with surrounding context.
func buildHunks(ops []diffOp, oldLines, newLines []string) []string {
	var hunks []string
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// Extend backwards for leading context.
		start := i
		for start > 0 && ops[start-1].kind == ' ' && i-start < diffContextLines {
			start--
		}
		// Walk forward to the end of this change cluster, allowing up to
		// 2*context equal lines before starting a new hunk.
		end := i
		gap := 0
		for end < len(ops) {
			if ops[end].kind == ' ' {
				gap++
				if gap > diffContextLines*2 {
					break
				}
			} else {
				gap = 0
			}
			end++
		}
		// The forward walk tolerates up to 2*context equal lines before ending a
		// hunk, so trim whatever exceeds the intended trailing context.
		trailing := 0
		for end-trailing > i && ops[end-trailing-1].kind == ' ' {
			trailing++
		}
		if trailing > diffContextLines {
			end -= trailing - diffContextLines
		}

		hunks = append(hunks, renderHunk(ops[start:end], oldLines, newLines))
		i = end
	}
	return hunks
}

func renderHunk(ops []diffOp, oldLines, newLines []string) string {
	oldStart, newStart := 0, 0
	var oldCount, newCount int
	foundOld, foundNew := false, false
	for _, o := range ops {
		if o.oldIdx >= 0 {
			if !foundOld {
				oldStart = o.oldIdx
				foundOld = true
			}
			oldCount++
		}
		if o.newIdx >= 0 {
			if !foundNew {
				newStart = o.newIdx
				foundNew = true
			}
			newCount++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldStart+1, oldCount, newStart+1, newCount)
	for _, o := range ops {
		switch o.kind {
		case ' ':
			b.WriteString(" " + oldLines[o.oldIdx] + "\n")
		case '-':
			b.WriteString("-" + oldLines[o.oldIdx] + "\n")
		case '+':
			b.WriteString("+" + newLines[o.newIdx] + "\n")
		}
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	cur := ""
	for _, ch := range s {
		if ch == '\n' {
			lines = append(lines, cur)
			cur = ""
		} else {
			cur += string(ch)
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
