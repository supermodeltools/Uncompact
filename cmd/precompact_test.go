package cmd

import "testing"

func TestLooksLikeFilePath_Positive(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"go source file", "cmd/run.go"},
		{"relative path with dot-slash", "./foo/bar.go"},
		{"absolute path", "/abs/path.go"},
		{"nested internal path", "internal/template/render.go"},
		{"json config", "config/settings.json"},
		{"yaml in subdirectory", "deploy/k8s/pod.yaml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !looksLikeFilePath(tc.input) {
				t.Errorf("looksLikeFilePath(%q) = false, want true", tc.input)
			}
		})
	}
}

func TestLooksLikeFilePath_Negative(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"plain word", "hello"},
		{"empty string", ""},
		{"too short (2 chars)", "ab"},
		{"https URL", "https://example.com/foo.js"},
		{"http URL", "http://example.com/path.go"},
		{"ftp URL", "ftp://files.example.com/archive.tar.gz"},
		{"version string", "1.2.3"},
		{"semver with v prefix", "v1.2.3"},
		{"no slash", "filename.go"},
		{"no dot", "path/without/extension"},
		{"too long (>200 chars)", func() string {
			s := "/"
			for i := 0; i < 200; i++ {
				s += "x"
			}
			return s + ".go"
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if looksLikeFilePath(tc.input) {
				t.Errorf("looksLikeFilePath(%q) = true, want false", tc.input)
			}
		})
	}
}
