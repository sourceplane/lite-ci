package git

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// ChangeDetector detects files that have changed in git
type ChangeDetector struct {
	baseBranch string // branch to compare against (e.g., "main", "develop")
}

// NewChangeDetector creates a new change detector
func NewChangeDetector(baseBranch string) *ChangeDetector {
	return &ChangeDetector{
		baseBranch: baseBranch,
	}
}

// GetChangedFiles returns files that have changed since the base branch or uncommitted changes
// When checking a specific branch (for MRs), combines all sources: uncommitted + branch changes
// Returns both modified and new files (both staged and unstaged and committed but not in base branch)
func (cd *ChangeDetector) GetChangedFiles() ([]string, error) {
	filesMap := make(map[string]bool)

	// Get unstaged modifications
	cmd := exec.Command("git", "diff", "--name-only")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, f := range files {
			if f != "" {
				filesMap[f] = true
			}
		}
	}

	// Also get staged changes
	cmd = exec.Command("git", "diff", "--cached", "--name-only")
	output, err = cmd.Output()
	if err == nil && len(output) > 0 {
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, f := range files {
			if f != "" {
				filesMap[f] = true
			}
		}
	}

	// ALWAYS check against base branch (for MR scenarios where commits are already pushed)
	// Try to use the specified base branch, but handle cases where it doesn't exist locally
	compareRef := cd.baseBranch
	if compareRef == "" {
		compareRef = "main"
	}

	// Try with the base branch first
	cmd = exec.Command("git", "diff", "--name-only", compareRef)
	output, err = cmd.Output()
	
	// If the branch reference fails, try with origin/baseBranch (common in CI)
	if err != nil || len(output) == 0 {
		cmd = exec.Command("git", "diff", "--name-only", "origin/"+compareRef)
		output, err = cmd.Output()
	}

	// If both fail, try merge-base as last resort (works in detached HEAD state)
	if err != nil || len(output) == 0 {
		// Try fork-point first (best for PR scenarios)
		cmd = exec.Command("git", "merge-base", "--fork-point", compareRef)
		mergeBaseOutput, mergeErr := cmd.Output()
		
		// If fork-point fails, try regular merge-base
		if mergeErr != nil {
			cmd = exec.Command("git", "merge-base", "HEAD", compareRef)
			mergeBaseOutput, mergeErr = cmd.Output()
		}
		
		// Try with origin/compareRef for merge-base too
		if mergeErr != nil {
			cmd = exec.Command("git", "merge-base", "HEAD", "origin/"+compareRef)
			mergeBaseOutput, mergeErr = cmd.Output()
		}
		
		if mergeErr == nil && len(mergeBaseOutput) > 0 {
			baseSha := strings.TrimSpace(string(mergeBaseOutput))
			cmd = exec.Command("git", "diff", "--name-only", baseSha)
			output, err = cmd.Output()
		}
	}

	if err == nil && len(output) > 0 {
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, f := range files {
			if f != "" {
				filesMap[f] = true
			}
		}
	}

	// Return combined set of all changes
	if len(filesMap) > 0 {
		var result []string
		for f := range filesMap {
			result = append(result, f)
		}
		return result, nil
	}

	return []string{}, nil
}

// IsPathChanged checks if any files under a given path have changed
func (cd *ChangeDetector) IsPathChanged(path string) (bool, error) {
	if path == "" || path == "./" {
		// Root path - check all changes
		files, err := cd.GetChangedFiles()
		return len(files) > 0, err
	}

	files, err := cd.GetChangedFiles()
	if err != nil {
		return false, err
	}

	// Normalize path (remove trailing slash)
	path = strings.TrimSuffix(path, "/")

	for _, file := range files {
		// Check if file is under this path
		if strings.HasPrefix(file, path+"/") || file == path {
			return true, nil
		}
	}

	return false, nil
}

// GetChangedFilesUnderPath returns files that have changed under a specific path
func (cd *ChangeDetector) GetChangedFilesUnderPath(path string) ([]string, error) {
	if path == "" || path == "./" {
		return cd.GetChangedFiles()
	}

	files, err := cd.GetChangedFiles()
	if err != nil {
		return []string{}, err
	}

	path = strings.TrimSuffix(path, "/")
	var result []string

	for _, file := range files {
		if strings.HasPrefix(file, path+"/") || file == path {
			result = append(result, file)
		}
	}

	return result, nil
}

// IsIntentFileChanged checks if the intent file itself has changed
func (cd *ChangeDetector) IsIntentFileChanged(intentFile string) (bool, error) {
	files, err := cd.GetChangedFiles()
	if err != nil {
		return false, err
	}

	// Normalize the intentFile path (handle both absolute and relative)
	var intentPaths []string
	intentPaths = append(intentPaths, intentFile)

	// Also check just the filename in case path differs
	intentPaths = append(intentPaths, filepath.Base(intentFile))

	for _, file := range files {
		for _, intentPath := range intentPaths {
			if file == intentPath || strings.HasSuffix(file, "/"+intentPath) || strings.HasSuffix(file, "\\"+intentPath) {
				return true, nil
			}
		}
	}
	return false, nil
}

// IsAnyPathChanged checks if any files under any of the given paths have changed
func (cd *ChangeDetector) IsAnyPathChanged(paths []string) (bool, error) {
	for _, path := range paths {
		if path == "" || path == "./" {
			continue
		}
		changed, err := cd.IsPathChanged(path)
		if err != nil {
			return false, err
		}
		if changed {
			return true, nil
		}
	}
	return false, nil
}
