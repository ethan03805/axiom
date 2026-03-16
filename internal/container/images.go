package container

// LanguageProfile defines language-specific dependency and validation strategies
// for hermetic validation sandboxes. Each profile ensures dependencies are
// available without network access.
//
// See Architecture Section 13.5 for the full specification.
type LanguageProfile struct {
	Name             string   // "go", "node", "python", "rust"
	Image            string   // Docker image for this language
	DependencyCmd    []string // Command to install dependencies from lockfile/cache
	CompileCmd       []string // Compilation command
	LintCmd          []string // Linting command
	TestCmd          []string // Unit test command
	SecurityScanCmd  []string // Optional security scanning command
	DepCacheMounts   []string // Read-only dependency cache paths to mount
}

// Predefined language profiles per Architecture Section 13.5.
var (
	// GoProfile uses vendored modules or a read-only GOMODCACHE.
	// No network access needed when modules are vendored.
	GoProfile = LanguageProfile{
		Name:            "go",
		Image:           "axiom-meeseeks-go:latest",
		DependencyCmd:   []string{"go", "mod", "download"},
		CompileCmd:      []string{"go", "build", "./..."},
		LintCmd:         []string{"golangci-lint", "run", "./..."},
		TestCmd:         []string{"go", "test", "./..."},
		SecurityScanCmd: []string{"gosec", "./..."},
		DepCacheMounts:  []string{"/go/pkg/mod"},
	}

	// NodeProfile uses npm ci --ignore-scripts with a read-only node_modules cache.
	// Scripts are disabled to prevent supply-chain attacks.
	NodeProfile = LanguageProfile{
		Name:            "node",
		Image:           "axiom-meeseeks-node:latest",
		DependencyCmd:   []string{"npm", "ci", "--ignore-scripts"},
		CompileCmd:      []string{"npx", "tsc", "--noEmit"},
		LintCmd:         []string{"npx", "eslint", "."},
		TestCmd:         []string{"npm", "test"},
		SecurityScanCmd: []string{"npx", "audit", "--audit-level=high"},
		DepCacheMounts:  []string{"/root/.npm"},
	}

	// PythonProfile uses pre-built wheels from a read-only cache.
	// No PyPI access needed.
	PythonProfile = LanguageProfile{
		Name:            "python",
		Image:           "axiom-meeseeks-python:latest",
		DependencyCmd:   []string{"pip", "install", "--no-index", "--find-links", "/cache/wheels", "-r", "requirements.txt"},
		CompileCmd:      []string{"python", "-m", "py_compile"}, // per-file, handled by runner
		LintCmd:         []string{"ruff", "check", "."},
		TestCmd:         []string{"python", "-m", "pytest"},
		SecurityScanCmd: []string{"bandit", "-r", "."},
		DepCacheMounts:  []string{"/cache/wheels"},
	}

	// RustProfile uses cargo with a pre-populated registry and crate cache.
	RustProfile = LanguageProfile{
		Name:            "rust",
		Image:           "axiom-meeseeks-rust:latest",
		DependencyCmd:   []string{"cargo", "fetch"},
		CompileCmd:      []string{"cargo", "build"},
		LintCmd:         []string{"cargo", "clippy", "--", "-D", "warnings"},
		TestCmd:         []string{"cargo", "test"},
		SecurityScanCmd: []string{"cargo", "audit"},
		DepCacheMounts:  []string{"/usr/local/cargo/registry"},
	}

	// MultiProfile is the default for multi-language projects.
	// Uses the multi image and attempts Go commands first.
	MultiProfile = LanguageProfile{
		Name:           "multi",
		Image:          "axiom-meeseeks-multi:latest",
		DependencyCmd:  nil, // Determined at runtime based on detected languages
		CompileCmd:     nil,
		LintCmd:        nil,
		TestCmd:        nil,
		DepCacheMounts: nil,
	}
)

// ProfileRegistry maps language names to their profiles.
var ProfileRegistry = map[string]*LanguageProfile{
	"go":     &GoProfile,
	"node":   &NodeProfile,
	"python": &PythonProfile,
	"rust":   &RustProfile,
	"multi":  &MultiProfile,
}

// DetectLanguage attempts to determine the project language from the
// configured Docker image name.
func DetectLanguage(imageName string) string {
	switch {
	case contains(imageName, "go"):
		return "go"
	case contains(imageName, "node"):
		return "node"
	case contains(imageName, "python"):
		return "python"
	case contains(imageName, "rust"):
		return "rust"
	default:
		return "multi"
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
