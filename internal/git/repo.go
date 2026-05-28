package git

import (
	"errors"
	"fmt"
	"os"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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

// unifiedDiff produces a very simple line-by-line diff (no context lines).
func unifiedDiff(old, newContent string) string {
	oldLines := splitLines(old)
	newLines := splitLines(newContent)
	var out string
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}
	for i := range maxLen {
		o := ""
		n := ""
		if i < len(oldLines) {
			o = oldLines[i]
		}
		if i < len(newLines) {
			n = newLines[i]
		}
		if o != n {
			if o != "" {
				out += "-" + o + "\n"
			}
			if n != "" {
				out += "+" + n + "\n"
			}
		}
	}
	return out
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
