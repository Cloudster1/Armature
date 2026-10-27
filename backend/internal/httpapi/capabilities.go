package httpapi

import (
	"slices"

	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/oauth"
)

// capability is one integration feature a partner checks for by name at /meta,
// with the operations and topics it promises; docs/integration.md lists them.
type capability struct {
	name, promise string
	// operations are "METHOD /path" as the operations table spells them.
	operations []string
	topics     []string
	// grants are token endpoint grant types, as the server metadata lists them.
	grants []string
}

// integrationCapabilities may only use names from Dynamo's shared list.
var integrationCapabilities = []capability{
	{
		name:    "auth.silent-sign-in@1",
		promise: "A visit without a session first asks the installation's identity provider quietly, so somebody signed in to another tool of the suite is let straight in, and signing out ends the provider's session too.",
		operations: []string{
			"GET /auth/oidc/start",
			"GET /auth/oidc/callback",
			"POST /auth/logout",
		},
	},
	{
		name:    "issue-embed@1",
		promise: "An issue read by its key, and issues found by NQL or by the words typed so far, with a session or a token.",
		operations: []string{
			"GET /issues/{issueKey}",
			"GET /issues",
			"GET /issues/suggest",
		},
	},
	{
		name:    "remote-links@1",
		promise: "The pages elsewhere that are about an issue: listed, put on by url, retitled by putting the same url again, taken off, and announced as topics.",
		operations: []string{
			"GET /issues/{issueKey}/remote-links",
			"POST /issues/{issueKey}/remote-links",
			"DELETE /issues/{issueKey}/remote-links/{remoteLinkID}",
		},
		topics: []string{events.TopicRemoteLinkAdded, events.TopicRemoteLinkRemoved},
	},
	{
		name:    "remote-link-relations@1",
		promise: "A remote link says whether it is a page or a requirement, on the link, the request that puts it and both topics; one sent without it is a page.",
		operations: []string{
			"GET /issues/{issueKey}/remote-links",
			"POST /issues/{issueKey}/remote-links",
		},
		topics: []string{events.TopicRemoteLinkAdded, events.TopicRemoteLinkRemoved},
	},
	{
		name:    "issue.access-requests@1",
		promise: "A reader asks for access to an issue with their own token and is answered 202 alike whether it exists or not; the project's administrators are told.",
		operations: []string{
			"POST /issues/{issueKey}/access-requests",
		},
	},
	{
		name:    "webhooks@1",
		promise: "Endpoints subscribed to topics, their deliveries, a redelivery, a test delivery and a new secret, each delivery a signed envelope.",
		operations: []string{
			"GET /webhooks",
			"POST /webhooks",
			"PATCH /webhooks/{endpointID}",
			"DELETE /webhooks/{endpointID}",
			"GET /webhooks/{endpointID}/deliveries",
			"POST /webhooks/{endpointID}/deliveries/{deliveryID}/redeliver",
			"POST /webhooks/{endpointID}/test",
			"POST /webhooks/{endpointID}/rotate-secret",
		},
	},
	{
		name:    "webhooks@2",
		promise: "An endpoint registered for envelope version 2 is posted each event as a CloudEvents 1.0 event in structured mode, the version 1 payload as its data, signed as version 1 is.",
		operations: []string{
			"POST /webhooks",
			"PATCH /webhooks/{endpointID}",
			"POST /webhooks/{endpointID}/test",
		},
	},
	{
		// The tools are discovered from their list, so only the endpoint is promised, as Exciter's mcp@1 does.
		name:    "mcp@1",
		promise: "A partner works with issues over MCP 2025-06-18 as the person whose token it holds, with the tools its list names.",
		operations: []string{
			"POST /mcp",
		},
	},
	{
		name:    "git-proxy@1",
		promise: "A project's repository cloned and fetched through Armature with a token holding read, and a branch made for an issue pushed with git.write, never the default branch, a tag, a deletion or a force push; the host's token never leaves Armature.",
		operations: []string{
			"GET /git/{projectKey}/{owner}/{name}/info/refs",
			"POST /git/{projectKey}/{owner}/{name}/git-upload-pack",
			"POST /git/{projectKey}/{owner}/{name}/git-receive-pack",
		},
	},
	{
		name:    "oauth.partner-tokens@1",
		promise: "A connected app gets a person's token with their consent, by authorization code with PKCE, refreshes it, revokes it, and registers itself pending an administrator.",
		operations: []string{
			"POST /oauth/register",
			"POST /oauth/token",
			"POST /oauth/revoke",
			"GET /oauth/consent",
			"POST /oauth/consent",
			"GET " + oauth.ServerMetadataPath,
			"GET " + oauth.ResourceMetadataPath,
		},
	},
	{
		name:    "oauth.token-exchange@1",
		promise: "A connected app trades a person's access token from the installation's identity provider, its azp the app's Keycloak client and its aud Armature's, for an access and a refresh token within the person's rights, never webhooks.partner.",
		operations: []string{
			"POST /oauth/token",
		},
		grants: []string{oauth.GrantTokenExchange},
	},
	{
		name:    "webhooks.partner@1",
		promise: "A connected app an administrator allowed makes, changes, tests, rotates and removes its own webhook endpoint.",
		operations: []string{
			"GET /webhooks/partner",
			"POST /webhooks/partner",
			"PATCH /webhooks/partner",
			"DELETE /webhooks/partner",
			"POST /webhooks/partner/rotate-secret",
			"POST /webhooks/partner/test",
		},
	},
	{
		name:    "theme-export@1",
		promise: "A theme exported as one file with its pictures and fonts, and such a file imported as a theme of the caller's own.",
		operations: []string{
			"GET /themes/{themeID}/export",
			"POST /themes/import",
		},
	},
	{
		name:    "reports@1",
		promise: "The report kinds a project has, one report computed now, and a dashboard shared by a link read without an account.",
		operations: []string{
			"GET /projects/{projectKey}/report-kinds",
			"GET /projects/{projectKey}/reports/{kind}",
			"GET /shared/{token}",
			"GET /shared/{token}/widgets/{widgetID}",
		},
	},
	{
		name:    "plan@1",
		promise: "A project laid out against a calendar, its people's hours against the work, and the share of each person's week it has, read and set.",
		operations: []string{
			"GET /projects/{projectKey}/plan",
			"GET /projects/{projectKey}/resources",
			"GET /projects/{projectKey}/allocations",
			"PUT /projects/{projectKey}/allocations/{userID}",
		},
	},
	{
		name:    "calendar@1",
		promise: "Everything a project has dated in a month: issues, sprints, milestones, versions, holidays and who is away.",
		operations: []string{
			"GET /projects/{projectKey}/calendar",
		},
	},
}

// capabilities is what this build announces beyond its API version; a client
// checks for a name here rather than comparing versions.
func capabilities() []string {
	names := make([]string, 0, len(integrationCapabilities))
	for _, c := range integrationCapabilities {
		names = append(names, c.name)
	}
	slices.Sort(names)
	return names
}
