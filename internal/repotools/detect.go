package repotools

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// VCS represents a version control system type
type VCS string

const (
	VCSGit VCS = "git"
	VCSSvn VCS = "svn" // future: LinkedIn still uses SVN as of 2025
)

// IsInstalled checks if the specified VCS tool is available in PATH
func IsInstalled(vcs VCS) bool {
	_, err := exec.LookPath(string(vcs))
	return err == nil
}

// RequireVCS checks if a VCS is installed and returns an error if not
// Use this for fail-fast behavior in commands that require VCS
func RequireVCS(vcs VCS) error {
	if !IsInstalled(vcs) {
		return fmt.Errorf("%s is not installed or not in PATH.\nPlease install %s and try again", vcs, vcs)
	}
	return nil
}

// DetectVCS determines which VCS is being used in the current directory
// Returns the detected VCS type or an error if none is found
func DetectVCS() (VCS, error) {
	// try git first (most common)
	if IsInstalled(VCSGit) {
		cmd := exec.Command("git", "rev-parse", "--git-dir")
		if err := cmd.Run(); err == nil {
			return VCSGit, nil
		}
	}

	// try svn
	if IsInstalled(VCSSvn) {
		cmd := exec.Command("svn", "info")
		if err := cmd.Run(); err == nil {
			return VCSSvn, nil
		}
	}

	return "", fmt.Errorf("no supported VCS detected (checked: git, svn)")
}

// FindRepoRoot finds the root directory of the repository for the given VCS
func FindRepoRoot(vcs VCS) (string, error) {
	switch vcs {
	case VCSGit:
		return findGitRoot()
	case VCSSvn:
		return findSvnRoot()
	default:
		return "", fmt.Errorf("unsupported VCS: %s", vcs)
	}
}

// FindMainRepoRoot finds the main repository root, resolving through worktrees.
// Unlike FindRepoRoot which returns the worktree directory, this always returns
// the main repository root that all worktrees share.
//
// Why this matters: git worktrees are separate working directories that share
// the same repository. For features like the ledger where we want ONE instance
// per repository (not per worktree), we need to resolve to the main repo root.
// Using --show-toplevel would give each worktree its own ledger, fragmenting data.
func FindMainRepoRoot(vcs VCS) (string, error) {
	switch vcs {
	case VCSGit:
		return findMainGitRoot()
	case VCSSvn:
		// SVN doesn't have worktrees, same as FindRepoRoot
		return findSvnRoot()
	default:
		return "", fmt.Errorf("unsupported VCS: %s", vcs)
	}
}

func findMainGitRoot() (string, error) {
	// --git-common-dir returns the shared git directory across all worktrees
	// --path-format=absolute ensures we get an absolute path (requires git 2.31+, March 2021)
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find git common dir: %w", err)
	}
	gitCommonDir := strings.TrimSpace(string(output))

	// parent of .git is the main repo root
	return filepath.Dir(filepath.Clean(gitCommonDir)), nil
}

func findGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find git root: %w", err)
	}
	// git prints forward slashes even on Windows; Clean gives the platform
	// form so a repo root has exactly one spelling wherever it is keyed on.
	return filepath.Clean(strings.TrimSpace(string(output))), nil
}

func findSvnRoot() (string, error) {
	// svn info --show-item wc-root gets the working copy root
	cmd := exec.Command("svn", "info", "--show-item", "wc-root")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find svn root: %w", err)
	}
	return filepath.Clean(strings.TrimSpace(string(output))), nil
}
