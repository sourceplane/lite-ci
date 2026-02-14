package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ChangeDetector detects files that have changed in git
type ChangeDetector struct {
	options ChangeOptions
}

// ChangeOptions defines Nx-style criteria for selecting changed files.
// Priority follows Nx semantics:
// 1) files
// 2) uncommitted
// 3) untracked
// 4) base + head
// 5) base only (committed + uncommitted + untracked)
// 6) default base (main) + current workspace changes
type ChangeOptions struct {
	Base        string
	Head        string
	Files       []string
	Uncommitted bool
	Untracked   bool
}

// NewChangeDetector creates a new change detector
func NewChangeDetector(baseBranch string) *ChangeDetector {
	return NewChangeDetectorWithOptions(ChangeOptions{Base: baseBranch})
}

// NewChangeDetectorWithOptions creates a new change detector with explicit options.
func NewChangeDetectorWithOptions(options ChangeOptions) *ChangeDetector {
	return &ChangeDetector{
		options: options,
	}
}

// GetChangedFiles returns files based on Nx-style affected resolution.
func (cd *ChangeDetector) GetChangedFiles() ([]string, error) {
	options := cd.options

	if len(options.Files) > 0 {
		return normalizeFiles(options.Files), nil
	}

	if options.Uncommitted {
		return normalizeFiles(getUncommittedFiles()), nil
	}

	if options.Untracked {
		return normalizeFiles(getUntrackedFiles()), nil
	}

	base := options.Base
	head := options.Head

	if base == "" {
		base = "main"
	}

	if base != "" && head != "" {
		return normalizeFiles(getFilesUsingBaseAndHead(base, head)), nil
	}

	if base != "" {
		files := append([]string{}, getFilesUsingBaseAndHead(base, "HEAD")...)
		files = append(files, getUncommittedFiles()...)
		files = append(files, getUntrackedFiles()...)
		return normalizeFiles(files), nil
	}

	return []string{}, nil
}

func getUncommittedFiles() []string {
	unstaged := parseGitOutput("diff", "--name-only", "--no-renames", "--relative", "HEAD", ".")
	staged := parseGitOutput("diff", "--cached", "--name-only", "--no-renames", "--relative")
	return append(unstaged, staged...)
}

func getUntrackedFiles() []string {
	return parseGitOutput("ls-files", "--others", "--exclude-standard")
}

func getMergeBase(base string, head string) string {
	mergeBase := strings.TrimSpace(runGitOutput("merge-base", base, head))
	if mergeBase != "" {
		return mergeBase
	}

	forkPoint := strings.TrimSpace(runGitOutput("merge-base", "--fork-point", base, head))
	if forkPoint != "" {
		return forkPoint
	}

	// Try origin/base as a fallback in CI where local branch is unavailable.
	if !strings.HasPrefix(base, "origin/") {
		originBase := "origin/" + base
		mergeBase = strings.TrimSpace(runGitOutput("merge-base", originBase, head))
		if mergeBase != "" {
			return mergeBase
		}
		forkPoint = strings.TrimSpace(runGitOutput("merge-base", "--fork-point", originBase, head))
		if forkPoint != "" {
			return forkPoint
		}
	}

	return base
}

func getFilesUsingBaseAndHead(base string, head string) []string {
	resolvedBase := getMergeBase(base, head)
	if resolvedBase == "" {
		resolvedBase = base
	}
	return parseGitOutput("diff", "--name-only", "--no-renames", "--relative", resolvedBase, head)
}

func parseGitOutput(args ...string) []string {
	output := runGitOutput(args...)
	if strings.TrimSpace(output) == "" {
		return []string{}
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}

	return result
}

func runGitOutput(args ...string) string {
	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}

func normalizeFiles(files []string) []string {
	set := make(map[string]struct{}, len(files))
	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		set[file] = struct{}{}
	}

	result := make([]string, 0, len(set))
	for file := range set {
		result = append(result, file)
	}
	sort.Strings(result)

	return result
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

// ValidateOptions validates affected-like option combinations.
func ValidateOptions(options ChangeOptions) error {
	if len(options.Files) > 0 {
		if options.Uncommitted || options.Untracked || options.Base != "" || options.Head != "" {
			return fmt.Errorf("--files conflicts with --uncommitted, --untracked, --base, and --head")
		}
		return nil
	}

	if options.Uncommitted && (options.Untracked || options.Base != "" || options.Head != "") {
		return fmt.Errorf("--uncommitted conflicts with --untracked, --base, and --head")
	}

	if options.Untracked && (options.Uncommitted || options.Base != "" || options.Head != "") {
		return fmt.Errorf("--untracked conflicts with --uncommitted, --base, and --head")
	}

	if options.Head != "" && options.Base == "" {
		return fmt.Errorf("--head requires --base")
	}

	return nil
}
