package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"senioguard-documents/internal/model"
	"senioguard-documents/internal/module"
	"senioguard-documents/internal/repository"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type GitHubConnector struct {
	Token   string
	Repos   []string
	Storage module.Storage
	ReposDB *repository.Repositories
	Enqueue func(primitive.ObjectID)
	Client  *http.Client
}

func (c *GitHubConnector) Name() string {
	return "github"
}

func (c *GitHubConnector) Sync(ctx context.Context) (module.SourceSyncResult, error) {
	result := module.SourceSyncResult{Source: c.Name()}
	for _, repo := range c.Repos {
		if err := c.syncRepo(ctx, repo, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (c *GitHubConnector) SyncSelection(ctx context.Context, selection string) (module.SourceSyncResult, error) {
	result := module.SourceSyncResult{Source: c.Name()}
	if err := c.syncRepo(ctx, selection, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *GitHubConnector) syncRepo(ctx context.Context, repo string, result *module.SourceSyncResult) error {
	owner, name, ok := splitRepo(repo)
	if !ok {
		result.Skipped++
		return fmt.Errorf("repo must use owner/name format")
	}
	repoRoot, err := c.ensurePath(ctx, nil, "GitHub", owner+"/"+name)
	if err != nil {
		return err
	}
	info, err := c.repo(ctx, owner, name)
	if err != nil {
		return err
	}
	if err := c.syncFiles(ctx, owner, name, info.DefaultBranch, repoRoot, result); err != nil {
		return err
	}
	if err := c.syncIssues(ctx, owner, name, repoRoot, result); err != nil {
		return err
	}
	return c.syncReleases(ctx, owner, name, repoRoot, result)
}

type githubRepo struct {
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
}

func (c *GitHubConnector) repo(ctx context.Context, owner, repo string) (githubRepo, error) {
	var info githubRepo
	err := c.getJSON(ctx, fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo), &info)
	if info.DefaultBranch == "" {
		info.DefaultBranch = "main"
	}
	return info, err
}

type treeResponse struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	} `json:"tree"`
}

func (c *GitHubConnector) syncFiles(ctx context.Context, owner, repo, branch string, root *primitive.ObjectID, result *module.SourceSyncResult) error {
	filesRoot, err := c.ensurePath(ctx, root, "Files")
	if err != nil {
		return err
	}
	var tree treeResponse
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, url.PathEscape(branch))
	if err := c.getJSON(ctx, endpoint, &tree); err != nil {
		return err
	}
	for _, item := range tree.Tree {
		if item.Type != "blob" || !isKnowledgeFile(item.Path) {
			continue
		}
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, url.PathEscape(branch), pathEscape(item.Path))
		data, err := c.getBytes(ctx, rawURL)
		if err != nil {
			result.Skipped++
			continue
		}
		targetCollection, err := c.ensurePath(ctx, filesRoot, filepath.Dir(filepath.ToSlash(item.Path)))
		if err != nil {
			return err
		}
		doc := model.Document{
			CollectionID: targetCollection,
			DisplayName:  filepath.Base(item.Path),
			StorageKey:   "github/" + owner + "/" + repo + "/files/" + item.Path,
			MIME:         mimeForPath(item.Path),
			Size:         int64(len(data)),
			Source:       "github",
			SourceType:   "file",
			ExternalID:   fmt.Sprintf("github:%s/%s:file:%s", owner, repo, item.Path),
			ExternalURL:  fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", owner, repo, branch, item.Path),
			Repository:   owner + "/" + repo,
		}
		if err := c.save(ctx, doc, data, result); err != nil {
			return err
		}
	}
	return nil
}

type issueResponse struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	HTMLURL     string `json:"html_url"`
	User        user   `json:"user"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type user struct {
	Login string `json:"login"`
}

func (c *GitHubConnector) syncIssues(ctx context.Context, owner, repo string, root *primitive.ObjectID, result *module.SourceSyncResult) error {
	issuesRoot, err := c.ensurePath(ctx, root, "Issues")
	if err != nil {
		return err
	}
	prRoot, err := c.ensurePath(ctx, root, "Pull Requests")
	if err != nil {
		return err
	}
	for page := 1; ; page++ {
		var issues []issueResponse
		endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?state=all&per_page=100&page=%d", owner, repo, page)
		if err := c.getJSON(ctx, endpoint, &issues); err != nil {
			return err
		}
		if len(issues) == 0 {
			break
		}
		for _, item := range issues {
			sourceType := "issue"
			parent := issuesRoot
			prefix := "issue"
			if item.PullRequest != nil {
				sourceType = "pull_request"
				parent = prRoot
				prefix = "pr"
			}
			content := issueMarkdown(item, owner+"/"+repo, sourceType)
			name := fmt.Sprintf("#%d %s.md", item.Number, safeName(item.Title))
			doc := model.Document{
				CollectionID: parent,
				DisplayName:  name,
				StorageKey:   fmt.Sprintf("github/%s/%s/%s/%d.md", owner, repo, prefix, item.Number),
				MIME:         "text/markdown",
				Size:         int64(len(content)),
				Source:       "github",
				SourceType:   sourceType,
				ExternalID:   fmt.Sprintf("github:%s/%s:%s:%d", owner, repo, sourceType, item.Number),
				ExternalURL:  item.HTMLURL,
				Repository:   owner + "/" + repo,
				Author:       item.User.Login,
			}
			if err := c.save(ctx, doc, []byte(content), result); err != nil {
				return err
			}
		}
	}
	return nil
}

type releaseResponse struct {
	ID          int64  `json:"id"`
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	Author      user   `json:"author"`
	PublishedAt string `json:"published_at"`
}

func (c *GitHubConnector) syncReleases(ctx context.Context, owner, repo string, root *primitive.ObjectID, result *module.SourceSyncResult) error {
	releasesRoot, err := c.ensurePath(ctx, root, "Releases")
	if err != nil {
		return err
	}
	for page := 1; ; page++ {
		var releases []releaseResponse
		endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=100&page=%d", owner, repo, page)
		if err := c.getJSON(ctx, endpoint, &releases); err != nil {
			return err
		}
		if len(releases) == 0 {
			break
		}
		for _, item := range releases {
			title := item.Name
			if title == "" {
				title = item.TagName
			}
			content := releaseMarkdown(item, owner+"/"+repo)
			doc := model.Document{
				CollectionID: releasesRoot,
				DisplayName:  safeName(title) + ".md",
				StorageKey:   fmt.Sprintf("github/%s/%s/releases/%d.md", owner, repo, item.ID),
				MIME:         "text/markdown",
				Size:         int64(len(content)),
				Source:       "github",
				SourceType:   "release",
				ExternalID:   fmt.Sprintf("github:%s/%s:release:%d", owner, repo, item.ID),
				ExternalURL:  item.HTMLURL,
				Repository:   owner + "/" + repo,
				Author:       item.Author.Login,
			}
			if err := c.save(ctx, doc, []byte(content), result); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *GitHubConnector) save(ctx context.Context, doc model.Document, data []byte, result *module.SourceSyncResult) error {
	if err := c.Storage.Upload(ctx, doc.StorageKey, bytes.NewReader(data), int64(len(data)), doc.MIME); err != nil {
		return err
	}
	saved, created, err := c.ReposDB.Documents.UpsertExternal(ctx, doc)
	if err != nil {
		return err
	}
	if created {
		result.Created++
	} else {
		result.Updated++
	}
	result.Processed++
	if c.Enqueue != nil {
		c.Enqueue(saved.ID)
	}
	return nil
}

func (c *GitHubConnector) ensurePath(ctx context.Context, root *primitive.ObjectID, parts ...string) (*primitive.ObjectID, error) {
	parent := root
	for _, part := range parts {
		for _, segment := range strings.Split(filepath.ToSlash(part), "/") {
			segment = strings.TrimSpace(segment)
			if segment == "" || segment == "." {
				continue
			}
			existing, err := c.ReposDB.Collections.FindChild(ctx, parent, segment)
			if err != nil {
				created, createErr := c.ReposDB.Collections.Create(ctx, segment, parent)
				if createErr != nil {
					return nil, createErr
				}
				existing = created
			}
			id := existing.ID
			parent = &id
		}
	}
	return parent, nil
}

func (c *GitHubConnector) getJSON(ctx context.Context, endpoint string, dest any) error {
	body, err := c.getBytes(ctx, endpoint)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dest)
}

func (c *GitHubConnector) getBytes(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("github %s: %s", endpoint, res.Status)
	}
	return io.ReadAll(res.Body)
}

func splitRepo(repo string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(repo), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func isKnowledgeFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".mdx", ".markdown", ".txt", ".rst", ".adoc":
		return true
	default:
		return false
	}
}

func mimeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt":
		return "text/plain"
	default:
		return "text/markdown"
	}
}

func pathEscape(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func issueMarkdown(item issueResponse, repo, sourceType string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s #%d: %s\n\n", sourceTitle(sourceType), item.Number, item.Title)
	fmt.Fprintf(&b, "- Repository: %s\n- State: %s\n- Author: %s\n- URL: %s\n", repo, item.State, item.User.Login, item.HTMLURL)
	if len(item.Labels) > 0 {
		labels := make([]string, len(item.Labels))
		for i, label := range item.Labels {
			labels[i] = label.Name
		}
		fmt.Fprintf(&b, "- Labels: %s\n", strings.Join(labels, ", "))
	}
	b.WriteString("\n")
	b.WriteString(item.Body)
	return b.String()
}

func releaseMarkdown(item releaseResponse, repo string) string {
	var b strings.Builder
	title := item.Name
	if title == "" {
		title = item.TagName
	}
	fmt.Fprintf(&b, "# Release %s\n\n", title)
	fmt.Fprintf(&b, "- Repository: %s\n- Tag: %s\n- Published: %s\n- Author: %s\n- URL: %s\n\n", repo, item.TagName, item.PublishedAt, item.Author.Login, item.HTMLURL)
	b.WriteString(item.Body)
	return b.String()
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "untitled"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "\n", " ", "\t", " ")
	value = replacer.Replace(value)
	if len(value) > 100 {
		value = value[:100]
	}
	return value
}

func sourceTitle(sourceType string) string {
	switch sourceType {
	case "pull_request":
		return "Pull Request"
	case "issue":
		return "Issue"
	default:
		return strings.ReplaceAll(sourceType, "_", " ")
	}
}
