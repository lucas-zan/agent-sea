package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolvePathInWorkspace(workspaceRoot, userPath string) (string, error) {
	if strings.TrimSpace(userPath) == "" {
		userPath = "."
	}

	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace root symlinks: %w", err)
	}
	rootReal = filepath.Clean(rootReal)

	var targetAbs string
	if filepath.IsAbs(userPath) {
		targetAbs = filepath.Clean(userPath)
	} else {
		targetAbs = filepath.Clean(filepath.Join(rootAbs, userPath))
	}

	targetAbs, err = filepath.Abs(targetAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	targetAbs = filepath.Clean(targetAbs)

	if !pathWithinRoot(rootAbs, targetAbs) {
		return "", fmt.Errorf("path escapes workspace: %s", userPath)
	}

	if _, err := os.Lstat(targetAbs); err == nil {
		targetReal, err := filepath.EvalSymlinks(targetAbs)
		if err != nil {
			return "", fmt.Errorf("failed to resolve path symlinks: %w", err)
		}
		targetReal = filepath.Clean(targetReal)
		if !pathWithinRoot(rootReal, targetReal) {
			return "", fmt.Errorf("path escapes workspace via symlink: %s", userPath)
		}
		return targetReal, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to stat path: %w", err)
	}

	// The target does not exist. Resolve the nearest existing parent and ensure it doesn't escape.
	parent := filepath.Dir(targetAbs)
	for {
		if _, err := os.Lstat(parent); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to stat parent path: %w", err)
		}

		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}

	parentReal, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("failed to resolve parent symlinks: %w", err)
	}
	parentReal = filepath.Clean(parentReal)

	suffix, err := filepath.Rel(parent, targetAbs)
	if err != nil {
		return "", fmt.Errorf("failed to compute target suffix: %w", err)
	}
	if suffix == ".." || strings.HasPrefix(suffix, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %s", userPath)
	}

	targetReal := filepath.Clean(filepath.Join(parentReal, suffix))
	if !pathWithinRoot(rootReal, targetReal) {
		return "", fmt.Errorf("path escapes workspace via symlink: %s", userPath)
	}
	return targetReal, nil
}

func pathWithinRoot(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)

	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
