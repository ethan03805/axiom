package skill

// Skill represents a reusable capability that can be assigned to agents.
type Skill struct {
	Name        string
	Description string
	Prompt      string
	Tools       []string
	ModelHint   string
}

// Library manages a collection of available skills.
type Library struct {
	skills map[string]*Skill
}

// New creates a new skill Library.
func New() *Library {
	return &Library{
		skills: make(map[string]*Skill),
	}
}

// Register adds a skill to the library.
func (l *Library) Register(skill *Skill) {
	l.skills[skill.Name] = skill
}

// Get retrieves a skill by name.
func (l *Library) Get(name string) (*Skill, bool) {
	s, ok := l.skills[name]
	return s, ok
}

// List returns all registered skills.
func (l *Library) List() []*Skill {
	result := make([]*Skill, 0, len(l.skills))
	for _, s := range l.skills {
		result = append(result, s)
	}
	return result
}

// LoadFromDir loads skill definitions from a directory of YAML/TOML files.
func (l *Library) LoadFromDir(dir string) error {
	return nil
}
