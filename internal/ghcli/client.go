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
	"github.com/google/go-github/v45/github"
	"golang.org/x/sync/errgroup"
)

type Client struct {
	gc *git.Client
}

func NewGithubCliClient() *Client {
	return &Client{
		gc: &git.Client{
			Stderr: os.Stderr,
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
		},
	}
}

func GetGhToken() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	cmd.Env = os.Environ()
	ghToken, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("please login with gh cli: %v", err)
	}
	return strings.TrimSuffix(string(ghToken), "\n"), nil
}

func (c Client) PullRepo(ctx context.Context, repoName string, branch string, url string, targetFolder string) error {
	log.Info(fmt.Sprintf("Pulling %35s in %s", repoName, targetFolder))
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	if err := c.gc.Pull(ctx, url, branch,
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

func (c Client) Clone(ctx context.Context, url string, targetLocation string) error {
	_, err := c.gc.Clone(ctx, url, []string{targetLocation})
	return err
}

func (c Client) CloneAllOrgRepos(ctx context.Context, repos <-chan *github.Repository, targetDir string, parallelWorkers int) error {
	g, _ := errgroup.WithContext(ctx)
	for i := 0; i < parallelWorkers; i++ {
		g.Go(func() error {
			for repo := range repos {
				targetLoc := targetDir + "/" + *repo.Name
				log.Info("Cloning", "repo", *repo.Name, "to", targetLoc)

				cmd, err := c.gc.AuthenticatedCommand(ctx, "clone", *repo.CloneURL, targetLoc)
				if err != nil {
					return err
				}
				stdErr := &bytes.Buffer{}
				cmd.Stderr = stdErr
				if err := cmd.Run(); err != nil {
					if strings.Contains(stdErr.String(), "already exists and is not an empty directory") {
						log.Debug("Repo already exists, skipping", "repo", *repo.Name)
						continue
					}
					return err
				}
			}
			return nil
		})
	}
	return g.Wait()
}

func (c Client) PullAllOrgRepos(ctx context.Context, repos <-chan *github.Repository, targetDir string, parallelWorkers int) error {
	g, _ := errgroup.WithContext(ctx)
	for i := 0; i < parallelWorkers; i++ {
		g.Go(func() error {
			for repo := range repos {
				if repo.Archived != nil && *repo.Archived {
					log.Debug("Repo is archived, skipping", "repo", *repo.Name)
					continue
				}
				err := c.PullRepo(ctx, *repo.Name, *repo.DefaultBranch, *repo.CloneURL, targetDir+"/"+*repo.Name)
				if err != nil {
					return err
				}
			}
			return nil
		})
	}
	return g.Wait()
}
