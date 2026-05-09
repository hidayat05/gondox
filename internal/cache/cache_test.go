package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

type CacheSuite struct {
	suite.Suite
	tmpDir string
}

func (s *CacheSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	s.T().Setenv("HOME", s.tmpDir)
	currentOS = "linux"
}

func TestCacheSuite(t *testing.T) {
	suite.Run(t, new(CacheSuite))
}

func (s *CacheSuite) TestCacheDir() {
	dir, err := CacheDir()
	s.Require().NoError(err)
	s.Equal(filepath.Join(s.tmpDir, ".gondox", "bin"), dir)

	info, err := os.Stat(dir)
	s.Require().NoError(err)
	s.True(info.IsDir())

	dir2, err := CacheDir()
	s.NoError(err)
	s.Equal(dir, dir2)
}

func (s *CacheSuite) TestCacheDirMkdirFails() {
	blocked := filepath.Join(s.tmpDir, ".gondox")
	s.Require().NoError(os.WriteFile(blocked, []byte("block"), 0644))

	_, err := CacheDir()
	s.Error(err)
}

func (s *CacheSuite) TestBinaryPath() {
	tests := []struct {
		name       string
		os         string
		binName    string
		version    string
		wantSuffix string
	}{
		{"linux binary", "linux", "protoc", "27.3", "protoc-27.3"},
		{"linux plugin", "linux", "protoc-gen-go", "1.34.2", "protoc-gen-go-1.34.2"},
		{"windows binary", "windows", "protoc", "27.3", "protoc-27.3.exe"},
		{"windows plugin", "windows", "protoc-gen-go-grpc", "1.5.1", "protoc-gen-go-grpc-1.5.1.exe"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			currentOS = tt.os
			path, err := BinaryPath(tt.binName, tt.version)
			s.Require().NoError(err)
			s.Equal(tt.wantSuffix, filepath.Base(path))
		})
	}
}

func (s *CacheSuite) TestCheckBinary() {
	dir, err := CacheDir()
	s.Require().NoError(err)

	tests := []struct {
		name       string
		version    string
		createFile bool
		wantExists bool
	}{
		{"empty version", "", false, false},
		{"loading version", "Loading...", false, false},
		{"file not found", "9.9.9", false, false},
		{"file exists", "1.0.0", true, true},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.createFile {
				p := filepath.Join(dir, "chkbin-"+tt.version)
				s.Require().NoError(os.WriteFile(p, []byte(""), 0755))
			}
			info := CheckBinary("chkbin", tt.version)
			s.Equal(tt.wantExists, info.Exists)
			s.Equal("chkbin", info.Name)
			s.Equal(tt.version, info.Version)
			if tt.createFile {
				s.NotEmpty(info.Path)
			}
		})
	}
}

func (s *CacheSuite) TestCheckBinaryPathError() {
	blocked := filepath.Join(s.tmpDir, ".gondox")
	s.Require().NoError(os.WriteFile(blocked, []byte("block"), 0644))

	info := CheckBinary("protoc", "1.0.0")
	s.False(info.Exists)
}

func (s *CacheSuite) TestListCachedVersions_CacheDirError() {
	blocked := filepath.Join(s.tmpDir, ".gondox")
	s.Require().NoError(os.WriteFile(blocked, []byte("block"), 0644))

	_, err := ListCachedVersions("protoc")
	s.Error(err)
}

func (s *CacheSuite) TestListCachedVersions_ReadDirError() {
	dir, err := CacheDir()
	s.Require().NoError(err)

	s.Require().NoError(os.Chmod(dir, 0000))
	defer os.Chmod(dir, 0755)

	_, err = ListCachedVersions("protoc")
	s.Error(err)
}

func (s *CacheSuite) TestListCachedVersions() {
	dir, err := CacheDir()
	s.Require().NoError(err)

	fixtures := []string{
		"protoc-27.3",
		"protoc-26.1",
		"protoc-25.0.exe",
		"protoc-gen-go-1.34.2",
		"other-tool-2.0.0",
		"protoc-",
	}
	for _, f := range fixtures {
		s.Require().NoError(os.WriteFile(filepath.Join(dir, f), []byte(""), 0755))
	}

	tests := []struct {
		binName  string
		expected []string
	}{
		{"protoc", []string{"27.3", "26.1", "25.0"}},
		{"protoc-gen-go", []string{"1.34.2"}},
		{"other-tool", []string{"2.0.0"}},
		{"nonexistent", nil},
	}

	for _, tt := range tests {
		s.Run(tt.binName, func() {
			vers, err := ListCachedVersions(tt.binName)
			s.NoError(err)
			s.ElementsMatch(tt.expected, vers)
		})
	}
}
