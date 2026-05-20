package platform

import (
	"fmt"
	"strings"

	aimconfig "bitbucket.srv.westpac.com.au/m055731/aim/internal/config"
)

// Endpoint holds the URLs for a specific DC/Env combination.
type Endpoint struct {
	DC          string // rcc, wsdc
	Env         string // edev, npr
	MaaSGateway string // MaaS API base URL
	OCPAPI      string // OpenShift API server (port 6443, may be blocked by Zscaler)
	OAuthBase   string // OAuth server via .apps route (port 443, always reachable)
}

// Key returns the canonical "dc/env" identifier.
func (e Endpoint) Key() string {
	return e.DC + "/" + e.Env
}

// OAuthAuthorizeURL returns the OAuth authorization endpoint.
func (e Endpoint) OAuthAuthorizeURL() string {
	return e.OAuthBase + "/oauth/authorize"
}

// OAuthTokenURL returns the OAuth token exchange endpoint.
func (e Endpoint) OAuthTokenURL() string {
	return e.OAuthBase + "/oauth/token"
}

// Endpoints is the bundled registry of known AIM platform endpoints.
var Endpoints = []Endpoint{
	{
		DC:          "rcc",
		Env:         "edev",
		MaaSGateway: "https://api.ai.rcc.edev.ocp.srv.westpac.com.au",
		OCPAPI:      "https://api.ocp-rcc-edev-isd-100.edev.ocp.srv.westpac.com.au:6443",
		OAuthBase:   "https://oauth-clusters-ocp-rcc-edev-isd-100.apps.ocp-rcc-edev-isd-000.edev.ocp.srv.westpac.com.au",
	},
	{
		DC:          "rcc",
		Env:         "npr",
		MaaSGateway: "https://api.ai.rcc.npr.ocp.srv.westpac.com.au",
		OCPAPI:      "https://api.ocp-rcc-npr-isd-100.npr.ocp.srv.westpac.com.au:6443",
		OAuthBase:   "https://oauth-clusters-ocp-rcc-npr-isd-100.apps.ocp-rcc-npr-isd-000.npr.ocp.srv.westpac.com.au",
	},
	{
		DC:          "wsdc",
		Env:         "edev",
		MaaSGateway: "https://api.ai.wsdc.edev.ocp.srv.westpac.com.au",
		OCPAPI:      "https://api.ocp-wsdc-edev-isd-100.edev.ocp.srv.westpac.com.au:6443",
		OAuthBase:   "https://oauth-clusters-ocp-wsdc-edev-isd-100.apps.ocp-wsdc-edev-isd-000.edev.ocp.srv.westpac.com.au",
	},
	{
		DC:          "wsdc",
		Env:         "npr",
		MaaSGateway: "https://api.ai.wsdc.npr.ocp.srv.westpac.com.au",
		OCPAPI:      "https://api.ocp-wsdc-npr-isd-100.npr.ocp.srv.westpac.com.au:6443",
		OAuthBase:   "https://oauth-clusters-ocp-wsdc-npr-isd-100.apps.ocp-wsdc-npr-isd-000.npr.ocp.srv.westpac.com.au",
	},
}

// LookupEndpoint resolves a "dc/env" string to an Endpoint.
func LookupEndpoint(key string) (Endpoint, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, endpoint := range Endpoints {
		if endpoint.Key() == key {
			return endpoint, nil
		}
	}

	keys := make([]string, len(Endpoints))
	for i, endpoint := range Endpoints {
		keys[i] = endpoint.Key()
	}

	return Endpoint{}, fmt.Errorf("unknown endpoint %q (available: %s)", key, strings.Join(keys, ", "))
}

// ResolveEndpoint determines the endpoint from CLI flags or config.
func ResolveEndpoint(endpointFlag string) (Endpoint, error) {
	key := endpointFlag
	if key == "" {
		key = aimconfig.LoadConfig().DefaultEndpoint
	}
	if key == "" {
		key = "rcc/npr"
	}
	return LookupEndpoint(key)
}
