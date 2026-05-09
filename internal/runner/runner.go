package runner

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gondox/internal/cache"
)

var googleProtobufRawBaseURL = "https://raw.githubusercontent.com/protocolbuffers/protobuf/main/src"

var commonGoogleProtobufFiles = []string{
	"google/protobuf/any.proto",
	"google/protobuf/api.proto",
	"google/protobuf/descriptor.proto",
	"google/protobuf/duration.proto",
	"google/protobuf/empty.proto",
	"google/protobuf/field_mask.proto",
	"google/protobuf/source_context.proto",
	"google/protobuf/struct.proto",
	"google/protobuf/timestamp.proto",
	"google/protobuf/type.proto",
	"google/protobuf/wrappers.proto",
}

type RunConfig struct {
	ProtocPath        string
	ProtocGenGoPath   string
	ProtocGenGrpcPath string
	ProtoDir          string
	DestDir           string
	Output            io.Writer
}

func Run(cfg RunConfig) error {
	if err := validateBinary(cfg.ProtocPath, "protoc"); err != nil {
		return err
	}
	if err := validateBinary(cfg.ProtocGenGoPath, "protoc-gen-go"); err != nil {
		return err
	}
	if err := validateBinary(cfg.ProtocGenGrpcPath, "protoc-gen-go-grpc"); err != nil {
		return err
	}

	fmt.Fprintf(cfg.Output, "Binaries in use:\n")
	fmt.Fprintf(cfg.Output, "  protoc             → %s\n", cfg.ProtocPath)
	fmt.Fprintf(cfg.Output, "  protoc-gen-go      → %s\n", cfg.ProtocGenGoPath)
	fmt.Fprintf(cfg.Output, "  protoc-gen-go-grpc → %s\n", cfg.ProtocGenGrpcPath)
	fmt.Fprintln(cfg.Output)

	var protoFiles []string
	err := filepath.Walk(cfg.ProtoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".proto") {
			protoFiles = append(protoFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to scan proto directory: %w", err)
	}

	if len(protoFiles) == 0 {
		fmt.Fprintln(cfg.Output, "⚠  No .proto files found in the selected directory.")
		return nil
	}

	if err := os.MkdirAll(cfg.DestDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	commonIncludeDir, err := ensureCommonProtoIncludes(cfg.Output)
	if err != nil {
		fmt.Fprintf(cfg.Output, "⚠  Failed preparing common protobuf includes: %v\n", err)
	}

	cacheDir := filepath.Dir(cfg.ProtocGenGoPath)
	cleanEnv := buildCleanEnv(cacheDir)

	success, failed := 0, 0

	for _, protoFile := range protoFiles {
		args := buildProtocArgs(cfg, protoFile, commonIncludeDir)

		fmt.Fprintf(cfg.Output, "\n▶ %s\n", filepath.Base(protoFile))

		cmd := exec.Command(cfg.ProtocPath, args...)
		cmd.Env = cleanEnv
		cmd.Stdout = cfg.Output
		cmd.Stderr = cfg.Output

		if err := cmd.Run(); err != nil {
			fmt.Fprintf(cfg.Output, "  ✗ Error: %v\n", err)
			failed++
		} else {
			fmt.Fprintf(cfg.Output, "  ✓ Success\n")
			success++
		}
	}

	fmt.Fprintf(cfg.Output, "\n─────────────────────────────\n")
	fmt.Fprintf(cfg.Output, "✅ Done: %d succeeded, %d failed out of %d files\n", success, failed, len(protoFiles))
	return nil
}

func buildProtocArgs(cfg RunConfig, protoFile, includeDir string) []string {
	args := []string{"--proto_path=" + cfg.ProtoDir}
	if includeDir != "" {
		args = append(args, "--proto_path="+includeDir)
	}
	args = append(args,
		"--go_out="+cfg.DestDir,
		"--go_opt=paths=source_relative",
		"--go-grpc_out="+cfg.DestDir,
		"--go-grpc_opt=paths=source_relative",
		"--plugin=protoc-gen-go="+cfg.ProtocGenGoPath,
		"--plugin=protoc-gen-go-grpc="+cfg.ProtocGenGrpcPath,
		protoFile,
	)
	return args
}

func ensureCommonProtoIncludes(out io.Writer) (string, error) {
	includeDir, err := commonProtoIncludeDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(includeDir, 0755); err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	for _, rel := range commonGoogleProtobufFiles {
		target := filepath.Join(includeDir, filepath.FromSlash(rel))
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}

		url := strings.TrimRight(googleProtobufRawBaseURL, "/") + "/" + rel
		if err := downloadTextFile(client, url, target); err != nil {
			fmt.Fprintf(out, "⚠  Could not fetch %s: %v\n", rel, err)
			continue
		}
		fmt.Fprintf(out, "✓ Cached include: %s\n", rel)
	}

	return includeDir, nil
}

func commonProtoIncludeDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("GONDOX_COMMON_PROTO_DIR")); v != "" {
		return v, nil
	}
	binDir, err := cache.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(binDir), "includes"), nil
}

func downloadTextFile(client *http.Client, url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func buildCleanEnv(cacheDir string) []string {
	result := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") {
			continue
		}
		result = append(result, kv)
	}
	result = append(result, "PATH="+cacheDir)
	return result
}

func validateBinary(path, name string) error {
	if path == "" {
		return fmt.Errorf("binary path for %s is empty", name)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("binary %s not found at %s: %w", name, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("path %s is a directory, not a binary", path)
	}
	if info.Mode()&0111 == 0 {
		if cherr := os.Chmod(path, 0755); cherr != nil {
			return fmt.Errorf("binary %s is not executable: %s", name, path)
		}
	}
	return nil
}
