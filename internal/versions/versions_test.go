package versions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/suite"
)

const testCacheDirEnv = "GONDOX_TEST_CACHE_DIR"

type VersionsSuite struct {
	suite.Suite
	origBase string
}

func (s *VersionsSuite) SetupSuite() {
	s.origBase = githubAPIBase
}

func (s *VersionsSuite) TearDownSuite() {
	githubAPIBase = s.origBase
}

func (s *VersionsSuite) SetupTest() {
	s.T().Setenv("GONDOX_VERSION_CACHE_DIR", s.T().TempDir())
}

func TestVersionsSuite(t *testing.T) {
	suite.Run(t, new(VersionsSuite))
}

func (s *VersionsSuite) withServer(handler http.HandlerFunc, fn func()) {
	srv := httptest.NewServer(handler)
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()
	fn()
}

func makeReleases(n int, tagFn func(i int) string) []githubRelease {
	out := make([]githubRelease, n)
	for i := range out {
		out[i] = githubRelease{TagName: tagFn(i), Prerelease: false, Draft: false}
	}
	return out
}

func (s *VersionsSuite) TestParseVersion() {
	tests := []struct {
		input string
		want  [3]int
	}{
		{"1.2.3", [3]int{1, 2, 3}},
		{"v1.2.3", [3]int{1, 2, 3}},
		{"10.20.30", [3]int{10, 20, 30}},
		{"1.0.0", [3]int{1, 0, 0}},
		{"0.0.0", [3]int{0, 0, 0}},
		{"1.2", [3]int{1, 2, 0}},
		{"1", [3]int{1, 0, 0}},
		{"", [3]int{0, 0, 0}},
		{"abc", [3]int{0, 0, 0}},
	}

	for _, tt := range tests {
		s.Run(tt.input, func() {
			s.Equal(tt.want, parseVersion(tt.input))
		})
	}
}

func (s *VersionsSuite) TestCompareVersion() {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "1.2.3", "1.2.3", 0},
		{"major greater", "2.0.0", "1.9.9", 1},
		{"major less", "1.0.0", "2.0.0", -1},
		{"minor greater", "1.3.0", "1.2.9", 1},
		{"minor less", "1.2.0", "1.3.0", -1},
		{"patch greater", "1.2.4", "1.2.3", 1},
		{"patch less", "1.2.3", "1.2.4", -1},
		{"all zeros", "0.0.0", "0.0.0", 0},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.want, compareVersion(tt.a, tt.b))
		})
	}
}

func (s *VersionsSuite) TestSortVersionsDesc() {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"already sorted", []string{"3.0.0", "2.0.0", "1.0.0"}, []string{"3.0.0", "2.0.0", "1.0.0"}},
		{"reverse order", []string{"1.0.0", "2.0.0", "3.0.0"}, []string{"3.0.0", "2.0.0", "1.0.0"}},
		{"mixed", []string{"1.5.0", "2.0.0", "1.10.0"}, []string{"2.0.0", "1.10.0", "1.5.0"}},
		{"single", []string{"1.0.0"}, []string{"1.0.0"}},
		{"empty", []string{}, []string{}},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			in := make([]string, len(tt.input))
			copy(in, tt.input)
			sortVersionsDesc(in)
			s.Equal(tt.want, in)
		})
	}
}

func (s *VersionsSuite) TestFetchRawReleases_Success_SinglePage() {
	releases := makeReleases(3, func(i int) string { return "v1." + strconv.Itoa(i) + ".0" })

	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releases)
	}, func() {
		got, err := fetchRawReleases("myrepo/myproject")
		s.Require().NoError(err)
		s.Len(got, 3)
	})
}

func (s *VersionsSuite) TestFetchRawReleases_MultiPage() {
	page1 := makeReleases(100, func(i int) string { return "v2." + strconv.Itoa(i) + ".0" })
	page2 := makeReleases(5, func(i int) string { return "v1." + strconv.Itoa(i) + ".0" })

	calls := 0
	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("page") == "1" {
			json.NewEncoder(w).Encode(page1)
		} else {
			json.NewEncoder(w).Encode(page2)
		}
	}, func() {
		got, err := fetchRawReleases("myrepo/paged")
		s.Require().NoError(err)
		s.Len(got, 105)
		s.Equal(2, calls)
	})
}

func (s *VersionsSuite) TestFetchRawReleases_MaxPageLimit() {
	full := makeReleases(100, func(i int) string { return "v1." + strconv.Itoa(i) + ".0" })

	pageCount := 0
	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		pageCount++
		json.NewEncoder(w).Encode(full)
	}, func() {
		got, err := fetchRawReleases("myrepo/bigproject")
		s.Require().NoError(err)
		s.Len(got, 500)
		s.Equal(5, pageCount)
	})
}

func (s *VersionsSuite) TestFetchRawReleases_Forbidden() {
	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}, func() {
		_, err := fetchRawReleases("myrepo/rate-limited")
		s.Require().Error(err)
		s.Contains(err.Error(), "rate limit")
	})
}

func (s *VersionsSuite) TestFetchRawReleases_NonOKStatus() {
	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, func() {
		_, err := fetchRawReleases("myrepo/broken")
		s.Require().Error(err)
		s.Contains(err.Error(), "500")
	})
}

func (s *VersionsSuite) TestFetchRawReleases_InvalidJSON() {
	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}, func() {
		_, err := fetchRawReleases("myrepo/badjson")
		s.Require().Error(err)
		s.Contains(err.Error(), "failed to parse response")
	})
}

func (s *VersionsSuite) TestFetchRawReleases_ConnectionError() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	old := githubAPIBase
	githubAPIBase = url
	defer func() { githubAPIBase = old }()

	_, err := fetchRawReleases("myrepo/closed")
	s.Require().Error(err)
	s.Contains(err.Error(), "failed to fetch releases")
}

func (s *VersionsSuite) TestFetchProtocVersions_FetchError() {
	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}, func() {
		tmpDir := s.T().TempDir()
		s.T().Setenv("HOME", tmpDir)
		_, err := FetchProtocVersions()
		s.Error(err)
	})
}

func (s *VersionsSuite) TestFetchProtocGenGoVersions_FetchError() {
	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}, func() {
		tmpDir := s.T().TempDir()
		s.T().Setenv("HOME", tmpDir)
		_, err := FetchProtocGenGoVersions()
		s.Error(err)
	})
}

func (s *VersionsSuite) TestFetchRawReleases_InvalidURLScheme() {
	old := githubAPIBase
	githubAPIBase = "h ttp://invalid host"
	defer func() { githubAPIBase = old }()

	_, err := fetchRawReleases("repo/project")
	s.Error(err)
}

func (s *VersionsSuite) TestFetchProtocVersions() {
	releases := []githubRelease{
		{TagName: "v27.3", Prerelease: false, Draft: false},
		{TagName: "v27.3-rc1", Prerelease: false, Draft: false},
		{TagName: "v27.3-alpha", Prerelease: false, Draft: false},
		{TagName: "v27.3-beta1", Prerelease: false, Draft: false},
		{TagName: "v26.1", Prerelease: true, Draft: false},
		{TagName: "v25.0", Prerelease: false, Draft: true},
		{TagName: "v27.3", Prerelease: false, Draft: false},
	}

	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releases)
	}, func() {
		vers, err := FetchProtocVersions()
		s.Require().NoError(err)
		s.Equal([]string{"27.3"}, vers)
	})
}

func (s *VersionsSuite) TestFetchProtocGenGoVersions() {
	releases := []githubRelease{
		{TagName: "v1.34.2", Prerelease: false, Draft: false},
		{TagName: "v1.34.2-rc1", Prerelease: false, Draft: false},
		{TagName: "v1.33.0", Prerelease: false, Draft: false},
		{TagName: "v1.32.0", Prerelease: true, Draft: false},
	}

	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releases)
	}, func() {
		vers, err := FetchProtocGenGoVersions()
		s.Require().NoError(err)
		s.Equal([]string{"1.34.2", "1.33.0"}, vers)
	})
}

func (s *VersionsSuite) TestFetchProtocGenGoGRPCVersions() {
	releases := []githubRelease{
		{TagName: "cmd/protoc-gen-go-grpc/v1.5.1", Prerelease: false, Draft: false},
		{TagName: "cmd/protoc-gen-go-grpc/v1.5.1", Prerelease: false, Draft: false},
		{TagName: "cmd/protoc-gen-go-grpc/v1.4.0", Prerelease: false, Draft: false},
		{TagName: "cmd/protoc-gen-go-grpc/v1.5.1-rc1", Prerelease: false, Draft: false},
		{TagName: "cmd/protoc-gen-go-grpc/v1.3.0", Prerelease: true, Draft: false},
		{TagName: "other/tool/v2.0.0", Prerelease: false, Draft: false},
		{TagName: "v1.0.0", Prerelease: false, Draft: false},
	}

	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releases)
	}, func() {
		vers, err := FetchProtocGenGoGRPCVersions()
		s.Require().NoError(err)
		s.Equal([]string{"1.5.1", "1.4.0"}, vers)
	})
}

func (s *VersionsSuite) TestFetchProtocGenGoGRPCVersions_Error() {
	s.withServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}, func() {
		tmpDir := s.T().TempDir()
		s.T().Setenv("HOME", tmpDir)
		_, err := FetchProtocGenGoGRPCVersions()
		s.Error(err)
	})
}

func (s *VersionsSuite) TestVersionCaching() {
	tmpDir := s.T().TempDir()
	s.T().Setenv("GONDOX_VERSION_CACHE_DIR", tmpDir)

	vers := []string{"1.2.3", "1.2.2", "1.2.1"}
	err := cacheVersions("test-tool", vers)
	s.NoError(err)

	loaded, err := loadCachedVersions("test-tool")
	s.NoError(err)
	s.Equal(vers, loaded)
}

func (s *VersionsSuite) TestLoadCachedVersions_NotFound() {
	tmpDir := s.T().TempDir()
	s.T().Setenv("GONDOX_VERSION_CACHE_DIR", tmpDir)

	_, err := loadCachedVersions("nonexistent-tool")
	s.Error(err)
}
