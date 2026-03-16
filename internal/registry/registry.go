package registry

// ModelInfo holds metadata about a registered model.
type ModelInfo struct {
	ID              string
	Family          string
	Provider        string
	InputCostPer1K  float64
	OutputCostPer1K float64
	MaxContext       int
	Capabilities    []string
	IsLocal         bool
}

// Registry maintains the catalog of available AI models and their capabilities.
type Registry struct {
	models map[string]*ModelInfo
}

// New creates a new model Registry.
func New() *Registry {
	return &Registry{
		models: make(map[string]*ModelInfo),
	}
}

// Register adds a model to the registry.
func (r *Registry) Register(info *ModelInfo) {
	r.models[info.ID] = info
}

// Get retrieves a model by ID.
func (r *Registry) Get(id string) (*ModelInfo, bool) {
	info, ok := r.models[id]
	return info, ok
}

// List returns all registered models.
func (r *Registry) List() []*ModelInfo {
	result := make([]*ModelInfo, 0, len(r.models))
	for _, m := range r.models {
		result = append(result, m)
	}
	return result
}

// FindByCapability returns models that support the given capability.
func (r *Registry) FindByCapability(cap string) []*ModelInfo {
	var result []*ModelInfo
	for _, m := range r.models {
		for _, c := range m.Capabilities {
			if c == cap {
				result = append(result, m)
				break
			}
		}
	}
	return result
}

// CostEstimate estimates the cost for a given number of tokens.
func (r *Registry) CostEstimate(modelID string, inputTokens, outputTokens int) (float64, error) {
	return 0, nil
}
