package oauth

// ServerMetadata is RFC 8414's authorization server metadata.
type ServerMetadata struct {
	Issuer                                 string   `json:"issuer"`
	AuthorizationEndpoint                  string   `json:"authorization_endpoint"`
	TokenEndpoint                          string   `json:"token_endpoint"`
	RevocationEndpoint                     string   `json:"revocation_endpoint"`
	RegistrationEndpoint                   string   `json:"registration_endpoint,omitempty"`
	ScopesSupported                        []string `json:"scopes_supported"`
	ResponseTypesSupported                 []string `json:"response_types_supported"`
	ResponseModesSupported                 []string `json:"response_modes_supported"`
	GrantTypesSupported                    []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
	AuthorizationResponseIssParameter      bool     `json:"authorization_response_iss_parameter_supported"`
	ServiceDocumentation                   string   `json:"service_documentation"`
}

// ResourceMetadata is RFC 9728's protected resource metadata, for MCP clients.
type ResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// The paths partners read from the metadata rather than build themselves.
const (
	AuthorizePath = "/oauth/authorize"
	TokenPath     = "/api/v1/oauth/token"
	RevokePath    = "/api/v1/oauth/revoke"
	RegisterPath  = "/api/v1/oauth/register"
	// ServerMetadataPath and ResourceMetadataPath are the well-known documents.
	ServerMetadataPath   = "/.well-known/oauth-authorization-server"
	ResourceMetadataPath = "/.well-known/oauth-protected-resource"
)

// Server is the authorization server's metadata.
func (s *Service) Server() ServerMetadata {
	methods := []string{AuthBasic, AuthPost, AuthNone}
	m := ServerMetadata{
		Issuer:                                 s.issuer,
		AuthorizationEndpoint:                  s.issuer + AuthorizePath,
		TokenEndpoint:                          s.issuer + TokenPath,
		RevocationEndpoint:                     s.issuer + RevokePath,
		ScopesSupported:                        Scopes,
		ResponseTypesSupported:                 []string{"code"},
		ResponseModesSupported:                 []string{"query"},
		GrantTypesSupported:                    grantTypes,
		CodeChallengeMethodsSupported:          []string{"S256"},
		TokenEndpointAuthMethodsSupported:      methods,
		RevocationEndpointAuthMethodsSupported: methods,
		AuthorizationResponseIssParameter:      true,
		ServiceDocumentation:                   s.issuer + "/api/v1/openapi.json",
	}
	if s.RegistrationOpen() {
		m.RegistrationEndpoint = s.issuer + RegisterPath
	}
	return m
}

// Resource is the API's metadata as a protected resource.
func (s *Service) Resource() ResourceMetadata {
	return ResourceMetadata{
		Resource:               s.issuer + "/api/v1",
		AuthorizationServers:   []string{s.issuer},
		ScopesSupported:        ResourceScopes,
		BearerMethodsSupported: []string{"header"},
	}
}
