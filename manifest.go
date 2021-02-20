package pass

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsimple"
)

var ErrDuplicateUpstreamIdentifier = fmt.Errorf("duplicate upstream identifier")

// Manifest is a list of upstream services in which to proxy.
type Manifest struct {
	Annotations map[string]string `hcl:"annotations,optional"` // Annotations to be used by other libraries
	Upstreams   []Upstream        `hcl:"upstream,block"`       // Upstream route configurations
	PrefixPath  string            `hcl:"prefix_path,optional"` // Prefix to add to all upstream routes. Stripped when proxying.

	upstreamIndex map[string]*Upstream // Index to lookup Upstream by identifier
}

// Upstream is an upstream service in which to proxy.
type Upstream struct {
	Identifier      string            `hcl:",label"`                     // Human identifier for the upstream
	Annotations     map[string]string `hcl:"annotations,optional"`       // Annotations to be used by other libraries
	Destination     string            `hcl:"destination"`                // Scheme and Hostname of the upstream component
	Routes          []Route           `hcl:"route,block"`                // Routes to accept
	FlushIntervalMS int               `hcl:"flush_interval_ms,optional"` // httputil.ReverseProxy.FlushInterval value in milliseconds
	Owner           string            `hcl:"owner,optional"`             // Team that owns the upstream component
	PrefixPath      string            `hcl:"prefix_path,optional"`       // Prefix to add to all routes. Stripped when proxying.
}

// Route is an individual HTTP method/path combination in which to proxy.
type Route struct {
	Methods []string `hcl:"methods"` // HTTP Methods
	Path    string   `hcl:"path"`    // HTTP Path
}

// LoadManifest parses an HCL file containing the manifest.
func LoadManifest(filename string, ectx *hcl.EvalContext) (*Manifest, error) {
	var m Manifest
	err := hclsimple.DecodeFile(filename, ectx, &m)
	if err != nil {
		return nil, err
	}

	// Establish defaults for annotation maps so the caller can simply ask about
	// keys without caring about nil values.
	if m.Annotations == nil {
		m.Annotations = map[string]string{}
	}
	for i := range m.Upstreams {
		if m.Upstreams[i].Annotations == nil {
			m.Upstreams[i].Annotations = map[string]string{}
		}
	}

	// Validate uniqueness of upstream identifiers
	upstreams := map[string]*Upstream{}
	for _, u := range m.Upstreams {
		id := u.Identifier
		_, ok := upstreams[id]
		if !ok {
			upstreams[id] = &u
			continue
		}
		return nil, fmt.Errorf("%w: %q", ErrDuplicateUpstreamIdentifier, id)
	}
	m.upstreamIndex = upstreams

	return &m, nil
}
