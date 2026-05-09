package versions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gondox/internal/cache"
)

type RateLimitError struct {
	Err error
}

func (e RateLimitError) Error() string {
	return e.Err.Error()
}

func IsRateLimitError(err error) bool {
	_, ok := err.(RateLimitError)
	return ok
}

var githubAPIBase = "https://api.github.com"

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

func FetchProtocVersions() ([]string, error) {
	if cached, err := loadCachedVersions("protoc"); err == nil && len(cached) > 0 {
		return cached, nil
	}

	vers, err := fetchGitHubVersions("protocolbuffers/protobuf", func(tag string) bool {
		lower := strings.ToLower(tag)
		return !strings.Contains(lower, "rc") &&
			!strings.Contains(lower, "alpha") &&
			!strings.Contains(lower, "beta")
	})
	if err == nil && len(vers) > 0 {
		_ = cacheVersions("protoc", vers)
		return vers, nil
	}
	if err != nil {
		return fallbackCachedVersions("protoc")
	}
	return vers, err
}

func FetchProtocGenGoVersions() ([]string, error) {
	if cached, err := loadCachedVersions("protoc-gen-go"); err == nil && len(cached) > 0 {
		return cached, nil
	}

	vers, err := fetchGitHubVersions("protocolbuffers/protobuf-go", func(tag string) bool {
		lower := strings.ToLower(tag)
		return !strings.Contains(lower, "rc") &&
			!strings.Contains(lower, "alpha") &&
			!strings.Contains(lower, "beta")
	})
	if err == nil && len(vers) > 0 {
		_ = cacheVersions("protoc-gen-go", vers)
		return vers, nil
	}
	if err != nil {
		return fallbackCachedVersions("protoc-gen-go")
	}
	return vers, err
}

func FetchProtocGenGoGRPCVersions() ([]string, error) {
	if cached, err := loadCachedVersions("protoc-gen-go-grpc"); err == nil && len(cached) > 0 {
		return cached, nil
	}

	releases, err := fetchRawReleases("grpc/grpc-go")
	if err != nil {
		return fallbackCachedVersions("protoc-gen-go-grpc")
	}

	prefix := "cmd/protoc-gen-go-grpc/v"
	var vers []string
	seen := map[string]bool{}

	for _, r := range releases {
		if r.Prerelease || r.Draft {
			continue
		}
		tag := r.TagName
		if strings.HasPrefix(tag, prefix) {
			ver := strings.TrimPrefix(tag, prefix)
			lower := strings.ToLower(ver)
			if !strings.Contains(lower, "rc") && !strings.Contains(lower, "alpha") && !seen[ver] {
				seen[ver] = true
				vers = append(vers, ver)
			}
		}
	}

	sortVersionsDesc(vers)
	_ = cacheVersions("protoc-gen-go-grpc", vers)
	return vers, nil
}

func fallbackCachedVersions(name string) ([]string, error) {
	cached, err := cache.ListCachedVersions(name)
	if err != nil || len(cached) == 0 {
		return nil, fmt.Errorf("no cached versions available for %s", name)
	}
	sortVersionsDesc(cached)
	return cached, nil
}

func versionCacheDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("GONDOX_VERSION_CACHE_DIR")); v != "" {
		return v, nil
	}
	binDir, err := cache.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(binDir), "versions"), nil
}

func cacheVersionList(name string) string {
	return filepath.Join(name + ".json")
}

func cacheVersions(name string, vers []string) error {
	dir, err := versionCacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := filepath.Join(dir, cacheVersionList(name))
	data, err := json.Marshal(vers)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func loadCachedVersions(name string) ([]string, error) {
	dir, err := versionCacheDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, cacheVersionList(name))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var vers []string
	if err := json.Unmarshal(data, &vers); err != nil {
		return nil, err
	}
	return vers, nil
}

func HasCachedVersionList(name string) bool {
	vers, err := loadCachedVersions(name)
	return err == nil && len(vers) > 0
}

func fetchGitHubVersions(repo string, filter func(string) bool) ([]string, error) {
	releases, err := fetchRawReleases(repo)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var vers []string

	for _, r := range releases {
		if r.Prerelease || r.Draft {
			continue
		}
		tag := r.TagName
		if filter(tag) && !seen[tag] {
			seen[tag] = true
			ver := strings.TrimPrefix(tag, "v")
			vers = append(vers, ver)
		}
	}

	sortVersionsDesc(vers)
	return vers, nil
}

func fetchRawReleases(repo string) ([]githubRelease, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	var all []githubRelease
	page := 1

	for {
		url := fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", githubAPIBase, repo, page)

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "gondox-app/1.0")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch releases from %s: %w", repo, err)
		}

		if resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			return nil, RateLimitError{fmt.Errorf("GitHub API rate limit reached, try again later")}
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub API error %d for repo %s", resp.StatusCode, repo)
		}

		var releases []githubRelease
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		resp.Body.Close()

		all = append(all, releases...)

		if len(releases) < 100 {
			break
		}
		page++
		if page > 5 {
			break
		}
	}

	return all, nil
}

func sortVersionsDesc(vers []string) {
	sort.Slice(vers, func(i, j int) bool {
		return compareVersion(vers[i], vers[j]) > 0
	})
}

func compareVersion(a, b string) int {
	aParts := parseVersion(a)
	bParts := parseVersion(b)
	for i := 0; i < 3; i++ {
		if aParts[i] > bParts[i] {
			return 1
		}
		if aParts[i] < bParts[i] {
			return -1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		fmt.Sscanf(parts[i], "%d", &result[i])
	}
	return result
}
