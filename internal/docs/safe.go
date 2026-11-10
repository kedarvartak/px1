package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The repository being documented can be a pull request's files, checked out
// as data by a workflow that publishes what px1 writes. A symbolic link in
// that tree could point at a file on the machine (a credential, say) and get
// it copied into a public site. So every read resolves links first and
// refuses a path that ends up outside the repository.

// inside returns an error when path, with links resolved, is not within root.
func inside(root, path string) error {
	realRoot, err := resolve(root)
	if err != nil {
		return err
	}
	real, err := resolve(path)
	if err != nil {
		return err
	}
	if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = filepath.Base(path)
		}
		return fmt.Errorf("%s is a link to a path outside the repository", filepath.ToSlash(rel))
	}
	return nil
}

// readRepo reads a file of the repository.
func readRepo(root, path string) ([]byte, error) {
	if err := inside(root, path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// readRepoDir lists a directory of the repository.
func readRepoDir(root, path string) ([]os.DirEntry, error) {
	if err := inside(root, path); err != nil {
		return nil, err
	}
	return os.ReadDir(path)
}

// resolve makes a path absolute and follows every link in it.
func resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}
