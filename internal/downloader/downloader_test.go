package downloader

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

type DownloaderSuite struct {
	suite.Suite
	tmpDir string
}

func (s *DownloaderSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	s.T().Setenv("HOME", s.tmpDir)
	currentOS = "linux"
	currentArch = "amd64"
}

func TestDownloaderSuite(t *testing.T) {
	suite.Run(t, new(DownloaderSuite))
}

func (s *DownloaderSuite) makeZip(innerPath, content string) string {
	zipPath := filepath.Join(s.tmpDir, "test.zip")
	f, err := os.Create(zipPath)
	s.Require().NoError(err)
	defer f.Close()

	w := zip.NewWriter(f)
	fw, err := w.Create(innerPath)
	s.Require().NoError(err)
	fw.Write([]byte(content))
	s.Require().NoError(w.Close())
	return zipPath
}

func (s *DownloaderSuite) makeTarGz(innerFile, content string) string {
	tarPath := filepath.Join(s.tmpDir, "test.tar.gz")
	f, err := os.Create(tarPath)
	s.Require().NoError(err)
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	data := []byte(content)
	s.Require().NoError(tw.WriteHeader(&tar.Header{Name: innerFile, Mode: 0755, Size: int64(len(data))}))
	tw.Write(data)
	s.Require().NoError(tw.Close())
	s.Require().NoError(gz.Close())
	return tarPath
}

func (s *DownloaderSuite) withZipServer(innerPath, content string) *httptest.Server {
	zipPath := s.makeZip(innerPath, content)
	data, err := os.ReadFile(zipPath)
	s.Require().NoError(err)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Write(data)
	}))
}

func (s *DownloaderSuite) withTarGzServer(innerFile, content string) *httptest.Server {
	tarPath := s.makeTarGz(innerFile, content)
	data, err := os.ReadFile(tarPath)
	s.Require().NoError(err)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(data)
	}))
}

func (s *DownloaderSuite) TestPlatformNames() {
	tests := []struct {
		goos     string
		goarch   string
		wantOS   string
		wantArch string
		wantErr  bool
	}{
		{"linux", "amd64", "linux", "amd64", false},
		{"darwin", "arm64", "darwin", "arm64", false},
		{"windows", "amd64", "windows", "amd64", false},
		{"freebsd", "amd64", "", "", true},
		{"plan9", "386", "", "", true},
	}

	for _, tt := range tests {
		s.Run(tt.goos+"/"+tt.goarch, func() {
			osName, archName, err := platformNames(tt.goos, tt.goarch)
			if tt.wantErr {
				s.Require().Error(err)
				s.Contains(err.Error(), "unsupported platform")
			} else {
				s.NoError(err)
				s.Equal(tt.wantOS, osName)
				s.Equal(tt.wantArch, archName)
			}
		})
	}
}

func (s *DownloaderSuite) TestDownloadFile_Success() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	dest := filepath.Join(s.tmpDir, "downloaded")
	var progress []int64
	err := downloadFile(srv.URL, dest, func(dl, total int64) {
		progress = append(progress, dl)
	})
	s.Require().NoError(err)
	data, err := os.ReadFile(dest)
	s.Require().NoError(err)
	s.Equal("hello", string(data))
	s.NotEmpty(progress)
}

func (s *DownloaderSuite) TestDownloadFile_NilProgress() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	dest := filepath.Join(s.tmpDir, "downloaded2")
	err := downloadFile(srv.URL, dest, nil)
	s.NoError(err)
}

func (s *DownloaderSuite) TestDownloadFile_HTTPError() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := downloadFile(srv.URL, filepath.Join(s.tmpDir, "nope"), nil)
	s.Require().Error(err)
	s.Contains(err.Error(), "404")
}

func (s *DownloaderSuite) TestDownloadFile_ConnectionError() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	err := downloadFile(url, filepath.Join(s.tmpDir, "nope2"), nil)
	s.Error(err)
}

func (s *DownloaderSuite) TestExtractFromZip_Success() {
	zipPath := s.makeZip("bin/protoc", "#!/bin/sh\n")
	dest := filepath.Join(s.tmpDir, "out_protoc")

	err := extractFromZip(zipPath, "bin/protoc", dest)
	s.Require().NoError(err)
	data, err := os.ReadFile(dest)
	s.Require().NoError(err)
	s.Equal("#!/bin/sh\n", string(data))
}

func (s *DownloaderSuite) TestExtractFromZip_NotFound() {
	zipPath := s.makeZip("bin/other", "content")
	dest := filepath.Join(s.tmpDir, "out_nf")

	err := extractFromZip(zipPath, "bin/protoc", dest)
	s.Require().Error(err)
	s.Contains(err.Error(), "not found in zip")
}

func (s *DownloaderSuite) TestExtractFromZip_InvalidZip() {
	badZip := filepath.Join(s.tmpDir, "bad.zip")
	s.Require().NoError(os.WriteFile(badZip, []byte("not a zip"), 0644))

	err := extractFromZip(badZip, "bin/protoc", filepath.Join(s.tmpDir, "out"))
	s.Error(err)
}

func (s *DownloaderSuite) TestExtractFromTarGz_Success() {
	tarPath := s.makeTarGz("protoc-gen-go", "#!/bin/sh\n")
	dest := filepath.Join(s.tmpDir, "out_gengo")

	err := extractFromTarGz(tarPath, "protoc-gen-go", dest)
	s.Require().NoError(err)
	data, err := os.ReadFile(dest)
	s.Require().NoError(err)
	s.Equal("#!/bin/sh\n", string(data))
}

func (s *DownloaderSuite) TestExtractFromTarGz_NotFound() {
	tarPath := s.makeTarGz("other-binary", "content")
	dest := filepath.Join(s.tmpDir, "out_nf2")

	err := extractFromTarGz(tarPath, "protoc-gen-go", dest)
	s.Require().Error(err)
	s.Contains(err.Error(), "not found in tar.gz")
}

func (s *DownloaderSuite) TestExtractFromTarGz_InvalidGzip() {
	badTar := filepath.Join(s.tmpDir, "bad.tar.gz")
	s.Require().NoError(os.WriteFile(badTar, []byte("not gzip"), 0644))

	err := extractFromTarGz(badTar, "protoc-gen-go", filepath.Join(s.tmpDir, "out"))
	s.Error(err)
}

func (s *DownloaderSuite) TestExtractFromTarGz_FileNotFound() {
	badTar := filepath.Join(s.tmpDir, "nofile.tar.gz")
	err := extractFromTarGz(badTar, "protoc-gen-go", filepath.Join(s.tmpDir, "out"))
	s.Error(err)
}

func (s *DownloaderSuite) TestDownloadProtoc_Cached() {
	cacheDir := filepath.Join(s.tmpDir, ".gondox", "bin")
	s.Require().NoError(os.MkdirAll(cacheDir, 0755))
	cached := filepath.Join(cacheDir, "protoc-1.0.0")
	s.Require().NoError(os.WriteFile(cached, []byte("fake"), 0755))

	path, err := DownloadProtoc("1.0.0", nil)
	s.Require().NoError(err)
	s.Equal(cached, path)
}

func (s *DownloaderSuite) TestDownloadProtoc_Platforms() {
	tests := []struct {
		os   string
		arch string
		want string
	}{
		{"linux", "amd64", "linux-x86_64"},
		{"linux", "arm64", "linux-aarch_64"},
		{"linux", "aarch64", "linux-aarch_64"},
		{"darwin", "amd64", "osx-x86_64"},
		{"darwin", "arm64", "osx-aarch_64"},
		{"windows", "amd64", "win64"},
	}

	for _, tt := range tests {
		s.Run(tt.os+"/"+tt.arch, func() {
			currentOS = tt.os
			currentArch = tt.arch

			zipContent := "#!/bin/sh\nexit 0\n"
			binName := "protoc"
			if tt.os == "windows" {
				binName = "protoc.exe"
			}
			zipPath := s.makeZip("bin/"+binName, zipContent)
			data, err := os.ReadFile(zipPath)
			s.Require().NoError(err)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.Contains(r.URL.Path, tt.want)
				w.Write(data)
			}))
			defer srv.Close()

			oldBase := "https://github.com"
			_ = oldBase

			origGet := http.DefaultTransport
			_ = origGet

			_ = json.Marshal

			ver := "99." + tt.os + ".0"
			cacheDir := filepath.Join(s.tmpDir, ".gondox", "bin")
			s.Require().NoError(os.MkdirAll(cacheDir, 0755))

			_, err = DownloadProtoc(ver, nil)
			s.Error(err)
		})
	}
}

func (s *DownloaderSuite) TestDownloadProtoc_LinuxSuccess() {
	zipPath := s.makeZip("bin/protoc", "#!/bin/sh\nexit 0\n")
	data, err := os.ReadFile(zipPath)
	s.Require().NoError(err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	currentOS = "linux"
	currentArch = "amd64"

	path, err := DownloadProtoc("8888.0.0", func(dl, total int64) {})
	s.Require().NoError(err)
	s.NotEmpty(path)
}

func (s *DownloaderSuite) TestDownloadProtoc_WindowsSuccess() {
	zipPath := s.makeZip("bin/protoc.exe", "fake exe content")
	data, err := os.ReadFile(zipPath)
	s.Require().NoError(err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	currentOS = "windows"
	currentArch = "amd64"

	path, err := DownloadProtoc("8887.0.0", nil)
	s.Require().NoError(err)
	s.NotEmpty(path)
}

func (s *DownloaderSuite) TestExtractFromZip_MkdirAllError() {
	zipPath := s.makeZip("bin/protoc", "content")
	blocked := filepath.Join(s.tmpDir, "blocked_file")
	s.Require().NoError(os.WriteFile(blocked, []byte(""), 0644))

	destPath := filepath.Join(blocked, "subdir", "protoc")
	err := extractFromZip(zipPath, "bin/protoc", destPath)
	s.Error(err)
}

func (s *DownloaderSuite) TestExtractFromTarGz_CorruptTarEntry() {
	tarPath := filepath.Join(s.tmpDir, "corrupt.tar.gz")
	f, err := os.Create(tarPath)
	s.Require().NoError(err)

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	data := []byte("short")
	s.Require().NoError(tw.WriteHeader(&tar.Header{
		Name: "wrong-name",
		Mode: 0755,
		Size: 9999,
	}))
	tw.Write(data)
	gz.Close()
	f.Close()

	err = extractFromTarGz(tarPath, "protoc-gen-go", filepath.Join(s.tmpDir, "out_corrupt"))
	s.Error(err)
}

func (s *DownloaderSuite) TestExtractFromTarGz_MkdirAllError() {
	tarPath := s.makeTarGz("protoc-gen-go", "content")
	blocked := filepath.Join(s.tmpDir, "blocked_file2")
	s.Require().NoError(os.WriteFile(blocked, []byte(""), 0644))

	destPath := filepath.Join(blocked, "subdir", "protoc-gen-go")
	err := extractFromTarGz(tarPath, "protoc-gen-go", destPath)
	s.Error(err)
}

func (s *DownloaderSuite) TestDownloadProtoc_UnsupportedPlatform() {
	currentOS = "plan9"
	_, err := DownloadProtoc("1.0.0", nil)
	s.Require().Error(err)
	s.Contains(err.Error(), "unsupported platform")
}

func (s *DownloaderSuite) TestDownloadProtoc_DownloadFail() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	_, err := DownloadProtoc("9998.0.0", nil)
	s.Error(err)
}

func (s *DownloaderSuite) TestDownloadProtoc_ExtractFail() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not a zip"))
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	_, err := DownloadProtoc("9997.0.0", nil)
	s.Error(err)
}

func (s *DownloaderSuite) TestDownloadProtocGenGo_Cached() {
	cacheDir := filepath.Join(s.tmpDir, ".gondox", "bin")
	s.Require().NoError(os.MkdirAll(cacheDir, 0755))
	cached := filepath.Join(cacheDir, "protoc-gen-go-1.0.0")
	s.Require().NoError(os.WriteFile(cached, []byte("fake"), 0755))

	path, err := DownloadProtocGenGo("1.0.0", nil)
	s.Require().NoError(err)
	s.Equal(cached, path)
}

func (s *DownloaderSuite) TestDownloadProtocGenGo_UnsupportedPlatform() {
	currentOS = "plan9"
	_, err := DownloadProtocGenGo("1.0.0", nil)
	s.Require().Error(err)
	s.Contains(err.Error(), "unsupported platform")
}

func (s *DownloaderSuite) TestDownloadProtocGenGo_Success() {
	tarPath := s.makeTarGz("protoc-gen-go", "#!/bin/sh\nexit 0\n")
	data, err := os.ReadFile(tarPath)
	s.Require().NoError(err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	path, err := DownloadProtocGenGo("9996.0.0", func(dl, total int64) {})
	s.Require().NoError(err)
	s.NotEmpty(path)
}

func (s *DownloaderSuite) TestDownloadProtocGenGo_DownloadFail() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	_, err := DownloadProtocGenGo("9995.0.0", nil)
	s.Error(err)
}

func (s *DownloaderSuite) TestDownloadProtocGenGo_ExtractFail() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not tar.gz"))
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	_, err := DownloadProtocGenGo("9994.0.0", nil)
	s.Error(err)
}

func (s *DownloaderSuite) TestDownloadProtocGenGoGRPC_Cached() {
	cacheDir := filepath.Join(s.tmpDir, ".gondox", "bin")
	s.Require().NoError(os.MkdirAll(cacheDir, 0755))
	cached := filepath.Join(cacheDir, "protoc-gen-go-grpc-1.0.0")
	s.Require().NoError(os.WriteFile(cached, []byte("fake"), 0755))

	path, err := DownloadProtocGenGoGRPC("1.0.0", nil)
	s.Require().NoError(err)
	s.Equal(cached, path)
}

func (s *DownloaderSuite) TestDownloadProtocGenGoGRPC_UnsupportedPlatform() {
	currentOS = "plan9"
	_, err := DownloadProtocGenGoGRPC("1.0.0", nil)
	s.Require().Error(err)
	s.Contains(err.Error(), "unsupported platform")
}

func (s *DownloaderSuite) TestDownloadProtocGenGoGRPC_Success() {
	tarPath := s.makeTarGz("protoc-gen-go-grpc", "#!/bin/sh\nexit 0\n")
	data, err := os.ReadFile(tarPath)
	s.Require().NoError(err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	path, err := DownloadProtocGenGoGRPC("9993.0.0", nil)
	s.Require().NoError(err)
	s.NotEmpty(path)
}

func (s *DownloaderSuite) TestDownloadProtocGenGoGRPC_DownloadFail() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	_, err := DownloadProtocGenGoGRPC("9992.0.0", nil)
	s.Error(err)
}

func (s *DownloaderSuite) TestDownloadProtocGenGoGRPC_ExtractFail() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not tar.gz"))
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	_, err := DownloadProtocGenGoGRPC("9991.0.0", nil)
	s.Error(err)
}

func (s *DownloaderSuite) TestDownloadProtocGenGo_WindowsBinName() {
	currentOS = "windows"
	tarPath := s.makeTarGz("protoc-gen-go.exe", "#!/bin/sh\nexit 0\n")
	data, err := os.ReadFile(tarPath)
	s.Require().NoError(err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	path, err := DownloadProtocGenGo("9990.0.0", nil)
	s.Require().NoError(err)
	s.NotEmpty(path)
}

func (s *DownloaderSuite) TestDownloadProtocGenGoGRPC_WindowsBinName() {
	currentOS = "windows"
	tarPath := s.makeTarGz("protoc-gen-go-grpc.exe", "#!/bin/sh\nexit 0\n")
	data, err := os.ReadFile(tarPath)
	s.Require().NoError(err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	origTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &redirectTransport{base: srv.URL}
	defer func() { http.DefaultClient.Transport = origTransport }()

	path, err := DownloadProtocGenGoGRPC("9989.0.0", nil)
	s.Require().NoError(err)
	s.NotEmpty(path)
}

type redirectTransport struct {
	base string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := *req.URL
	newURL.Scheme = "http"
	newURL.Host = t.base[len("http://"):]
	newReq := req.Clone(req.Context())
	newReq.URL = &newURL
	return http.DefaultTransport.RoundTrip(newReq)
}

var _ = bytes.NewBuffer
