package update

import (
	"testing"
)

func TestClassifyPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		wantMethod  InstallMethod
		wantManager ExternalManager
	}{
		{
			name:       "homebrew cask on apple silicon",
			path:       "/opt/homebrew/Caskroom/lstk/0.3.0/lstk",
			wantMethod: InstallHomebrew,
		},
		{
			name:       "homebrew cask on intel mac",
			path:       "/usr/local/Caskroom/lstk/0.3.0/lstk",
			wantMethod: InstallHomebrew,
		},
		{
			// An npm lstk under a mise-managed *node* is still an npm install and
			// must keep updating through npm (see 273738e).
			name:       "npm global install under mise-managed node",
			path:       "/Users/someone/.local/share/mise/installs/node/24.8.0/lib/node_modules/@localstack/lstk_darwin_arm64/lstk",
			wantMethod: InstallNPM,
		},
		{
			name:       "npm global install default prefix",
			path:       "/usr/local/lib/node_modules/@localstack/lstk_darwin_amd64/lstk",
			wantMethod: InstallNPM,
		},
		{
			name:       "npm global install via asdf",
			path:       "/Users/someone/.asdf/installs/nodejs/22.12.0/lib/node_modules/@localstack/lstk_darwin_arm64/lstk",
			wantMethod: InstallNPM,
		},
		{
			name:       "npm global install on windows",
			path:       `C:\Users\me\AppData\Roaming\npm\node_modules\@localstack\lstk_windows_amd64\lstk.exe`,
			wantMethod: InstallNPM,
		},
		{
			name:        "nix store",
			path:        "/nix/store/9k1qz3lstk-lstk-1.2.3/bin/lstk",
			wantMethod:  InstallExternal,
			wantManager: ManagerNix,
		},
		{
			name:        "nix profile",
			path:        "/Users/me/.nix-profile/bin/lstk",
			wantMethod:  InstallExternal,
			wantManager: ManagerNix,
		},
		// A manager's name alone is not rare enough: a checkout of the tool itself
		// would otherwise get a refusal advising a command that does not apply.
		{
			name:       "directory merely named nix is not a nix install",
			path:       "/home/user/projects/nix/bin/lstk",
			wantMethod: InstallBinary,
		},
		{
			name:       "directory merely named mise is not a mise install",
			path:       "/home/user/projects/mise/target/release/lstk",
			wantMethod: InstallBinary,
		},
		{
			name:       "directory merely named scoop is not a scoop install",
			path:       "/home/user/projects/scoop/bin/lstk",
			wantMethod: InstallBinary,
		},
		{
			name:        "mise shim",
			path:        "/Users/me/.local/share/mise/shims/lstk",
			wantMethod:  InstallExternal,
			wantManager: ManagerMise,
		},
		{
			name:        "scoop shim",
			path:        `C:\Users\me\scoop\shims\lstk.exe`,
			wantMethod:  InstallExternal,
			wantManager: ManagerScoop,
		},
		{
			name:        "mise managed lstk",
			path:        "/Users/me/.local/share/mise/installs/lstk/1.2.3/lstk",
			wantMethod:  InstallExternal,
			wantManager: ManagerMise,
		},
		{
			name:        "asdf managed lstk",
			path:        "/Users/me/.asdf/installs/lstk/1.2.3/bin/lstk",
			wantMethod:  InstallExternal,
			wantManager: ManagerASDF,
		},
		{
			name:        "scoop managed lstk",
			path:        `C:\Users\me\scoop\apps\lstk\current\lstk.exe`,
			wantMethod:  InstallExternal,
			wantManager: ManagerScoop,
		},
		{
			name:        "chocolatey managed lstk",
			path:        `C:\ProgramData\chocolatey\lib\lstk\tools\lstk.exe`,
			wantMethod:  InstallExternal,
			wantManager: ManagerChocolatey,
		},
		{
			name:       "standalone binary in usr local bin",
			path:       "/usr/local/bin/lstk",
			wantMethod: InstallBinary,
		},
		{
			name:       "standalone binary in home dir",
			path:       "/home/user/bin/lstk",
			wantMethod: InstallBinary,
		},
		{
			name:       "dev build",
			path:       "/home/user/Projects/lstk/bin/lstk",
			wantMethod: InstallBinary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyPath(tt.path)
			if got.Method != tt.wantMethod {
				t.Fatalf("classifyPath(%q).Method = %v, want %v", tt.path, got.Method, tt.wantMethod)
			}
			if got.Manager != tt.wantManager {
				t.Fatalf("classifyPath(%q).Manager = %q, want %q", tt.path, got.Manager, tt.wantManager)
			}
			if got.ResolvedPath != tt.path {
				t.Fatalf("classifyPath(%q).ResolvedPath = %q, want the input path", tt.path, got.ResolvedPath)
			}
			if got.ExternallyManaged() != (tt.wantMethod == InstallExternal) {
				t.Fatalf("classifyPath(%q).ExternallyManaged() = %v, want %v", tt.path, got.ExternallyManaged(), tt.wantMethod == InstallExternal)
			}
		})
	}
}

// TestExternalManagerHints pins wording printed verbatim in the notify line and
// the `lstk update` refusal, so a change here changes the CLI's output.
func TestExternalManagerHints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		manager        ExternalManager
		displayName    string
		upgradeCommand string
		upgradeAdvice  string
	}{
		{ManagerMise, "mise", "mise upgrade lstk", "run mise upgrade lstk"},
		{ManagerScoop, "Scoop", "scoop update lstk", "run scoop update lstk"},
		{ManagerChocolatey, "Chocolatey", "choco upgrade lstk", "run choco upgrade lstk"},
		// Nix splits across profile / nixos-rebuild / home-manager and asdf has no
		// upgrade verb, so neither names a command.
		{ManagerNix, "Nix", "", "update it with Nix"},
		{ManagerASDF, "asdf", "", "update it with asdf"},
	}

	for _, tt := range tests {
		t.Run(string(tt.manager), func(t *testing.T) {
			t.Parallel()
			if got := tt.manager.DisplayName(); got != tt.displayName {
				t.Errorf("DisplayName() = %q, want %q", got, tt.displayName)
			}
			if got := tt.manager.UpgradeCommand(); got != tt.upgradeCommand {
				t.Errorf("UpgradeCommand() = %q, want %q", got, tt.upgradeCommand)
			}
			if got := tt.manager.UpgradeAdvice(); got != tt.upgradeAdvice {
				t.Errorf("UpgradeAdvice() = %q, want %q", got, tt.upgradeAdvice)
			}
		})
	}
}

func TestInstallMethodString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method InstallMethod
		want   string
	}{
		{InstallBinary, "binary"},
		{InstallHomebrew, "homebrew"},
		{InstallNPM, "npm"},
		{InstallExternal, "external"},
	}

	for _, tt := range tests {
		if got := tt.method.String(); got != tt.want {
			t.Errorf("InstallMethod(%d).String() = %q, want %q", tt.method, got, tt.want)
		}
	}
}
