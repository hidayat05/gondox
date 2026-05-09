package downloader

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gondox/internal/cache"
)

var currentOS = runtime.GOOS
var currentArch = runtime.GOARCH

type ProgressFunc func(downloaded, total int64)

func DownloadProtoc(version string, progress ProgressFunc) (string, error) {
	info := cache.CheckBinary("protoc", version)
	if info.Exists {
		return info.Path, nil
	}

	goos := currentOS
	goarch := currentArch

	var platform string
	switch goos {
	case "linux":
		if goarch == "arm64" || goarch == "aarch64" {
			platform = "linux-aarch_64"
		} else {
			platform = "linux-x86_64"
		}
	case "darwin":
		if goarch == "arm64" {
			platform = "osx-aarch_64"
		} else {
			platform = "osx-x86_64"
		}
	case "windows":
		platform = "win64"
	default:
		return "", fmt.Errorf("unsupported platform: %s", goos)
	}

	url := fmt.Sprintf(
		"https://github.com/protocolbuffers/protobuf/releases/download/v%s/protoc-%s-%s.zip",
		version, version, platform,
	)

	destPath := info.Path
	tmpZip := destPath + ".zip.tmp"

	if err := downloadFile(url, tmpZip, progress); err != nil {
		os.Remove(tmpZip)
		return "", fmt.Errorf("failed to download protoc: %w", err)
	}

	binName := "protoc"
	if goos == "windows" {
		binName = "protoc.exe"
	}
	if err := extractFromZip(tmpZip, "bin/"+binName, destPath); err != nil {
		os.Remove(tmpZip)
		return "", fmt.Errorf("failed to extract protoc: %w", err)
	}
	os.Remove(tmpZip)
	os.Chmod(destPath, 0755)
	return destPath, nil
}

func DownloadProtocGenGo(version string, progress ProgressFunc) (string, error) {
	info := cache.CheckBinary("protoc-gen-go", version)
	if info.Exists {
		return info.Path, nil
	}

	goos := currentOS
	goarch := currentArch
	osName, archName, err := platformNames(goos, goarch)
	if err != nil {
		return "", err
	}

	assetName := fmt.Sprintf("protoc-gen-go.v%s.%s.%s.tar.gz", version, osName, archName)
	url := fmt.Sprintf(
		"https://github.com/protocolbuffers/protobuf-go/releases/download/v%s/%s",
		version, assetName,
	)

	binName := "protoc-gen-go"
	if goos == "windows" {
		binName += ".exe"
	}

	return downloadAndExtractTarGz("protoc-gen-go", url, binName, info.Path, progress)
}

func DownloadProtocGenGoGRPC(version string, progress ProgressFunc) (string, error) {
	info := cache.CheckBinary("protoc-gen-go-grpc", version)
	if info.Exists {
		return info.Path, nil
	}

	goos := currentOS
	goarch := currentArch
	osName, archName, err := platformNames(goos, goarch)
	if err != nil {
		return "", err
	}

	assetName := fmt.Sprintf("protoc-gen-go-grpc.v%s.%s.%s.tar.gz", version, osName, archName)
	url := fmt.Sprintf(
		"https://github.com/grpc/grpc-go/releases/download/cmd%%2Fprotoc-gen-go-grpc%%2Fv%s/%s",
		version, assetName,
	)

	binName := "protoc-gen-go-grpc"
	if goos == "windows" {
		binName += ".exe"
	}

	return downloadAndExtractTarGz("protoc-gen-go-grpc", url, binName, info.Path, progress)
}

func downloadAndExtractTarGz(name, url, binName, destPath string, progress ProgressFunc) (string, error) {
	tmpTar := destPath + ".tar.gz.tmp"

	if err := downloadFile(url, tmpTar, progress); err != nil {
		os.Remove(tmpTar)
		return "", fmt.Errorf("failed to download %s: %w", name, err)
	}

	if err := extractFromTarGz(tmpTar, binName, destPath); err != nil {
		os.Remove(tmpTar)
		return "", fmt.Errorf("failed to extract %s: %w", name, err)
	}
	os.Remove(tmpTar)
	os.Chmod(destPath, 0755)
	return destPath, nil
}

func platformNames(goos, goarch string) (osName, archName string, err error) {
	switch goos {
	case "linux":
		osName = "linux"
	case "darwin":
		osName = "darwin"
	case "windows":
		osName = "windows"
	default:
		return "", "", fmt.Errorf("unsupported platform: %s", goos)
	}
	archName = goarch
	return
}

func downloadFile(url, dest string, progress ProgressFunc) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d downloading from: %s", resp.StatusCode, url)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func extractFromZip(zipPath, targetFile, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if name == targetFile {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}

			out, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer out.Close()

			_, err = io.Copy(out, rc)
			return err
		}
	}
	return fmt.Errorf("file %s not found in zip", targetFile)
}

func extractFromTarGz(tarPath, targetFile, destPath string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Base(hdr.Name)
		if name == targetFile {
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}
			out, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, tr)
			return err
		}
	}
	return fmt.Errorf("file %s not found in tar.gz", targetFile)
}
