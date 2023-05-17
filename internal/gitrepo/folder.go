package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

func GetFolderRepos(ctx context.Context, dir string) ([]string, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, dirEntry := range dirEntries {
		fullPath := filepath.Join(dir, dirEntry.Name())
		if dirEntry.IsDir() && isGitRepo(ctx, fullPath) { //todo add isOnBranch
			dirs = append(dirs, dirEntry.Name())
		}
	}
	return dirs, nil
}

func isGitRepo(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	err := cmd.Run()
	return err == nil
}
