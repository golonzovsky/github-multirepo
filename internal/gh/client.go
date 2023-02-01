package gh

import (
	"context"
	"fmt"
	"os"
	"strconv"

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
}

func NewGithubClient(ctx context.Context, githubAccessToken string, githubOrg string) *GithubClient {
	return &GithubClient{
		client: InitClient(ctx, githubAccessToken),
		ctx:    ctx,
		org:    githubOrg,
	}
}

func (gc GithubClient) CreateBranch(headBranch string, repo string, name string) (*github.Reference, error) {
	if ref, _, err := gc.client.Git.GetRef(gc.ctx, gc.org, repo, "refs/heads/"+name); err == nil {
		fmt.Fprintln(os.Stdout, termenv.String("branch "+name+" already exists").Foreground(logger.Yellow).Bold())
		return ref, nil
	}

	baseRef, _, err := gc.client.Git.GetRef(gc.ctx, gc.org, repo, "refs/heads/"+headBranch)
	if err != nil {
		return nil, err
	}
	newBranchRef := &github.Reference{
		Ref:    github.String("refs/heads/" + name),
		Object: &github.GitObject{SHA: baseRef.Object.SHA},
	}
	ref, _, err := gc.client.Git.CreateRef(gc.ctx, gc.org, repo, newBranchRef)
	if err != nil {
		return nil, err
	}

	refPushed, _, err := gc.client.Git.UpdateRef(gc.ctx, gc.org, repo, ref, false)
	fmt.Fprintln(os.Stdout, termenv.String("branch "+name+" created").Foreground(logger.Green).Bold())

	return refPushed, err
}

func (gc GithubClient) GetRepo(repo string) (*github.Repository, error) {
	githubRepo, _, err := gc.client.Repositories.Get(gc.ctx, gc.org, repo)
	if err != nil {
		return nil, err
	}
	return githubRepo, nil
}

type CommitFile struct {
	Content []byte
	Message string
	Path    string
}

func (gc GithubClient) CommitFile(branchRef *github.Reference, repo string, targetBranch string, commit *CommitFile) error {
	contentOptions := &github.RepositoryContentFileOptions{
		Content: commit.Content,
		Message: github.String(commit.Message),
		Branch:  github.String(targetBranch),
	}

	file, _, resp, err := gc.client.Repositories.GetContents(gc.ctx, gc.org, repo, commit.Path, &github.RepositoryContentGetOptions{
		Ref: *branchRef.Object.SHA,
	})

	if resp != nil && resp.StatusCode == 404 {
		//file does not exist, create one
		_, resp, err := gc.client.Repositories.CreateFile(gc.ctx, gc.org, repo, commit.Path, contentOptions)
		if err != nil {
			fmt.Fprintln(os.Stderr, termenv.String("file creation failed").Foreground(logger.Red).Bold(), err)
			return err
		}
		fmt.Fprintln(os.Stdout, termenv.String("create file: "+strconv.Itoa(resp.StatusCode)).Foreground(logger.Green).Bold())
		return nil
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, termenv.String("file lookup failed").Foreground(logger.Red).Bold(), err)
		return err
	}

	oldContent, err := file.GetContent()
	if err != nil {
		fmt.Fprintln(os.Stderr, termenv.String("existing file content decode failed").Foreground(logger.Red).Bold(), err)
		return err
	}
	if oldContent == string(commit.Content) {
		fmt.Fprintln(os.Stderr, termenv.String("file '"+commit.Path+"' content is unchanged, skipping").Foreground(logger.Yellow).Bold())
		return nil
	}

	// file exists, update it
	contentOptions.SHA = file.SHA
	_, resp, err = gc.client.Repositories.UpdateFile(gc.ctx, gc.org, repo, commit.Path, contentOptions)
	fmt.Fprintln(os.Stdout, termenv.String("update file '"+commit.Path+"': "+strconv.Itoa(resp.StatusCode)).Foreground(logger.Green).Bold())
	return err
}

func (gc GithubClient) CreatePR(defaultBranch string, repo string, branch string, title string, body string) error {
	// make sure we are pointing to the right head - might have changed on commits after branch creation
	ref, _, err := gc.client.Git.GetRef(gc.ctx, gc.org, repo, "refs/heads/"+branch)
	if err != nil {
		return err
	}

	existing, _, err := gc.client.PullRequests.ListPullRequestsWithCommit(gc.ctx, gc.org, repo, *ref.Object.SHA,
		&github.PullRequestListOptions{
			Head:  *ref.Ref,
			Base:  defaultBranch,
			State: "open",
		})
	if err != nil {
		fmt.Fprintln(os.Stderr, termenv.String("PR lookup failed").Foreground(logger.Red).Bold(), err)
		return err
	}
	if len(existing) > 0 {
		fmt.Fprintln(os.Stdout, termenv.String("PR already exists: "+*existing[0].HTMLURL).Foreground(logger.Yellow).Bold())
		//todo should we update text/body here?
		return nil
	}

	pr, _, err := gc.client.PullRequests.Create(gc.ctx, gc.org, repo,
		&github.NewPullRequest{
			Title:               github.String(title),
			Head:                github.String(branch),
			Base:                github.String(defaultBranch),
			Body:                github.String(body),
			MaintainerCanModify: github.Bool(true),
		})

	fmt.Fprintln(os.Stdout, termenv.String("PR created: "+*pr.HTMLURL).Foreground(logger.Green).Bold())
	return err
}

func (gc GithubClient) GetAllOrgRepos() (int, <-chan string, error) {
	org, _, err := gc.client.Organizations.Get(gc.ctx, gc.org)
	if err != nil {
		return 0, nil, err
	}

	const perPage = 50
	totalRepos := *org.OwnedPrivateRepos + *org.PublicRepos
	numPages := (totalRepos + perPage - 1) / perPage
	repos := make(chan string)
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
					repos <- *repo.Name
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
