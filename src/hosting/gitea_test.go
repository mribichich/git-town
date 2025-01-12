package hosting_test

import (
	"net/http"
	"testing"

	"github.com/git-town/git-town/v7/src/giturl"
	"github.com/git-town/git-town/v7/src/hosting"
	"github.com/stretchr/testify/assert"
	httpmock "gopkg.in/jarcoal/httpmock.v1"
)

const (
	giteaRoot     = "https://gitea.com/api/v1"
	giteaVersion  = giteaRoot + "/version"
	giteaCurrOpen = giteaRoot + "/repos/git-town/git-town/pulls?limit=50&page=0&state=open"
	giteaPR1      = giteaRoot + "/repos/git-town/git-town/pulls/1"
	giteaPR1Merge = giteaRoot + "/repos/git-town/git-town/pulls/1/merge"
)

func log(template string, messages ...interface{}) {}

func setupGiteaDriver(t *testing.T, token string) (*hosting.GiteaDriver, func()) {
	t.Helper()
	httpmock.Activate()
	config := mockConfig{
		originURL:  "git@gitea.com:git-town/git-town.git",
		giteaToken: token,
	}
	url := giturl.Parse(config.originURL)
	driver := hosting.NewGiteaDriver(*url, config, log)
	assert.NotNil(t, driver)
	return driver, func() {
		httpmock.DeactivateAndReset()
	}
}

func TestNewGiteaDriver(t *testing.T) {
	t.Parallel()
	t.Run("normal repo", func(t *testing.T) {
		t.Parallel()
		config := mockConfig{
			hostingService: "gitea",
			originURL:      "git@self-hosted-gitea.com:git-town/git-town.git",
		}
		url := giturl.Parse(config.originURL)
		driver := hosting.NewGiteaDriver(*url, config, log)
		assert.NotNil(t, driver)
		assert.Equal(t, "Gitea", driver.HostingServiceName())
		assert.Equal(t, "https://self-hosted-gitea.com/git-town/git-town", driver.RepositoryURL())
	})

	t.Run("custom hostname", func(t *testing.T) {
		t.Parallel()
		config := mockConfig{
			originURL:      "git@my-ssh-identity.com:git-town/git-town.git",
			originOverride: "gitea.com",
		}
		url := giturl.Parse(config.originURL)
		driver := hosting.NewGiteaDriver(*url, config, log)
		assert.NotNil(t, driver)
		assert.Equal(t, "Gitea", driver.HostingServiceName())
		assert.Equal(t, "https://gitea.com/git-town/git-town", driver.RepositoryURL())
	})
}

//nolint:paralleltest  // mocks HTTP
func TestGitea(t *testing.T) {
	//nolint:dupl
	t.Run(".LoadPullRequestInfo()", func(t *testing.T) {
		t.Run("happy path", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "TOKEN")
			defer teardown()
			httpmock.RegisterResponder("GET", giteaCurrOpen, httpmock.NewStringResponder(200, `[{"number": 1, "title": "my title", "mergeable": true, "base": {"label": "main"}, "head": {"label": "git-town/feature"} }]`))
			prInfo, err := driver.LoadPullRequestInfo("feature", "main")
			assert.NoError(t, err)
			assert.True(t, prInfo.CanMergeWithAPI)
			assert.Equal(t, "my title (#1)", prInfo.DefaultCommitMessage)
			assert.Equal(t, int64(1), prInfo.PullRequestNumber)
		})

		t.Run("empty Git token", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "")
			defer teardown()
			prInfo, err := driver.LoadPullRequestInfo("feature", "main")
			assert.NoError(t, err)
			assert.False(t, prInfo.CanMergeWithAPI)
		})

		t.Run("cannot load pull request number", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "TOKEN")
			defer teardown()
			httpmock.RegisterResponder("GET", giteaCurrOpen, httpmock.NewStringResponder(404, ""))
			_, err := driver.LoadPullRequestInfo("feature", "main")
			assert.Error(t, err)
		})

		t.Run("branch has no pull request", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "TOKEN")
			defer teardown()
			httpmock.RegisterResponder("GET", giteaCurrOpen, httpmock.NewStringResponder(200, "[]"))
			prInfo, err := driver.LoadPullRequestInfo("feature", "main")
			assert.NoError(t, err)
			assert.False(t, prInfo.CanMergeWithAPI)
		})

		t.Run("multiple pull requests for this banch", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "TOKEN")
			defer teardown()
			httpmock.RegisterResponder("GET", giteaCurrOpen, httpmock.NewStringResponder(200, `[{"number": 1, "base": {"label": "main"}, "head": {"label": "no-match"} }, {"number": 2, "base": {"label": "main"}, "head": {"label": "no-match2"} }]`))
			prInfo, err := driver.LoadPullRequestInfo("feature", "main")
			assert.NoError(t, err)
			assert.False(t, prInfo.CanMergeWithAPI)
		})
	})

	t.Run(".MergePullRequest()", func(t *testing.T) {
		t.Run("happy path", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "TOKEN")
			defer teardown()
			options := hosting.MergePullRequestOptions{
				Branch:            "feature",
				PullRequestNumber: 1,
				CommitMessage:     "title\nextra detail1\nextra detail2",
				ParentBranch:      "main",
			}
			var mergeRequest *http.Request
			httpmock.RegisterResponder("GET", giteaCurrOpen, httpmock.NewStringResponder(200, `[{"number": 1, "base": {"label": "main"}, "head": {"label": "git-town/feature"} }]`))
			httpmock.RegisterResponder("GET", giteaVersion, httpmock.NewStringResponder(200, `{"version": "1.11.5"}`))
			httpmock.RegisterResponder("POST", giteaPR1Merge, func(req *http.Request) (*http.Response, error) {
				mergeRequest = req
				return httpmock.NewStringResponse(200, `[]`), nil
			})
			httpmock.RegisterResponder("GET", giteaPR1, httpmock.NewStringResponder(200, `{"number": 1, "merge_commit_sha": "abc123"}`))
			sha, err := driver.MergePullRequest(options)
			assert.NoError(t, err)
			assert.Equal(t, "abc123", sha)
			mergeParameters := loadRequestData(mergeRequest)
			assert.Equal(t, "title", mergeParameters["MergeTitleField"])
			assert.Equal(t, "extra detail1\nextra detail2", mergeParameters["MergeMessageField"])
			assert.Equal(t, "squash", mergeParameters["Do"])
		})

		t.Run("cannot load pull request id", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "TOKEN")
			defer teardown()
			options := hosting.MergePullRequestOptions{
				Branch:        "feature",
				CommitMessage: "title\nextra detail1\nextra detail2",
				ParentBranch:  "main",
			}
			httpmock.RegisterResponder("GET", giteaCurrOpen, httpmock.NewStringResponder(404, ""))
			_, err := driver.MergePullRequest(options)
			assert.Error(t, err)
		})

		t.Run("cannot load pull request to merge", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "TOKEN")
			defer teardown()
			options := hosting.MergePullRequestOptions{
				Branch:            "feature",
				PullRequestNumber: 1,
				CommitMessage:     "title\nextra detail1\nextra detail2",
				ParentBranch:      "main",
			}
			httpmock.RegisterResponder("GET", giteaCurrOpen, httpmock.NewStringResponder(200, "[]"))
			httpmock.RegisterResponder("GET", giteaPR1Merge, httpmock.NewStringResponder(404, ""))
			_, err := driver.MergePullRequest(options)
			assert.Error(t, err)
		})

		t.Run("pull request not found", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "TOKEN")
			defer teardown()
			options := hosting.MergePullRequestOptions{
				Branch:            "feature",
				PullRequestNumber: 1,
				CommitMessage:     "title\nextra detail1\nextra detail2",
				ParentBranch:      "main",
			}
			httpmock.RegisterResponder("GET", giteaCurrOpen, httpmock.NewStringResponder(200, "[]"))
			httpmock.RegisterResponder("POST", giteaPR1Merge, func(req *http.Request) (*http.Response, error) {
				return httpmock.NewStringResponse(409, `{}`), nil
			})
			_, err := driver.MergePullRequest(options)
			assert.Error(t, err)
		})

		t.Run("merge fails", func(t *testing.T) {
			driver, teardown := setupGiteaDriver(t, "TOKEN")
			defer teardown()
			options := hosting.MergePullRequestOptions{
				Branch:        "feature",
				CommitMessage: "title\nextra detail1\nextra detail2",
				ParentBranch:  "main",
			}
			httpmock.RegisterResponder("GET", giteaCurrOpen, httpmock.NewStringResponder(200, `[{"number": 1, "base": {"label": "main"}, "head": {"label": "foo"} }]`))
			httpmock.RegisterResponder("GET", giteaVersion, httpmock.NewStringResponder(200, `{"version": "1.11.5"}`))
			httpmock.RegisterResponder("POST", giteaPR1Merge, httpmock.NewStringResponder(404, ""))
			_, err := driver.MergePullRequest(options)
			assert.Error(t, err)
		})
	})
}
