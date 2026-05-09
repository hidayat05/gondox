package cache

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var currentOS = runtime.GOOS

type BinaryInfo struct {
	Name    string
	Version string
	Path    string
	Exists  bool
}

func CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".gondox", "bin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func BinaryPath(name, version string) (string, error) {
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	filename := name + "-" + version
	if currentOS == "windows" {
		filename += ".exe"
	}
	return filepath.Join(dir, filename), nil
}

func CheckBinary(name, version string) BinaryInfo {
	if version == "" || version == "Loading..." {
		return BinaryInfo{Name: name, Version: version, Exists: false}
	}
	path, err := BinaryPath(name, version)
	if err != nil {
		return BinaryInfo{Name: name, Version: version, Exists: false}
	}
	info, err := os.Stat(path)
	exists := err == nil && !info.IsDir()
	return BinaryInfo{
		Name:    name,
		Version: version,
		Path:    path,
		Exists:  exists,
	}
}

func ListCachedVersions(name string) ([]string, error) {
	dir, err := CacheDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var vers []string
	prefix := name + "-"
	for _, e := range entries {
		fname := e.Name()
		if filepath.Ext(fname) == ".exe" {
			fname = fname[:len(fname)-4]
		}
		if strings.HasPrefix(fname, prefix) && len(fname) > len(prefix) {
			ver := fname[len(prefix):]
			if len(ver) > 0 && ver[0] >= '0' && ver[0] <= '9' {
				vers = append(vers, ver)
			}
		}
	}
	return vers, nil
}

func ClearCache() error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
