package runner

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type RunnerSuite struct {
	suite.Suite
	tmpDir string
}

func (s *RunnerSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	includeDir := filepath.Join(s.tmpDir, "includes")
	s.T().Setenv("GONDOX_COMMON_PROTO_DIR", includeDir)
	for _, rel := range commonGoogleProtobufFiles {
		path := filepath.Join(includeDir, filepath.FromSlash(rel))
		s.Require().NoError(os.MkdirAll(filepath.Dir(path), 0755))
		s.Require().NoError(os.WriteFile(path, []byte(`syntax = "proto3";`), 0644))
	}
}

func TestRunnerSuite(t *testing.T) {
	suite.Run(t, new(RunnerSuite))
}

func (s *RunnerSuite) fakeExec(name string, exitCode int) string {
	path := filepath.Join(s.tmpDir, name)
	content := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	s.Require().NoError(os.WriteFile(path, []byte(content), 0755))
	return path
}

func (s *RunnerSuite) protoDir(files ...string) string {
	dir := filepath.Join(s.tmpDir, "protos")
	s.Require().NoError(os.MkdirAll(dir, 0755))
	for _, f := range files {
		s.Require().NoError(os.WriteFile(filepath.Join(dir, f), []byte(`syntax = "proto3";`), 0644))
	}
	return dir
}

func (s *RunnerSuite) TestBuildCleanEnv() {
	s.T().Setenv("PATH", "/usr/bin:/usr/local/bin")
	s.T().Setenv("HOME", "/home/user")
	s.T().Setenv("GOPATH", "/go")

	cacheDir := "/custom/cache"
	env := buildCleanEnv(cacheDir)

	pathEntries := []string{}
	otherEntries := []string{}
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			pathEntries = append(pathEntries, kv)
		} else {
			otherEntries = append(otherEntries, kv)
		}
	}

	s.Len(pathEntries, 1)
	s.Equal("PATH="+cacheDir, pathEntries[0])
	s.NotEmpty(otherEntries)
}

func (s *RunnerSuite) TestValidateBinary() {
	existingExec := s.fakeExec("valid", 0)

	existingDir := filepath.Join(s.tmpDir, "adir")
	s.Require().NoError(os.MkdirAll(existingDir, 0755))

	noExecFile := filepath.Join(s.tmpDir, "noexec")
	s.Require().NoError(os.WriteFile(noExecFile, []byte("#!/bin/sh\n"), 0644))

	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		{"empty path", "", true, "is empty"},
		{"not found", "/nonexistent/path/bin", true, "not found"},
		{"is directory", existingDir, true, "is a directory"},
		{"not executable auto-fixed", noExecFile, false, ""},
		{"executable", existingExec, false, ""},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := validateBinary(tt.path, "test-tool")
			if tt.wantErr {
				s.Require().Error(err)
				s.Contains(err.Error(), tt.errMsg)
			} else {
				s.NoError(err)
			}
		})
	}
}

func (s *RunnerSuite) TestRun_ValidateBinaryFails() {
	validProtoc := s.fakeExec("protoc", 0)
	validGenGo := s.fakeExec("protoc-gen-go", 0)

	tests := []struct {
		name      string
		cfg       RunConfig
		errSubstr string
	}{
		{
			"empty ProtocPath",
			RunConfig{ProtocPath: "", ProtocGenGoPath: validGenGo, ProtocGenGrpcPath: validGenGo, Output: &bytes.Buffer{}},
			"is empty",
		},
		{
			"empty ProtocGenGoPath",
			RunConfig{ProtocPath: validProtoc, ProtocGenGoPath: "", ProtocGenGrpcPath: validGenGo, Output: &bytes.Buffer{}},
			"is empty",
		},
		{
			"empty ProtocGenGrpcPath",
			RunConfig{ProtocPath: validProtoc, ProtocGenGoPath: validGenGo, ProtocGenGrpcPath: "", Output: &bytes.Buffer{}},
			"is empty",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := Run(tt.cfg)
			s.Require().Error(err)
			s.Contains(err.Error(), tt.errSubstr)
		})
	}
}

func (s *RunnerSuite) TestRun_WalkFails() {
	protoc := s.fakeExec("protoc", 0)
	genGo := s.fakeExec("protoc-gen-go", 0)
	genGrpc := s.fakeExec("protoc-gen-go-grpc", 0)

	var out bytes.Buffer
	err := Run(RunConfig{
		ProtocPath:        protoc,
		ProtocGenGoPath:   genGo,
		ProtocGenGrpcPath: genGrpc,
		ProtoDir:          filepath.Join(s.tmpDir, "nonexistent_proto_dir"),
		DestDir:           filepath.Join(s.tmpDir, "out"),
		Output:            &out,
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "failed to scan proto directory")
}

func (s *RunnerSuite) TestRun_NoProtoFiles() {
	protoc := s.fakeExec("protoc", 0)
	genGo := s.fakeExec("protoc-gen-go", 0)
	genGrpc := s.fakeExec("protoc-gen-go-grpc", 0)
	protoDir := s.protoDir()

	var out bytes.Buffer
	err := Run(RunConfig{
		ProtocPath:        protoc,
		ProtocGenGoPath:   genGo,
		ProtocGenGrpcPath: genGrpc,
		ProtoDir:          protoDir,
		DestDir:           filepath.Join(s.tmpDir, "out"),
		Output:            &out,
	})
	s.NoError(err)
	s.Contains(out.String(), "No .proto files found")
}

func (s *RunnerSuite) TestRun_MkdirAllFails() {
	protoc := s.fakeExec("protoc", 0)
	genGo := s.fakeExec("protoc-gen-go", 0)
	genGrpc := s.fakeExec("protoc-gen-go-grpc", 0)
	protoDir := s.protoDir("service.proto")

	blockedDest := filepath.Join(s.tmpDir, "blocked_dest")
	s.Require().NoError(os.WriteFile(blockedDest, []byte(""), 0644))

	var out bytes.Buffer
	err := Run(RunConfig{
		ProtocPath:        protoc,
		ProtocGenGoPath:   genGo,
		ProtocGenGrpcPath: genGrpc,
		ProtoDir:          protoDir,
		DestDir:           blockedDest,
		Output:            &out,
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "failed to create output directory")
}

func (s *RunnerSuite) TestRun_CommandSucceeds() {
	protoc := s.fakeExec("protoc", 0)
	genGo := s.fakeExec("protoc-gen-go", 0)
	genGrpc := s.fakeExec("protoc-gen-go-grpc", 0)
	protoDir := s.protoDir("a.proto", "b.proto")
	destDir := filepath.Join(s.tmpDir, "out")

	var out bytes.Buffer
	err := Run(RunConfig{
		ProtocPath:        protoc,
		ProtocGenGoPath:   genGo,
		ProtocGenGrpcPath: genGrpc,
		ProtoDir:          protoDir,
		DestDir:           destDir,
		Output:            &out,
	})
	s.NoError(err)
	s.Contains(out.String(), "✓ Success")
	s.Contains(out.String(), "2 succeeded")
}

func (s *RunnerSuite) TestRun_CommandFails() {
	protoc := s.fakeExec("protoc_fail", 1)
	genGo := s.fakeExec("protoc-gen-go", 0)
	genGrpc := s.fakeExec("protoc-gen-go-grpc", 0)
	protoDir := s.protoDir("c.proto")
	destDir := filepath.Join(s.tmpDir, "out2")

	var out bytes.Buffer
	err := Run(RunConfig{
		ProtocPath:        protoc,
		ProtocGenGoPath:   genGo,
		ProtocGenGrpcPath: genGrpc,
		ProtoDir:          protoDir,
		DestDir:           destDir,
		Output:            &out,
	})
	s.NoError(err)
	s.Contains(out.String(), "✗ Error")
	s.Contains(out.String(), "0 succeeded, 1 failed")
}

func (s *RunnerSuite) TestBuildProtocArgs_IncludesCommonDir() {
	cfg := RunConfig{
		ProtocGenGoPath:   "/tmp/protoc-gen-go",
		ProtocGenGrpcPath: "/tmp/protoc-gen-go-grpc",
		ProtoDir:          "/work/proto",
		DestDir:           "/work/out",
	}

	args := buildProtocArgs(cfg, "/work/proto/a.proto", "/cache/includes")
	s.Contains(args, "--proto_path=/work/proto")
	s.Contains(args, "--proto_path=/cache/includes")
}

func (s *RunnerSuite) TestCommonProtoIncludeDir_UsesEnvOverride() {
	expected := filepath.Join(s.tmpDir, "custom-includes")
	s.T().Setenv("GONDOX_COMMON_PROTO_DIR", expected)

	actual, err := commonProtoIncludeDir()
	s.Require().NoError(err)
	s.Equal(expected, actual)
}
