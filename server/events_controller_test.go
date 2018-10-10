// Copyright 2017 HootSuite Media Inc.
//
// Licensed under the Apache License, Version 2.0 (the License);
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an AS IS BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// Modified hereafter by contributors to runatlantis/atlantis.

package server_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lkysow/go-gitlab"
	. "github.com/petergtz/pegomock"
	"github.com/runatlantis/atlantis/server"
	"github.com/runatlantis/atlantis/server/events"
	emocks "github.com/runatlantis/atlantis/server/events/mocks"
	"github.com/runatlantis/atlantis/server/events/mocks/matchers"
	"github.com/runatlantis/atlantis/server/events/models"
	vcsmocks "github.com/runatlantis/atlantis/server/events/vcs/mocks"
	"github.com/runatlantis/atlantis/server/logging"
	"github.com/runatlantis/atlantis/server/mocks"
	. "github.com/runatlantis/atlantis/testing"
)

const githubHeader = "X-Github-Event"
const gitlabHeader = "X-Gitlab-Event"

var secret = []byte("secret")

func TestPost_NotGithubOrGitlab(t *testing.T) {
	t.Log("when the request is not for gitlab or github a 400 is returned")
	e, _, _, _, _, _, _, _ := setup(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	e.Post(w, req)
	responseContains(t, w, http.StatusBadRequest, "Ignoring request")
}

func TestPost_UnsupportedVCSGithub(t *testing.T) {
	t.Log("when the request is for an unsupported vcs a 400 is returned")
	e, _, _, _, _, _, _, _ := setup(t)
	e.SupportedVCSHosts = nil
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "value")
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusBadRequest, "Ignoring request since not configured to support GitHub")
}

func TestPost_UnsupportedVCSGitlab(t *testing.T) {
	t.Log("when the request is for an unsupported vcs a 400 is returned")
	e, _, _, _, _, _, _, _ := setup(t)
	e.SupportedVCSHosts = nil
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusBadRequest, "Ignoring request since not configured to support GitLab")
}

func TestPost_InvalidGithubSecret(t *testing.T) {
	t.Log("when the github payload can't be validated a 400 is returned")
	e, v, _, _, _, _, _, _ := setup(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "value")
	When(v.Validate(req, secret)).ThenReturn(nil, errors.New("err"))
	e.Post(w, req)
	responseContains(t, w, http.StatusBadRequest, "err")
}

func TestPost_InvalidGitlabSecret(t *testing.T) {
	t.Log("when the gitlab payload can't be validated a 400 is returned")
	e, _, gl, _, _, _, _, _ := setup(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	When(gl.ParseAndValidate(req, secret)).ThenReturn(nil, errors.New("err"))
	e.Post(w, req)
	responseContains(t, w, http.StatusBadRequest, "err")
}

func TestPost_UnsupportedGithubEvent(t *testing.T) {
	t.Log("when the event type is an unsupported github event we ignore it")
	e, v, _, _, _, _, _, _ := setup(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "value")
	When(v.Validate(req, nil)).ThenReturn([]byte(`{"not an event": ""}`), nil)
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Ignoring unsupported event")
}

func TestPost_UnsupportedGitlabEvent(t *testing.T) {
	t.Log("when the event type is an unsupported gitlab event we ignore it")
	e, _, gl, _, _, _, _, _ := setup(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	When(gl.ParseAndValidate(req, secret)).ThenReturn([]byte(`{"not an event": ""}`), nil)
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Ignoring unsupported event")
}

func TestPost_GithubCommentNotCreated(t *testing.T) {
	t.Log("when the event is a github comment but it's not a created event we ignore it")
	e, v, _, _, _, _, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "issue_comment")
	// comment action is deleted, not created
	event := `{"action": "deleted"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Ignoring comment event since action was not created")
}

func TestPost_GithubInvalidComment(t *testing.T) {
	t.Log("when the event is a github comment without all expected data we return a 400")
	e, v, _, p, _, _, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "issue_comment")
	event := `{"action": "created"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	When(p.ParseGithubIssueCommentEvent(matchers.AnyPtrToGithubIssueCommentEvent())).ThenReturn(models.Repo{}, models.User{}, 1, errors.New("err"))
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusBadRequest, "Failed parsing event")
}

func TestPost_GitlabCommentInvalidCommand(t *testing.T) {
	t.Log("when the event is a gitlab comment with an invalid command we ignore it")
	e, _, gl, _, _, _, _, cp := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	When(gl.ParseAndValidate(req, secret)).ThenReturn(gitlab.MergeCommentEvent{}, nil)
	When(cp.Parse("", models.Gitlab)).ThenReturn(events.CommentParseResult{Ignore: true})
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Ignoring non-command comment: \"\"")
}

func TestPost_GithubCommentInvalidCommand(t *testing.T) {
	t.Log("when the event is a github comment with an invalid command we ignore it")
	e, v, _, p, _, _, _, cp := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "issue_comment")
	event := `{"action": "created"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	When(p.ParseGithubIssueCommentEvent(matchers.AnyPtrToGithubIssueCommentEvent())).ThenReturn(models.Repo{}, models.User{}, 1, nil)
	When(cp.Parse("", models.Github)).ThenReturn(events.CommentParseResult{Ignore: true})
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Ignoring non-command comment: \"\"")
}

func TestPost_GitlabCommentNotWhitelisted(t *testing.T) {
	t.Log("when the event is a gitlab comment from a repo that isn't whitelisted we comment with an error")
	RegisterMockTestingT(t)
	vcsClient := vcsmocks.NewMockClientProxy()
	e := server.EventsController{
		Logger:                       logging.NewNoopLogger(),
		CommentParser:                &events.CommentParser{},
		GitlabRequestParserValidator: &server.DefaultGitlabRequestParserValidator{},
		Parser:                       &events.EventParser{},
		SupportedVCSHosts:            []models.VCSHostType{models.Gitlab},
		RepoWhitelistChecker:         &events.RepoWhitelistChecker{},
		VCSClient:                    vcsClient,
	}
	requestJSON, err := ioutil.ReadFile(filepath.Join("testfixtures", "gitlabMergeCommentEvent_notWhitelisted.json"))
	Ok(t, err)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(requestJSON))
	req.Header.Set(gitlabHeader, "Note Hook")
	w := httptest.NewRecorder()
	e.Post(w, req)

	Equals(t, http.StatusForbidden, w.Result().StatusCode)
	body, _ := ioutil.ReadAll(w.Result().Body)
	exp := "Repo not whitelisted"
	Assert(t, strings.Contains(string(body), exp), "exp %q to be contained in %q", exp, string(body))
	expRepo, _ := models.NewRepo(models.Gitlab, "gitlabhq/gitlab-test", "https://example.com/gitlabhq/gitlab-test.git", "", "")
	vcsClient.VerifyWasCalledOnce().CreateComment(expRepo, 1, "```\nError: This repo is not whitelisted for Atlantis.\n```")
}

func TestPost_GitlabCommentNotWhitelistedWithSilenceErrors(t *testing.T) {
	t.Log("when the event is a gitlab comment from a repo that isn't whitelisted and we are silencing errors, do no comment with an error")
	RegisterMockTestingT(t)
	vcsClient := vcsmocks.NewMockClientProxy()
	e := server.EventsController{
		Logger:                       logging.NewNoopLogger(),
		CommentParser:                &events.CommentParser{},
		GitlabRequestParserValidator: &server.DefaultGitlabRequestParserValidator{},
		Parser:                       &events.EventParser{},
		SupportedVCSHosts:            []models.VCSHostType{models.Gitlab},
		RepoWhitelistChecker:         &events.RepoWhitelistChecker{},
		VCSClient:                    vcsClient,
		SilenceWhitelistErrors:       true,
	}
	requestJSON, err := ioutil.ReadFile(filepath.Join("testfixtures", "gitlabMergeCommentEvent_notWhitelisted.json"))
	Ok(t, err)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(requestJSON))
	req.Header.Set(gitlabHeader, "Note Hook")
	w := httptest.NewRecorder()
	e.Post(w, req)

	Equals(t, http.StatusForbidden, w.Result().StatusCode)
	body, _ := ioutil.ReadAll(w.Result().Body)
	exp := "Repo not whitelisted"
	Assert(t, strings.Contains(string(body), exp), "exp %q to be contained in %q", exp, string(body))
	expRepo, _ := models.NewRepo(models.Gitlab, "gitlabhq/gitlab-test", "https://example.com/gitlabhq/gitlab-test.git", "", "")
	vcsClient.VerifyWasCalled(Never()).CreateComment(expRepo, 1, "```\nError: This repo is not whitelisted for Atlantis.\n```")
}

func TestPost_GithubCommentNotWhitelisted(t *testing.T) {
	t.Log("when the event is a github comment from a repo that isn't whitelisted we comment with an error")
	RegisterMockTestingT(t)
	vcsClient := vcsmocks.NewMockClientProxy()
	e := server.EventsController{
		Logger:                 logging.NewNoopLogger(),
		GithubRequestValidator: &server.DefaultGithubRequestValidator{},
		CommentParser:          &events.CommentParser{},
		Parser:                 &events.EventParser{},
		SupportedVCSHosts:      []models.VCSHostType{models.Github},
		RepoWhitelistChecker:   &events.RepoWhitelistChecker{},
		VCSClient:              vcsClient,
	}
	requestJSON, err := ioutil.ReadFile(filepath.Join("testfixtures", "githubIssueCommentEvent_notWhitelisted.json"))
	Ok(t, err)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(requestJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(githubHeader, "issue_comment")
	w := httptest.NewRecorder()
	e.Post(w, req)

	Equals(t, http.StatusForbidden, w.Result().StatusCode)
	body, _ := ioutil.ReadAll(w.Result().Body)
	exp := "Repo not whitelisted"
	Assert(t, strings.Contains(string(body), exp), "exp %q to be contained in %q", exp, string(body))
	expRepo, _ := models.NewRepo(models.Github, "baxterthehacker/public-repo", "https://github.com/baxterthehacker/public-repo.git", "", "")
	vcsClient.VerifyWasCalledOnce().CreateComment(expRepo, 2, "```\nError: This repo is not whitelisted for Atlantis.\n```")
}

func TestPost_GithubCommentNotWhitelistedWithSilenceErrors(t *testing.T) {
	t.Log("when the event is a github comment from a repo that isn't whitelisted and we are silencing errors, do no comment with an error")
	RegisterMockTestingT(t)
	vcsClient := vcsmocks.NewMockClientProxy()
	e := server.EventsController{
		Logger:                 logging.NewNoopLogger(),
		GithubRequestValidator: &server.DefaultGithubRequestValidator{},
		CommentParser:          &events.CommentParser{},
		Parser:                 &events.EventParser{},
		SupportedVCSHosts:      []models.VCSHostType{models.Github},
		RepoWhitelistChecker:   &events.RepoWhitelistChecker{},
		VCSClient:              vcsClient,
		SilenceWhitelistErrors: true,
	}
	requestJSON, err := ioutil.ReadFile(filepath.Join("testfixtures", "githubIssueCommentEvent_notWhitelisted.json"))
	Ok(t, err)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(requestJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(githubHeader, "issue_comment")
	w := httptest.NewRecorder()
	e.Post(w, req)

	Equals(t, http.StatusForbidden, w.Result().StatusCode)
	body, _ := ioutil.ReadAll(w.Result().Body)
	exp := "Repo not whitelisted"
	Assert(t, strings.Contains(string(body), exp), "exp %q to be contained in %q", exp, string(body))
	expRepo, _ := models.NewRepo(models.Github, "baxterthehacker/public-repo", "https://github.com/baxterthehacker/public-repo.git", "", "")
	vcsClient.VerifyWasCalled(Never()).CreateComment(expRepo, 2, "```\nError: This repo is not whitelisted for Atlantis.\n```")
}

func TestPost_GitlabCommentResponse(t *testing.T) {
	// When the event is a gitlab comment that warrants a comment response we comment back.
	e, _, gl, _, _, _, vcsClient, cp := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	When(gl.ParseAndValidate(req, secret)).ThenReturn(gitlab.MergeCommentEvent{}, nil)
	When(cp.Parse("", models.Gitlab)).ThenReturn(events.CommentParseResult{CommentResponse: "a comment"})
	w := httptest.NewRecorder()
	e.Post(w, req)
	vcsClient.VerifyWasCalledOnce().CreateComment(models.Repo{}, 0, "a comment")
	responseContains(t, w, http.StatusOK, "Commenting back on pull request")
}

func TestPost_GithubCommentResponse(t *testing.T) {
	t.Log("when the event is a github comment that warrants a comment response we comment back")
	e, v, _, p, _, _, vcsClient, cp := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "issue_comment")
	event := `{"action": "created"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	baseRepo := models.Repo{}
	user := models.User{}
	When(p.ParseGithubIssueCommentEvent(matchers.AnyPtrToGithubIssueCommentEvent())).ThenReturn(baseRepo, user, 1, nil)
	When(cp.Parse("", models.Github)).ThenReturn(events.CommentParseResult{CommentResponse: "a comment"})
	w := httptest.NewRecorder()

	e.Post(w, req)
	vcsClient.VerifyWasCalledOnce().CreateComment(baseRepo, 1, "a comment")
	responseContains(t, w, http.StatusOK, "Commenting back on pull request")
}

func TestPost_GitlabCommentSuccess(t *testing.T) {
	t.Log("when the event is a gitlab comment with a valid command we call the command handler")
	e, _, gl, _, cr, _, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	When(gl.ParseAndValidate(req, secret)).ThenReturn(gitlab.MergeCommentEvent{}, nil)
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Processing...")

	cr.VerifyWasCalledOnce().RunCommentCommand(models.Repo{}, &models.Repo{}, nil, models.User{}, 0, nil)
}

func TestPost_GithubCommentSuccess(t *testing.T) {
	t.Log("when the event is a github comment with a valid command we call the command handler")
	e, v, _, p, cr, _, _, cp := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "issue_comment")
	event := `{"action": "created"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	baseRepo := models.Repo{}
	user := models.User{}
	cmd := events.CommentCommand{}
	When(p.ParseGithubIssueCommentEvent(matchers.AnyPtrToGithubIssueCommentEvent())).ThenReturn(baseRepo, user, 1, nil)
	When(cp.Parse("", models.Github)).ThenReturn(events.CommentParseResult{Command: &cmd})
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Processing...")

	cr.VerifyWasCalledOnce().RunCommentCommand(baseRepo, nil, nil, user, 1, &cmd)
}

func TestPost_GithubPullRequestInvalid(t *testing.T) {
	t.Log("when the event is a github pull request with invalid data we return a 400")
	e, v, _, p, _, _, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "pull_request")

	event := `{"action": "closed"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	When(p.ParseGithubPullEvent(matchers.AnyPtrToGithubPullRequestEvent())).ThenReturn(models.PullRequest{}, models.OpenedPullEvent, models.Repo{}, models.Repo{}, models.User{}, errors.New("err"))
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusBadRequest, "Error parsing pull data: err")
}

func TestPost_GitlabMergeRequestInvalid(t *testing.T) {
	t.Log("when the event is a gitlab merge request with invalid data we return a 400")
	e, _, gl, p, _, _, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	When(gl.ParseAndValidate(req, secret)).ThenReturn(gitlabMergeEvent, nil)
	repo := models.Repo{}
	pullRequest := models.PullRequest{State: models.ClosedPullState}
	When(p.ParseGitlabMergeRequestEvent(gitlabMergeEvent)).ThenReturn(pullRequest, models.OpenedPullEvent, repo, repo, models.User{}, errors.New("err"))
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusBadRequest, "Error parsing webhook: err")
}

func TestPost_GithubPullRequestNotWhitelisted(t *testing.T) {
	t.Log("when the event is a github pull request to a non-whitelisted repo we return a 400")
	e, v, _, _, _, _, _, _ := setup(t)
	var err error
	e.RepoWhitelistChecker, err = events.NewRepoWhitelistChecker("github.com/nevermatch")
	Ok(t, err)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "pull_request")

	event := `{"action": "closed"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusForbidden, "Ignoring pull request event from non-whitelisted repo")
}

func TestPost_GitlabMergeRequestNotWhitelisted(t *testing.T) {
	t.Log("when the event is a gitlab merge request to a non-whitelisted repo we return a 400")
	e, _, gl, p, _, _, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")

	var err error
	e.RepoWhitelistChecker, err = events.NewRepoWhitelistChecker("github.com/nevermatch")
	Ok(t, err)
	When(gl.ParseAndValidate(req, secret)).ThenReturn(gitlabMergeEvent, nil)
	repo := models.Repo{}
	pullRequest := models.PullRequest{State: models.ClosedPullState}
	When(p.ParseGitlabMergeRequestEvent(gitlabMergeEvent)).ThenReturn(pullRequest, models.OpenedPullEvent, repo, repo, models.User{}, nil)

	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusForbidden, "Ignoring pull request event from non-whitelisted repo")
}

func TestPost_GithubPullRequestUnsupportedAction(t *testing.T) {
	t.Skip("relies too much on mocks, should use real event parser")
	e, v, _, _, _, _, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "pull_request")

	event := `{"action": "unsupported"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	w := httptest.NewRecorder()
	e.Parser = &events.EventParser{}
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Ignoring non-actionable pull request event")
}

func TestPost_GitlabMergeRequestUnsupportedAction(t *testing.T) {
	t.Skip("relies too much on mocks, should use real event parser")
	t.Log("when the event is a gitlab merge request to a non-whitelisted repo we return a 400")
	e, _, gl, p, _, _, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	gitlabMergeEvent.ObjectAttributes.Action = "unsupported"
	When(gl.ParseAndValidate(req, secret)).ThenReturn(gitlabMergeEvent, nil)
	repo := models.Repo{}
	pullRequest := models.PullRequest{State: models.ClosedPullState}
	When(p.ParseGitlabMergeRequestEvent(gitlabMergeEvent)).ThenReturn(pullRequest, repo, repo, models.User{}, nil)

	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Ignoring non-actionable pull request event")
}

func TestPost_GithubPullRequestClosedErrCleaningPull(t *testing.T) {
	t.Skip("relies too much on mocks, should use real event parser")
	t.Log("when the event is a closed pull request and we have an error calling CleanUpPull we return a 503")
	RegisterMockTestingT(t)
	e, v, _, p, _, c, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "pull_request")

	event := `{"action": "closed"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	repo := models.Repo{}
	pull := models.PullRequest{State: models.ClosedPullState}
	When(p.ParseGithubPullEvent(matchers.AnyPtrToGithubPullRequestEvent())).ThenReturn(pull, models.OpenedPullEvent, repo, repo, models.User{}, nil)
	When(c.CleanUpPull(repo, pull)).ThenReturn(errors.New("cleanup err"))
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusInternalServerError, "Error cleaning pull request: cleanup err")
}

func TestPost_GitlabMergeRequestClosedErrCleaningPull(t *testing.T) {
	t.Skip("relies too much on mocks, should use real event parser")
	t.Log("when the event is a closed gitlab merge request and an error occurs calling CleanUpPull we return a 500")
	e, _, gl, p, _, c, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	gitlabMergeEvent.ObjectAttributes.Action = "close"
	When(gl.ParseAndValidate(req, secret)).ThenReturn(gitlabMergeEvent, nil)
	repo := models.Repo{}
	pullRequest := models.PullRequest{State: models.ClosedPullState}
	When(p.ParseGitlabMergeRequestEvent(gitlabMergeEvent)).ThenReturn(pullRequest, models.OpenedPullEvent, repo, repo, models.User{}, nil)
	When(c.CleanUpPull(repo, pullRequest)).ThenReturn(errors.New("err"))
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusInternalServerError, "Error cleaning pull request: err")
}

func TestPost_GithubClosedPullRequestSuccess(t *testing.T) {
	t.Skip("relies too much on mocks, should use real event parser")
	t.Log("when the event is a pull request and everything works we return a 200")
	e, v, _, p, _, c, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(githubHeader, "pull_request")

	event := `{"action": "closed"}`
	When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
	repo := models.Repo{}
	pull := models.PullRequest{State: models.ClosedPullState}
	When(p.ParseGithubPullEvent(matchers.AnyPtrToGithubPullRequestEvent())).ThenReturn(pull, models.OpenedPullEvent, repo, repo, models.User{}, nil)
	When(c.CleanUpPull(repo, pull)).ThenReturn(nil)
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Pull request cleaned successfully")
}

func TestPost_GitlabMergeRequestSuccess(t *testing.T) {
	t.Skip("relies too much on mocks, should use real event parser")
	t.Log("when the event is a gitlab merge request and the cleanup works we return a 200")
	e, _, gl, p, _, _, _, _ := setup(t)
	req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
	req.Header.Set(gitlabHeader, "value")
	When(gl.ParseAndValidate(req, secret)).ThenReturn(gitlabMergeEvent, nil)
	repo := models.Repo{}
	pullRequest := models.PullRequest{State: models.ClosedPullState}
	When(p.ParseGitlabMergeRequestEvent(gitlabMergeEvent)).ThenReturn(pullRequest, models.OpenedPullEvent, repo, repo, models.User{}, nil)
	w := httptest.NewRecorder()
	e.Post(w, req)
	responseContains(t, w, http.StatusOK, "Pull request cleaned successfully")
}

func TestPost_PullOpenedOrUpdated(t *testing.T) {
	cases := []struct {
		Description string
		HostType    models.VCSHostType
		Action      string
	}{
		{
			"github opened",
			models.Github,
			"opened",
		},
		{
			"gitlab opened",
			models.Gitlab,
			"open",
		},
		{
			"github synchronized",
			models.Github,
			"synchronize",
		},
		{
			"gitlab update",
			models.Gitlab,
			"update",
		},
	}

	for _, c := range cases {
		t.Run(c.Description, func(t *testing.T) {
			e, v, gl, p, cr, _, _, _ := setup(t)
			req, _ := http.NewRequest("GET", "", bytes.NewBuffer(nil))
			switch c.HostType {
			case models.Gitlab:
				req.Header.Set(gitlabHeader, "value")
				gitlabMergeEvent.ObjectAttributes.Action = c.Action
				When(gl.ParseAndValidate(req, secret)).ThenReturn(gitlabMergeEvent, nil)
				repo := models.Repo{}
				pullRequest := models.PullRequest{State: models.ClosedPullState}
				When(p.ParseGitlabMergeRequestEvent(gitlabMergeEvent)).ThenReturn(pullRequest, models.OpenedPullEvent, repo, repo, models.User{}, nil)
			case models.Github:
				req.Header.Set(githubHeader, "pull_request")
				event := fmt.Sprintf(`{"action": "%s"}`, c.Action)
				When(v.Validate(req, secret)).ThenReturn([]byte(event), nil)
				repo := models.Repo{}
				pull := models.PullRequest{State: models.ClosedPullState}
				When(p.ParseGithubPullEvent(matchers.AnyPtrToGithubPullRequestEvent())).ThenReturn(pull, models.OpenedPullEvent, repo, repo, models.User{}, nil)
			}
			w := httptest.NewRecorder()
			e.Post(w, req)
			responseContains(t, w, http.StatusOK, "Processing...")
			cr.VerifyWasCalledOnce().RunAutoplanCommand(models.Repo{}, models.Repo{}, models.PullRequest{State: models.ClosedPullState}, models.User{})
		})
	}
}

func setup(t *testing.T) (server.EventsController, *mocks.MockGithubRequestValidator, *mocks.MockGitlabRequestParserValidator, *emocks.MockEventParsing, *emocks.MockCommandRunner, *emocks.MockPullCleaner, *vcsmocks.MockClientProxy, *emocks.MockCommentParsing) {
	RegisterMockTestingT(t)
	v := mocks.NewMockGithubRequestValidator()
	gl := mocks.NewMockGitlabRequestParserValidator()
	p := emocks.NewMockEventParsing()
	cp := emocks.NewMockCommentParsing()
	cr := emocks.NewMockCommandRunner()
	c := emocks.NewMockPullCleaner()
	vcsmock := vcsmocks.NewMockClientProxy()
	repoWhitelistChecker, err := events.NewRepoWhitelistChecker("*")
	Ok(t, err)
	e := server.EventsController{
		TestingMode:                  true,
		Logger:                       logging.NewNoopLogger(),
		GithubRequestValidator:       v,
		Parser:                       p,
		CommentParser:                cp,
		CommandRunner:                cr,
		PullCleaner:                  c,
		GithubWebhookSecret:          secret,
		SupportedVCSHosts:            []models.VCSHostType{models.Github, models.Gitlab},
		GitlabWebhookSecret:          secret,
		GitlabRequestParserValidator: gl,
		RepoWhitelistChecker:         repoWhitelistChecker,
		VCSClient:                    vcsmock,
	}
	return e, v, gl, p, cr, c, vcsmock, cp
}

var gitlabMergeEvent = gitlab.MergeEvent{
	ObjectAttributes: struct {
		ID              int              `json:"id"`
		TargetBranch    string           `json:"target_branch"`
		SourceBranch    string           `json:"source_branch"`
		SourceProjectID int              `json:"source_project_id"`
		AuthorID        int              `json:"author_id"`
		AssigneeID      int              `json:"assignee_id"`
		Title           string           `json:"title"`
		CreatedAt       string           `json:"created_at"`
		UpdatedAt       string           `json:"updated_at"`
		StCommits       []*gitlab.Commit `json:"st_commits"`
		StDiffs         []*gitlab.Diff   `json:"st_diffs"`
		MilestoneID     int              `json:"milestone_id"`
		State           string           `json:"state"`
		MergeStatus     string           `json:"merge_status"`
		TargetProjectID int              `json:"target_project_id"`
		IID             int              `json:"iid"`
		Description     string           `json:"description"`
		Position        int              `json:"position"`
		LockedAt        string           `json:"locked_at"`
		UpdatedByID     int              `json:"updated_by_id"`
		MergeError      string           `json:"merge_error"`
		MergeParams     struct {
			ForceRemoveSourceBranch string `json:"force_remove_source_branch"`
		} `json:"merge_params"`
		MergeWhenBuildSucceeds   bool               `json:"merge_when_build_succeeds"`
		MergeUserID              int                `json:"merge_user_id"`
		MergeCommitSha           string             `json:"merge_commit_sha"`
		DeletedAt                string             `json:"deleted_at"`
		ApprovalsBeforeMerge     string             `json:"approvals_before_merge"`
		RebaseCommitSha          string             `json:"rebase_commit_sha"`
		InProgressMergeCommitSha string             `json:"in_progress_merge_commit_sha"`
		LockVersion              int                `json:"lock_version"`
		TimeEstimate             int                `json:"time_estimate"`
		Source                   *gitlab.Repository `json:"source"`
		Target                   *gitlab.Repository `json:"target"`
		LastCommit               struct {
			ID        string         `json:"id"`
			Message   string         `json:"message"`
			Timestamp *time.Time     `json:"timestamp"`
			URL       string         `json:"url"`
			Author    *gitlab.Author `json:"author"`
		} `json:"last_commit"`
		WorkInProgress bool   `json:"work_in_progress"`
		URL            string `json:"url"`
		Action         string `json:"action"`
		Assignee       struct {
			Name      string `json:"name"`
			Username  string `json:"username"`
			AvatarURL string `json:"avatar_url"`
		} `json:"assignee"`
	}{
		Action: "merge",
	},
}
