package gh

import (
	"context"
	"fmt"
	"os"

	"github.com/cli/cli/v2/git"
	"github.com/golonzovsky/github-multirepo/internal/logger"
	"github.com/google/go-github/v45/github"
	"github.com/muesli/termenv"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
)

func InitClient(ctx context.Context, githubAccessToken string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubAccessToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

type GithubClient struct {
	client *github.Client
	ctx    context.Context
	org    string
	c      *git.Client
}

func NewGithubClient(ctx context.Context, githubAccessToken string, githubOrg string) *GithubClient {
	return &GithubClient{
		client: InitClient(ctx, githubAccessToken),
		ctx:    ctx,
		org:    githubOrg,
		c: &git.Client{
			Stderr: os.Stderr,
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
		},
	}
}

func (gc GithubClient) GetAllOrgRepos() (int, <-chan *github.Repository, error) {
	org, _, err := gc.client.Organizations.Get(gc.ctx, gc.org)
	if err != nil {
		return 0, nil, err
	}

	const perPage = 50
	totalRepos := *org.OwnedPrivateRepos + *org.PublicRepos
	numPages := (totalRepos + perPage - 1) / perPage
	repos := make(chan *github.Repository)
	g, _ := errgroup.WithContext(gc.ctx)
	for p := 1; p <= numPages; p++ {
		page := p
		g.Go(func() error {
			opt := &github.RepositoryListByOrgOptions{
				ListOptions: github.ListOptions{Page: page, PerPage: perPage},
				Type:        "all",
			}
			repoPage, _, err := gc.client.Repositories.ListByOrg(gc.ctx, gc.org, opt)
			if err != nil {
				return err
			}
			for _, repo := range repoPage {
				if !*repo.Archived {
					repos <- repo
				}
			}
			return nil
		})
	}
	go func() {
		err := g.Wait()
		if err != nil && err != context.Canceled {
			fmt.Fprintln(os.Stderr, termenv.String("error fetching org repos: ").Foreground(logger.Red).Bold(), err)
		}
		close(repos)
	}()
	return totalRepos, repos, nil
}

func (gc GithubClient) Clone(url string, targetLocation string) error {
	_, err := gc.c.Clone(gc.ctx, url, []string{targetLocation})
	return err
}
