package httpapi

import (
	"slices"

	"github.com/armature/armature/backend/internal/events"
)

// capability is one integration feature a partner checks for by name at /meta,
// with the operations and topics it promises; docs/integration.md lists them.
type capability struct {
	name, promise string
	// operations are "METHOD /path" as the operations table spells them.
	operations []string
	topics     []string
}

// integrationCapabilities may only use names from Dynamo's shared list.
var integrationCapabilities = []capability{
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
