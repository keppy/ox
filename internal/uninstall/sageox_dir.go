package uninstall

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RemovalItem represents a file or directory that will be removed during uninstall
type SageoxFileItem struct {
	Path      string // absolute path to the item
	RelPath   string // path relative to git root
	Size      int64  // size in bytes (0 for directories)
	IsTracked bool   // whether the item is tracked by git
	IsDir     bool   // whether the item is a directory
}

// FindSageoxFiles finds all files in the .sageox/ directory
// Returns a list of RemovalItem entries for preview and processing
func FindSageoxFiles(repoRoot string) ([]SageoxFileItem, error) {
	sageoxDir := filepath.Join(repoRoot, ".sageox")

	// check if .sageox directory exists
	if _, err := os.Stat(sageoxDir); os.IsNotExist(err) {
		slog.Debug("sageox directory does not exist", "path", sageoxDir)
		return nil, nil
	}

	var items []SageoxFileItem

	// get list of tracked files in .sageox/
	trackedFiles, err := getTrackedFiles(repoRoot, ".sageox")
	if err != nil {
		slog.Warn("failed to get tracked files", "error", err)
		// continue anyway - we can still find files on disk
		trackedFiles = make(map[string]bool)
	}

	// walk the .sageox directory
	err = filepath.WalkDir(sageoxDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("error accessing path", "path", path, "error", err)
			return nil // continue walking
		}

		// skip the root .sageox directory itself
		if path == sageoxDir {
			return nil
		}

		// get relative path from git root, in git's slash form: git ls-files
		// reports forward slashes on every platform, so a platform-form
		// relPath would never match the tracked set on Windows — every tracked
		// file would be reported untracked in the uninstall preview.
		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			slog.Warn("failed to get relative path", "path", path, "error", err)
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		var size int64
		isDir := d.IsDir()

		// get size for files (not directories)
		if !isDir {
			if info, err := d.Info(); err == nil {
				size = info.Size()
			}
		}

		// check if tracked by git
		isTracked := trackedFiles[relPath]

		items = append(items, SageoxFileItem{
			Path:      path,
			RelPath:   relPath,
			Size:      size,
			IsTracked: isTracked,
			IsDir:     isDir,
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking .sageox directory: %w", err)
	}

	return items, nil
}

// getTrackedFiles returns a map of git-tracked files under the given path
// Keys are paths relative to repoRoot
func getTrackedFiles(repoRoot, path string) (map[string]bool, error) {
	// use git ls-files to find tracked files
	cmd := exec.Command("git", "ls-files", path)
	cmd.Dir = repoRoot

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	tracked := make(map[string]bool)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			tracked[line] = true
		}
	}

	return tracked, nil
}

// RemoveSageoxDir removes the .sageox directory from the repository
// Uses 'git rm -r' for tracked files and os.RemoveAll for untracked files
// If dryRun is true, only logs what would be done without making changes
func RemoveSageoxDir(repoRoot string, dryRun bool) error {
	sageoxDir := filepath.Join(repoRoot, ".sageox")

	// check if .sageox directory exists
	if _, err := os.Stat(sageoxDir); os.IsNotExist(err) {
		slog.Debug("sageox directory does not exist", "path", sageoxDir)
		return nil // not an error - gracefully handle missing directory
	}

	slog.Debug("removing sageox directory", "path", sageoxDir, "dry_run", dryRun)

	// find tracked files first
	trackedFiles, err := getTrackedFiles(repoRoot, ".sageox")
	if err != nil {
		slog.Debug("failed to get tracked files", "error", err)
		// continue - we can still remove untracked files
		trackedFiles = make(map[string]bool)
	}

	// remove tracked files using git rm
	if len(trackedFiles) > 0 {
		slog.Debug("removing tracked files", "count", len(trackedFiles))

		if dryRun {
			slog.Debug("would execute git rm", "path", ".sageox", "tracked_count", len(trackedFiles))
		} else {
			// use git rm -rf to remove the entire .sageox directory if any files are tracked
			cmd := exec.Command("git", "rm", "-rf", ".sageox")
			cmd.Dir = repoRoot

			output, err := cmd.CombinedOutput()
			if err != nil {
				// log the error but continue - we still want to remove untracked files
				slog.Debug("git rm failed, will try os.RemoveAll", "error", err, "output", string(output))
				// fall through to remove untracked files
			} else {
				slog.Debug("git rm completed", "output", string(output))
			}
		}
	}

	// remove any remaining untracked files/directories
	// this handles both the case where git rm failed and untracked files
	if dryRun {
		slog.Debug("would remove directory", "path", sageoxDir)
	} else {
		// check if directory still exists (git rm might have already removed it)
		if _, err := os.Stat(sageoxDir); err == nil {
			slog.Debug("removing untracked files", "path", sageoxDir)
			if err := os.RemoveAll(sageoxDir); err != nil {
				return fmt.Errorf("removing .sageox directory: %w", err)
			}
			slog.Debug("directory removed", "path", sageoxDir)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("checking directory status: %w", err)
		}

		// verify directory is actually gone
		if _, err := os.Stat(sageoxDir); err == nil {
			// directory still exists - try removing subdirectories explicitly
			slog.Warn("directory still exists after removal, retrying", "path", sageoxDir)
			entries, _ := os.ReadDir(sageoxDir)
			for _, entry := range entries {
				subPath := filepath.Join(sageoxDir, entry.Name())
				if err := os.RemoveAll(subPath); err != nil {
					slog.Warn("failed to remove subdirectory", "path", subPath, "error", err)
				}
			}
			// final attempt to remove parent
			if err := os.Remove(sageoxDir); err != nil {
				return fmt.Errorf("failed to remove .sageox directory after cleanup: %w", err)
			}
		}
	}

	return nil
}
