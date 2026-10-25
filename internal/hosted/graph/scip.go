package graph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"px1/internal/hosted/indexer"
)

const SCIPCPULimit = 10 * time.Minute

type SCIPImage struct {
	Image   string
	Command []string
}

type SCIPRequest struct {
	RepositoryDir  string
	OutputFile     string
	Image          string
	Command        []string
	NetworkEnabled bool
	ReadOnlyRoot   bool
	CPULimit       time.Duration
}

type SCIPRunner interface {
	Run(context.Context, SCIPRequest) error
}

type SCIPIndexer struct {
	Runner SCIPRunner
	Images map[indexer.Language]SCIPImage
}

// Index runs exact cross-file indexing only for a repository's trusted default
// branch. The container runtime must enforce the request's isolation fields.
func (s SCIPIndexer) Index(ctx context.Context, language indexer.Language, repositoryDir, ref, defaultBranch, outputFile string) error {
	if s.Runner == nil {
		return errors.New("SCIP sandbox runner is required")
	}
	if repositoryDir == "" || outputFile == "" || ref == "" || defaultBranch == "" {
		return errors.New("repository, output, ref, and default branch are required")
	}
	if ref != defaultBranch {
		return errors.New("SCIP indexing is restricted to the default branch")
	}
	image, ok := s.Images[language]
	if !ok {
		return fmt.Errorf("no SCIP indexer configured for %s", language)
	}
	if !validDigestImage(image.Image) || len(image.Command) == 0 {
		return errors.New("SCIP indexer image must be digest-pinned and provide a command")
	}
	runCtx, cancel := context.WithTimeout(ctx, SCIPCPULimit)
	defer cancel()
	request := SCIPRequest{
		RepositoryDir:  repositoryDir,
		OutputFile:     outputFile,
		Image:          image.Image,
		Command:        append([]string(nil), image.Command...),
		NetworkEnabled: false,
		ReadOnlyRoot:   true,
		CPULimit:       SCIPCPULimit,
	}
	if err := s.Runner.Run(runCtx, request); err != nil {
		return fmt.Errorf("run %s SCIP indexer: %w", language, err)
	}
	return nil
}

func validDigestImage(image string) bool {
	marker := strings.LastIndex(image, "@sha256:")
	if marker <= 0 || len(image[marker+8:]) != 64 {
		return false
	}
	for _, r := range image[marker+8:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
