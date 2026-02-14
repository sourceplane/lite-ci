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
// Returns both modified and new files
func (cd *ChangeDetector) GetChangedFiles() ([]string, error) {
	// First, try to get changes from unstaged modifications
	cmd := exec.Command("git", "diff", "--name-only")
	output, err := cmd.Output()
	
	if err == nil && len(output) > 0 {
		// Parse unstaged files
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		var result []string
		for _, f := range files {
			if f != "" {
				result = append(result, f)
			}
		}
		return result, nil
	}

	// If no unstaged changes, try staged changes
	cmd = exec.Command("git", "diff", "--cached", "--name-only")
	output, err = cmd.Output()
	
	if err == nil && len(output) > 0 {
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		var result []string
		for _, f := range files {
			if f != "" {
				result = append(result, f)
			}
		}
		return result, nil
	}

	// Otherwise try against base branch
	compareRef := cd.baseBranch
	if compareRef == "" {
		compareRef = "main"
	}

	cmd = exec.Command("git", "diff", "--name-only", compareRef)
	output, err = cmd.Output()
	if err != nil {
		return []string{}, nil
	}

	if len(output) == 0 {
		return []string{}, nil
	}

	// Parse file list
	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	var result []string
	for _, f := range files {
		if f != "" {
			result = append(result, f)
		}
	}
	return result, nil
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
