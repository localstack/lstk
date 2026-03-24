package update

import (
	"testing"
)

func TestClassifyPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want InstallMethod
	}{
		{
			name: "homebrew cask on apple silicon",
			path: "/opt/homebrew/Caskroom/lstk/0.3.0/lstk",
			want: InstallHomebrew,
		},
		{
			name: "homebrew cask on intel mac",
			path: "/usr/local/Caskroom/lstk/0.3.0/lstk",
			want: InstallHomebrew,
		},
		{
			name: "npm global install",
			path: "/Users/someone/.local/share/mise/installs/node/24.8.0/lib/node_modules/@localstack/lstk_darwin_arm64/lstk",
			want: InstallNPM,
		},
		{
			name: "npm global install default prefix",
			path: "/usr/local/lib/node_modules/@localstack/lstk_darwin_amd64/lstk",
			want: InstallNPM,
		},
		{
			name: "npm global install via asdf",
			path: "/Users/geo/.asdf/installs/nodejs/22.12.0/lib/node_modules/@localstack/lstk_darwin_arm64/lstk",
			want: InstallNPM,
		},
		{
			name: "standalone binary in usr local bin",
			path: "/usr/local/bin/lstk",
			want: InstallBinary,
		},
		{
			name: "standalone binary in home dir",
			path: "/home/user/bin/lstk",
			want: InstallBinary,
		},
		{
			name: "dev build",
			path: "/home/user/Projects/lstk/bin/lstk",
			want: InstallBinary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyPath(tt.path)
			if got != tt.want {
				t.Fatalf("classifyPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
