package index

// FileEntry represents a single file in the project index.
type FileEntry struct {
	Path     string
	Language string
	SHA256   string
	Size     int64
	Symbols  []Symbol
}

// Symbol represents a code symbol (function, type, variable, etc.).
type Symbol struct {
	Name       string
	Kind       string
	StartLine  int
	EndLine    int
	Signature  string
	ParentName string
}

// DependencyEdge represents a dependency between two files.
type DependencyEdge struct {
	From string
	To   string
	Kind string
}

// Index maintains a searchable index of the project's code structure.
type Index struct {
	files map[string]*FileEntry
	deps  []DependencyEdge
}

// New creates a new Index.
func New() *Index {
	return &Index{
		files: make(map[string]*FileEntry),
	}
}

// Build scans the project directory and builds the index.
func (idx *Index) Build(rootDir string) error {
	return nil
}

// Update incrementally updates the index for changed files.
func (idx *Index) Update(changedPaths []string) error {
	return nil
}

// Lookup returns the file entry for a given path.
func (idx *Index) Lookup(path string) (*FileEntry, bool) {
	entry, ok := idx.files[path]
	return entry, ok
}

// FindSymbol searches for symbols matching the given name pattern.
func (idx *Index) FindSymbol(name string) []Symbol {
	return nil
}

// Dependencies returns the dependency edges for a given file.
func (idx *Index) Dependencies(path string) []DependencyEdge {
	return nil
}

// Dependents returns files that depend on the given file.
func (idx *Index) Dependents(path string) []string {
	return nil
}

// AllFiles returns all indexed file paths.
func (idx *Index) AllFiles() []string {
	result := make([]string, 0, len(idx.files))
	for path := range idx.files {
		result = append(result, path)
	}
	return result
}
