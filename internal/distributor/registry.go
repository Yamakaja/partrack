package distributor

// Registry maps api_type strings to Client instances.
type Registry struct {
	clients map[string]Client
}

// NewRegistry returns an empty Registry.
func NewRegistry() Registry {
	return Registry{clients: make(map[string]Client)}
}

// Register adds a Client for the given api_type.
func (r *Registry) Register(apiType string, c Client) {
	r.clients[apiType] = c
}

// Lookup returns the Client registered for apiType, or nil if none is registered.
func (r Registry) Lookup(apiType string) Client {
	return r.clients[apiType]
}
