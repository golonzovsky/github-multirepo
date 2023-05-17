package ghcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/cli/cli/v2/git"
)

func GetGhToken() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	cmd.Env = os.Environ()
	ghToken, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("please login with gh cli: %v", err)
	}
	return strings.TrimSuffix(string(ghToken), "\n"), nil
}

func PullRepo(ctx context.Context, client *git.Client, repoName string, branch string, url string, targetFolder string) error {
	log.Info(fmt.Sprintf("Pulling %35s in %s", repoName, targetFolder))
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	if err := client.Pull(ctx, url, branch,
		git.WithRepoDir(targetFolder), git.WithStderr(stderr), git.WithStdout(stdout), withForceGitColors()); err != nil {
		errorMsg := stderr.String()
		if strings.Contains(errorMsg, "couldn't find remote ref") {
			log.Warn("No default branch found, skipping", "repo", repoName)
			return nil
		}
		return fmt.Errorf("failed to pull %s: %w, with message: %s", repoName, err, errorMsg)
	}
	out := stdout.String()
	if !strings.HasPrefix(out, "Already up to date.") {
		fmt.Print(out)
	}
	return nil
}

func withForceGitColors() git.CommandModifier {
	return func(gc *git.Command) {
		// add -c after git executable (first index)
		gc.Args = append([]string{gc.Args[0], "-c", "color.ui=always"}, gc.Args[1:]...)
	}
}
