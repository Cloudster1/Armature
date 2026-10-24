package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/armature/armature/backend/internal/buildinfo"
	"github.com/armature/armature/backend/internal/oauth"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/project"
)

// Routes builds the HTTP surface.
//
// The middleware order matters: an id first so every later line can quote it,
// then the logger, then panic recovery inside the logger so a panic still
// produces an access line, then authentication.
func (s *Server) Routes(allowedOrigins []string) http.Handler {
	r := chi.NewRouter()

	r.Use(requestID)
	if s.Telemetry != nil {
		r.Use(observe(s.Telemetry.Metrics))
	}
	r.Use(logging(s.Log))
	r.Use(recovery)
	r.Use(s.clientAddress)
	r.Use(securityHeaders)
	r.Use(cors(allowedOrigins))
	r.Use(s.sameSite(allowedOrigins))
	r.Use(requestTimeout(requestLimit))
	r.Use(s.authenticate)
	r.Use(readOnlyToken)
	r.Use(appScope(r))

	// Liveness and readiness are deliberately outside authentication.
	r.Get("/healthz", s.handleLiveness)
	r.Get("/readyz", s.handleReadiness)

	// Where a partner and an MCP client learn how to get a token.
	r.Get(oauth.ServerMetadataPath, s.handleServerMetadata)
	r.Get(oauth.ResourceMetadataPath, s.handleResourceMetadata)

	r.Route("/api/v1", func(r chi.Router) {
		// The API's own description, derived from the code that serves it.
		r.Get("/openapi.json", s.handleOpenAPI)
		// Which build answers and which API it speaks, for anyone who asks;
		// the commit only for somebody signed in.
		r.Get("/meta", s.handleMeta)

		// Unauthenticated entry points.
		r.Get("/auth/signup", s.handleSignupOpen)
		r.Post("/auth/signup", s.handleSignup)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/login/second-factor", s.handleLoginSecondFactor)
		r.Post("/auth/invites/accept", s.handleAcceptInvite)
		r.Post("/auth/invites/preview", s.handlePreviewInvite)
		r.Post("/auth/password-reset", s.handleRequestPasswordReset)
		r.Post("/auth/password-reset/confirm", s.handleConfirmPasswordReset)

		// Signing in through an identity provider. Both halves are outside
		// authentication: nobody is signed in yet, which is the point.
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		// Until the next release, for addresses that named an organization.
		r.Get("/auth/oidc/{orgSlug}/start", s.handleOIDCStartForOrganization)

		// The portal's door for people without an account: the desk is named
		// by its key, since nobody is signed in to say which one.
		r.Get("/desk", s.handleDeskEntry)
		r.Post("/desk/codes", s.handlePortalCode)
		r.Post("/desk/sessions", s.handlePortalSession)
		// Until the next release, for addresses that named an organization.
		r.Get("/desk/{orgSlug}", s.handleDeskEntryMoved)
		r.Post("/desk/{orgSlug}/codes", s.handlePortalCodeMoved)
		r.Post("/desk/{orgSlug}/sessions", s.handlePortalSessionMoved)
		// The way out of following that a mail carries; the token is the
		// whole of the authorization.
		r.Post("/unwatch", s.handleUnwatch)
		// The rating a resolution mail invites, likewise.
		r.Get("/csat/{token}", s.handleRatingPage)
		r.Post("/csat/{token}", s.handleRate)

		// A shared dashboard. The token names the organization and the
		// dashboard; the visitor reads what the link froze and nothing else.
		r.Get("/shared/{token}", s.handleShared)
		r.Get("/shared/{token}/widgets/{widgetID}", s.handleSharedWidget)
		r.Get("/shared/{token}/pdf", s.handleSharedPDF)

		// A connected app's side of OAuth: no session, the app proves itself.
		// The consent page's two calls come from the person's browser and
		// take a held session to its own answer rather than the generic one.
		r.Post("/oauth/register", s.handleOAuthRegister)
		r.Post("/oauth/token", s.handleOAuthToken)
		r.Post("/oauth/revoke", s.handleOAuthRevoke)
		r.Get("/oauth/consent", s.handleOAuthConsent)
		r.Post("/oauth/consent", s.handleOAuthDecide)

		// What a git host calls. No session: the delivery proves itself with
		// the repository's secret.
		r.Post("/git/webhooks/{repositoryID}", s.handleWebhook)
		// git's smart HTTP, proxied to the host as the token's person. The
		// handlers judge the token themselves and answer as git reads it.
		r.Get("/git/{projectKey}/{owner}/{name}/info/refs", s.handleGitRefs)
		r.Post("/git/{projectKey}/{owner}/{name}/git-upload-pack", s.handleGitUploadPack)
		r.Post("/git/{projectKey}/{owner}/{name}/git-receive-pack", s.handleGitReceivePack)
		// What an outside system posts to start a rule; the address is the key.
		r.Post("/automation/hooks/{token}", s.handleIncomingHook)

		// Anything below here needs a signed-in caller.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			// A session the installation holds to enrolment reaches little else.
			r.Use(requireSecondFactor)

			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			// Which build answers, deprecated for /meta and kept for one more
			// release. Behind the session: a commit hash is nobody else's business.
			r.Get("/build", s.handleBuild)
			// One's own account: what a person may change about themselves.
			r.Patch("/auth/me", s.handleUpdateProfile)
			// A password is changed by the person who has it, never by a token.
			r.With(requireSession).Put("/auth/me/password", s.handleChangePassword)
			// Where one is signed in, and ending the others; a token is no
			// session of its own to keep.
			r.With(requireSession).Get("/auth/sessions", s.handleListSessions)
			r.With(requireSession).Delete("/auth/sessions", s.handleEndOtherSessions)
			r.With(requireSession).Delete("/auth/sessions/{sessionID}", s.handleEndSession)
			r.Post("/auth/me/avatar", s.handleSetAvatar)
			r.Delete("/auth/me/avatar", s.handleRemoveAvatar)
			// A person's data is theirs to take and theirs to have erased.
			r.Get("/auth/me/export", s.handleExportMe)
			r.Delete("/auth/me", s.handleEraseMe)
			s.secondFactorRoutes(r)

			// Below here, the work itself.
			r.Group(func(r chi.Router) {
				// Customers get the portal and nothing else; the check lets
				// portal paths through and refuses the rest for them.
				r.Use(requireAgent)

				// And a project is a wall inside it: what somebody may not read is
				// not there for them.
				r.Use(s.projectScope)
				s.portalRoutes(r)
				s.automationRoutes(r)
				s.searchRoutes(r)
				s.organizationRoutes(r)
				s.projectRoutes(r)
				s.boardRoutes(r)
				s.dashboardRoutes(r)
				s.deskRoutes(r)
				s.issueRoutes(r)
			})
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, ErrNotFound("No such endpoint."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, &APIError{
			Status:  http.StatusMethodNotAllowed,
			Code:    "method_not_allowed",
			Message: "That method is not allowed here.",
		})
	})

	s.handler = r
	return r
}

// requestLimit is how long a request may take, but for a git transfer.
const requestLimit = 30 * time.Second

// requestTimeout bounds every request but a clone's or a push's, which streams
// for as long as git.TransferTimeout allows.
func requestTimeout(d time.Duration) func(http.Handler) http.Handler {
	bounded := middleware.Timeout(d)
	return func(next http.Handler) http.Handler {
		limited := bounded(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if gitTransfer(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}

// handleLiveness answers whether the process is running at all. It touches
// nothing external on purpose: a dependency outage must not get the container
// killed and restarted, which would only make the outage worse.
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"build": buildinfo.Current()})
}

// handleReadiness answers whether the process can serve traffic, and reports
// how the replicas are doing while it is at it.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 3*time.Second)
	defer cancel()

	if err := s.DB.Ping(ctx); err != nil {
		// Nobody has signed in to read this, so it says whether, not why.
		s.Log.Error("not ready", "error", err)
		respondJSON(w, r, http.StatusServiceUnavailable, map[string]any{"status": "unavailable"})
		return
	}

	stats := s.DB.Stats()
	respondJSON(w, r, http.StatusOK, map[string]any{
		"status":  "ok",
		"routing": stats,
	})
}

// cors allows the development SPA on a different port to call the API with
// credentials. An empty allow list disables it entirely, which is the right
// default when the SPA is served from the same origin.
func cors(allowed []string) func(http.Handler) http.Handler {
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		set[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && set[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, traceparent, tracestate")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Max-Age", "600")
				h.Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// portalRoutes registers the portal, and the transports that replay a request through the router.
func (s *Server) portalRoutes(r chi.Router) {
	// The portal: what a customer sees of the desk. Agents may
	// look through it too.
	r.Get("/portal/desks", s.handlePortalDesks)
	r.Post("/portal/requests", s.handlePortalRaise)
	r.Get("/portal/requests", s.handlePortalRequests)
	r.Get("/portal/requests/{issueKey}", s.handlePortalRequest)
	r.Post("/portal/requests/{issueKey}/replies", s.handlePortalReply)
	r.Get("/portal/requests/{issueKey}/watchers", s.handlePortalWatchers)
	r.Post("/portal/requests/{issueKey}/watchers", s.handlePortalFollow)
	r.Delete("/portal/requests/{issueKey}/watchers/{userID}", s.handlePortalUnfollow)
	r.Get("/portal/requests/{issueKey}/attachments", s.handlePortalAttachments)
	r.Post("/portal/requests/{issueKey}/attachments", s.handlePortalAttach)
	r.Get("/portal/attachments/{attachmentID}", s.handlePortalAttachment)
	r.Delete("/portal/attachments/{attachmentID}", s.handlePortalDetach)
	r.Get("/portal/desks/{projectKey}/articles", s.handlePortalArticles)
	r.Get("/portal/articles/{articleID}", s.handlePortalArticle)

	r.Post("/mcp", s.handleMCP)
	r.Get("/assistant", s.handleAssistantStatus)
	r.Post("/assistant/ask", s.handleAsk)
}

// automationRoutes registers rules and where events go out.
func (s *Server) automationRoutes(r chi.Router) {
	// Rules. The catalogue is readable by anyone who can see the
	// editor; a rule is guarded by where it lives, in the handler.
	r.Get("/automation/catalog", s.handleAutomationCatalog)
	r.With(requireProjectPerm(perm.ProjectAdminister)).Get("/projects/{projectKey}/automation/rules", s.handleListProjectRules)
	r.With(requireProjectPerm(perm.ProjectAdminister), s.requireFeature(project.FeatureAutomation)).Post("/projects/{projectKey}/automation/rules", s.handleCreateProjectRule)
	// A connected app's own endpoint, which it manages while the person it
	// acts for administers Armature.
	r.Group(func(r chi.Router) {
		r.Use(requirePartner)
		r.Get("/webhooks/partner", s.handleGetPartnerWebhook)
		r.Post("/webhooks/partner", s.handleCreatePartnerWebhook)
		r.Patch("/webhooks/partner", s.handleUpdatePartnerWebhook)
		r.Delete("/webhooks/partner", s.handleDeletePartnerWebhook)
		r.Post("/webhooks/partner/rotate-secret", s.handleRotatePartnerWebhookSecret)
		r.Post("/webhooks/partner/test", s.handleTestPartnerWebhook)
	})
	r.Group(func(r chi.Router) {
		r.Use(requirePerm(perm.OrgAdminister))
		r.Get("/automation/rules", s.handleListOrgRules)
		r.Post("/automation/rules", s.handleCreateOrgRule)
		// Where events go out. Administering the installation.
		r.Get("/webhooks", s.handleListWebhooks)
		r.Post("/webhooks", s.handleCreateWebhook)
		r.Patch("/webhooks/{endpointID}", s.handleUpdateWebhook)
		r.Delete("/webhooks/{endpointID}", s.handleDeleteWebhook)
		r.Post("/webhooks/{endpointID}/rotate-secret", s.handleRotateWebhookEndpointSecret)
		r.Post("/webhooks/{endpointID}/test", s.handleTestWebhook)
		r.Get("/webhooks/{endpointID}/deliveries", s.handleWebhookDeliveries)
		r.Post("/webhooks/{endpointID}/deliveries/{deliveryID}/redeliver", s.handleRedeliverWebhook)
	})
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.ProjectAdminister))
		r.Get("/automation/rules/{ruleID}", s.handleGetRule)
		r.Patch("/automation/rules/{ruleID}", s.handleUpdateRule)
		r.Delete("/automation/rules/{ruleID}", s.handleDeleteRule)
		r.Get("/automation/rules/{ruleID}/runs", s.handleRuleRuns)
		r.Post("/automation/rules/{ruleID}/run", s.handleRunRule)
	})
}

// searchRoutes registers saved filters, searching across projects, and moving issues in and out.
func (s *Server) searchRoutes(r chi.Router) {
	// Saved filters are the caller's own or shared with them; the
	// handlers keep a filter to its owner for changes.
	r.Get("/filters", s.handleListFilters)
	r.Post("/filters", s.handleCreateFilter)
	r.Get("/filters/{filterID}", s.handleGetFilter)
	r.Patch("/filters/{filterID}", s.handleUpdateFilter)
	r.Delete("/filters/{filterID}", s.handleDeleteFilter)
	r.Put("/filters/{filterID}/star", s.handleStarFilter)
	r.Delete("/filters/{filterID}/star", s.handleUnstarFilter)
	r.Put("/filters/{filterID}/subscription", s.handleSubscribeFilter)
	r.Delete("/filters/{filterID}/subscription", s.handleUnsubscribeFilter)
	r.Get("/filters/{filterID}/issues", s.handleRunFilter)
	// Reading out as a file is reading; the rest write and are
	// guarded per issue or per project inside.
	r.Get("/issues/export", s.handleExportIssues)
	r.Get("/issues/suggest", s.handleSuggest)
	r.Post("/issues/bulk", s.handleBulkEdit)
	// An import writes dates, reporters and statuses of its own, which is the
	// project's record rather than one more issue in it.
	r.With(requireProjectPerm(perm.ProjectAdminister), s.requireFeature(project.FeatureImport)).Post("/projects/{projectKey}/import/preview", s.handleImportPreview)
	r.With(requireProjectPerm(perm.ProjectAdminister), s.requireFeature(project.FeatureImport)).Post("/projects/{projectKey}/import", s.handleImport)
	r.With(requireIssuePerm(perm.IssueWrite)).Post("/issues/{issueKey}/clone", s.handleCloneIssue)
	r.With(requireIssuePerm(perm.IssueWrite)).Post("/issues/{issueKey}/move", s.handleMoveIssue)
}

// projectRoutes registers projects and what they plan with: sprints, milestones, versions, components, teams.
func (s *Server) projectRoutes(r chi.Router) {
	// Projects. Creating and configuring one is an administrative
	// act; reading and working in one is not.
	r.Get("/projects", s.handleListProjects)
	r.Get("/project-templates", s.handleListTemplates)
	r.Get("/projects/key-check", s.handleCheckProjectKey)
	r.Get("/projects/{projectKey}", s.handleGetProject)
	r.Get("/projects/{projectKey}/issues", s.handleListIssues)
	r.With(requireProjectPerm(perm.IssueWrite)).
		Post("/projects/{projectKey}/issues", s.handleCreateIssue)

	// Which workflow each issue type uses here, and whether this
	// project or the organization decided it.
	r.Get("/projects/{projectKey}/workflows", s.handleProjectWorkflows)
	// And the statuses those workflows hold, which a board is made of.
	r.Get("/projects/{projectKey}/statuses", s.handleProjectStatuses)

	// The whole project as a tree, roots first.
	r.Get("/projects/{projectKey}/hierarchy", s.handleProjectHierarchy)

	// And the same tree laid out against a calendar.
	r.Get("/projects/{projectKey}/plan", s.handleGetPlan)
	// What the auto plan would move. It saves nothing, but only someone who
	// could make the moves is shown them.
	r.With(requireProjectPerm(perm.IssueWrite)).
		Post("/projects/{projectKey}/plan/align", s.handleAlignPlan)
	// Moving several of the plan's issues at once, all or none; every one is
	// checked to be the project's, so the project's permission covers them.
	r.With(requireProjectPerm(perm.IssueWrite)).
		Put("/projects/{projectKey}/plan/schedule", s.handleSchedulePlan)
	// And the work set against the hours there are, per team or person.
	r.With(s.requireFeature(project.FeatureResources)).Get("/projects/{projectKey}/resources", s.handleGetResources)
	// What share of each person's week the project has.
	r.Get("/projects/{projectKey}/allocations", s.handleListAllocations)
	r.With(requireProjectPerm(perm.ProjectAdminister)).
		Put("/projects/{projectKey}/allocations/{userID}", s.handleSetAllocation)

	// Sprints. Reading them is ordinary; running them is what a
	// scrum master is for.
	r.Get("/projects/{projectKey}/sprints", s.handleListSprints)
	r.With(requireProjectPerm(perm.SprintManage), s.requireFeature(project.FeatureSprints)).
		Post("/projects/{projectKey}/sprints", s.handleCreateSprint)

	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.SprintManage))
		r.Patch("/sprints/{sprintID}", s.handleUpdateSprint)
		r.Post("/sprints/{sprintID}/start", s.handleStartSprint)
		r.Post("/sprints/{sprintID}/complete", s.handleCompleteSprint)
		r.Delete("/sprints/{sprintID}", s.handleDeleteSprint)
	})

	// Milestones. Reading them is ordinary; setting the targets is
	// the same job as planning the sprints.
	r.Get("/projects/{projectKey}/milestones", s.handleListMilestones)
	r.With(requireProjectPerm(perm.SprintManage), s.requireFeature(project.FeatureMilestones)).
		Post("/projects/{projectKey}/milestones", s.handleCreateMilestone)

	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.SprintManage))
		r.Patch("/milestones/{milestoneID}", s.handleUpdateMilestone)
		r.Post("/milestones/{milestoneID}/close", s.handleCloseMilestone)
		r.Post("/milestones/{milestoneID}/reopen", s.handleReopenMilestone)
		r.Delete("/milestones/{milestoneID}", s.handleDeleteMilestone)
	})

	// Versions: what ships, planned like sprints; components are
	// configuration. Naming either on an issue is editing the issue.
	r.Get("/projects/{projectKey}/versions", s.handleListVersions)
	r.With(requireProjectPerm(perm.SprintManage), s.requireFeature(project.FeatureReleases)).
		Post("/projects/{projectKey}/versions", s.handleCreateVersion)
	r.Get("/versions/{versionID}/notes", s.handleReleaseNotes)
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.SprintManage))
		r.Patch("/versions/{versionID}", s.handleUpdateVersion)
		r.Post("/versions/{versionID}/release", s.handleReleaseVersion)
		r.Post("/versions/{versionID}/unrelease", s.handleUnreleaseVersion)
		r.Post("/versions/{versionID}/archive", s.handleArchiveVersion)
		r.Delete("/versions/{versionID}", s.handleDeleteVersion)
	})
	r.Get("/projects/{projectKey}/components", s.handleListComponents)
	r.With(requireProjectPerm(perm.ProjectAdminister), s.requireFeature(project.FeatureComponents)).
		Post("/projects/{projectKey}/components", s.handleCreateComponent)
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.ProjectAdminister))
		r.Patch("/components/{componentID}", s.handleUpdateComponent)
		r.Delete("/components/{componentID}", s.handleDeleteComponent)
	})
	r.Group(func(r chi.Router) {
		r.Use(requireIssuePerm(perm.IssueWrite))
		r.Put("/issues/{issueKey}/versions", s.handleSetIssueVersions)
		r.Put("/issues/{issueKey}/components", s.handleSetIssueComponents)
	})

	// Teams: the people inside a project who work together. Forming
	// one is the project's own business, like planning a sprint.
	r.Get("/projects/{projectKey}/teams", s.handleListTeams)
	r.Get("/teams/{teamID}", s.handleGetTeam)
	r.With(requireProjectPerm(perm.TeamManage), s.requireFeature(project.FeatureTeams)).
		Post("/projects/{projectKey}/teams", s.handleCreateTeam)

	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.TeamManage))
		r.Patch("/teams/{teamID}", s.handleUpdateTeam)
		r.Delete("/teams/{teamID}", s.handleDeleteTeam)
		r.Post("/teams/{teamID}/members", s.handleAddTeamMember)
		r.Delete("/teams/{teamID}/members/{userID}", s.handleRemoveTeamMember)
	})
}

// boardRoutes registers boards, the project's configuration, and repositories.
func (s *Server) boardRoutes(r chi.Router) {
	// The board. Reading it and dragging cards on it is ordinary
	// work; changing which states make up a swimlane is
	// configuration, and sits behind the admin guard below.
	//
	// A project may have several boards. The singular endpoint is
	// the one it opens on; the rest are addressed by id.
	r.Get("/projects/{projectKey}/board", s.handleGetBoard)
	r.With(requireProjectPerm(perm.IssueTransition)).
		Post("/projects/{projectKey}/board/move", s.handleMoveCard)
	r.Get("/projects/{projectKey}/boards", s.handleListBoards)
	r.Get("/boards/{boardID}", s.handleGetBoardByID)
	r.Get("/boards/{boardID}/backlog", s.handleGetBacklog)
	// Every sprint can be tracked on a board, not only the running one.
	r.Get("/sprints/{sprintID}/board", s.handleGetSprintBoard)

	// Making a project is a tenant level act; configuring one is a
	// project level act, so they are guarded differently.
	r.With(requirePerm(perm.ProjectCreate)).
		Post("/projects", s.handleCreateProject)

	r.Group(func(r chi.Router) {
		r.Use(requireProjectPerm(perm.ProjectAdminister))
		r.Patch("/projects/{projectKey}", s.handleUpdateProject)
		// Archiving takes a project away from everybody in it, which is the
		// one thing under project administration a key may not do.
		r.With(requireSession).Delete("/projects/{projectKey}", s.handleArchiveProject)
		r.Post("/projects/{projectKey}/restore", s.handleRestoreProject)

		// How this project draws an issue of one type, or handing that back
		// to the organization.
		r.Put("/projects/{projectKey}/issue-arrangement", s.handleSetProjectArrangement)
		// Overriding the organization's workflows for one project,
		// or handing the decision back to it.
		r.Put("/projects/{projectKey}/workflow-scheme", s.handleSetProjectScheme)
		r.Put("/projects/{projectKey}/workflow-assignments/{issueTypeID}", s.handleSetProjectAssignment)

		r.Post("/projects/{projectKey}/boards", s.handleCreateBoard)

		// Connecting a repository is configuring the project.
		r.With(s.requireFeature(project.FeatureRepositories)).Post("/projects/{projectKey}/repositories", s.handleConnectRepository)
	})

	// A board addressed by id carries no project key in the path,
	// so these are guarded on holding the permission somewhere and
	// then checked against the board's own project in the handler.
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.BoardConfigure))
		r.Patch("/boards/{boardID}", s.handleUpdateBoardByID)
		r.Delete("/boards/{boardID}", s.handleDeleteBoard)
		// A project has several boards, so its lanes are addressed by
		// the board they are on, never by the project.
		r.Post("/boards/{boardID}/swimlanes", s.handleAddSwimlane)
		r.Put("/boards/{boardID}/swimlanes", s.handleReorderSwimlanes)
		r.Patch("/boards/{boardID}/swimlanes/{swimlaneID}", s.handleUpdateSwimlane)
		r.Delete("/boards/{boardID}/swimlanes/{swimlaneID}", s.handleDeleteSwimlane)
	})

	// Repositories and what flows in from them.
	r.Get("/projects/{projectKey}/repositories", s.handleListRepositories)
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.ProjectAdminister))
		r.Patch("/repositories/{repositoryID}", s.handleUpdateRepository)
		r.Post("/repositories/{repositoryID}/rotate-secret", s.handleRotateWebhookSecret)
		r.Delete("/repositories/{repositoryID}", s.handleDisconnectRepository)
	})
	r.Get("/issues/{issueKey}/development", s.handleIssueDevelopment)
}

// dashboardRoutes registers dashboards and their shares.
func (s *Server) dashboardRoutes(r chi.Router) {
	// Dashboards: reading one is ordinary work, arranging one is
	// configuring the project.
	r.Get("/projects/{projectKey}/dashboards", s.handleListDashboards)
	r.Get("/projects/{projectKey}/report-kinds", s.handleReportKinds)
	r.Get("/dashboard-templates", s.handleListDashboardTemplates)
	// Printing a dashboard is reading it.
	r.Get("/dashboards/{dashboardID}/pdf", s.handleDashboardPDF)
	r.Get("/projects/{projectKey}/reports/{kind}", s.handleReport)
	r.With(requireProjectPerm(perm.ProjectAdminister), s.requireFeature(project.FeatureDashboard)).
		Post("/projects/{projectKey}/dashboards", s.handleCreateDashboard)
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.ProjectAdminister))
		r.Patch("/dashboards/{dashboardID}", s.handleRenameDashboard)
		r.Delete("/dashboards/{dashboardID}", s.handleDeleteDashboard)
		r.Post("/dashboards/{dashboardID}/widgets", s.handleAddWidget)
		r.Put("/dashboards/{dashboardID}/widgets", s.handleReorderWidgets)
		r.Patch("/widgets/{widgetID}", s.handleUpdateWidget)
		r.Delete("/widgets/{widgetID}", s.handleRemoveWidget)
		// A template hangs off nothing: making or removing one takes
		// nothing from anyone but the chooser.
		r.Post("/dashboards/{dashboardID}/template", s.handleSaveDashboardTemplate)
		r.Delete("/dashboard-templates/{templateID}", s.handleDeleteDashboardTemplate)
		// Sharing a dashboard outside is arranging where it is seen.
		r.Get("/dashboards/{dashboardID}/shares", s.handleListShares)
		r.Post("/dashboards/{dashboardID}/shares", s.handleCreateShare)
		r.Delete("/dashboards/{dashboardID}/shares/{shareID}", s.handleRevokeShare)
	})
}

// deskRoutes registers the service desk's agent side.
func (s *Server) deskRoutes(r chi.Router) {
	// The service desk's agent side.
	r.Get("/projects/{projectKey}/articles", s.handleListArticles)
	r.Get("/projects/{projectKey}/canned-responses", s.handleListCanned)
	r.Post("/canned-responses/{responseID}/render", s.handleRenderCanned)
	r.Get("/issues/{issueKey}/csat", s.handleIssueRating)
	r.Get("/projects/{projectKey}/business-calendar", s.handleGetCalendar)
	r.With(requireProjectPerm(perm.ProjectAdminister)).Post("/projects/{projectKey}/articles", s.handleCreateArticle)
	r.With(requireProjectPerm(perm.ProjectAdminister)).Post("/projects/{projectKey}/canned-responses", s.handleCreateCanned)
	r.With(requireProjectPerm(perm.ProjectAdminister)).Put("/projects/{projectKey}/business-calendar", s.handleSaveCalendar)
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.ProjectAdminister))
		r.Patch("/articles/{articleID}", s.handleUpdateArticle)
		r.Delete("/articles/{articleID}", s.handleDeleteArticle)
		r.Patch("/canned-responses/{responseID}", s.handleUpdateCanned)
		r.Delete("/canned-responses/{responseID}", s.handleDeleteCanned)
	})
	r.Get("/projects/{projectKey}/request-types", s.handleListRequestTypes)
	r.Get("/projects/{projectKey}/mail-address", s.handleDeskMailAddress)
	r.Get("/projects/{projectKey}/sla-policies", s.handleListPolicies)
	r.Get("/projects/{projectKey}/queue", s.handleQueue)
	r.Get("/issues/{issueKey}/timers", s.handleIssueTimers)
	r.With(requireIssuePerm(perm.CommentWrite)).
		Post("/issues/{issueKey}/notes", s.handleAddNote)

	// Watchers: who is told about an issue. Adding somebody is as
	// much of a say as commenting is.
	r.Get("/issues/{issueKey}/watchers", s.handleListWatchers)
	r.Group(func(r chi.Router) {
		r.Use(requireIssuePerm(perm.CommentWrite))
		r.Post("/issues/{issueKey}/watchers", s.handleAddWatcher)
		r.Delete("/issues/{issueKey}/watchers/{userID}", s.handleRemoveWatcher)
	})
	r.With(requireProjectPerm(perm.ProjectAdminister)).
		Post("/projects/{projectKey}/request-types", s.handleCreateRequestType)
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.ProjectAdminister))
		r.Patch("/request-types/{requestTypeID}", s.handleUpdateRequestType)
		r.Delete("/request-types/{requestTypeID}", s.handleDeleteRequestType)
		r.Patch("/sla-policies/{policyID}", s.handleUpdatePolicy)
	})
	r.With(requireIssuePerm(perm.IssueWrite)).
		Post("/issues/{issueKey}/branches", s.handleCreateBranch)
	r.With(requireIssuePerm(perm.IssueWrite)).
		Post("/issues/{issueKey}/pull-requests", s.handleOpenPullRequest)

	// Companies: who reads whose shared requests is the desk's to say.
	r.Get("/projects/{projectKey}/companies", s.handleListCompanies)
	r.With(requireProjectPerm(perm.ProjectAdminister)).
		Post("/projects/{projectKey}/companies", s.handleCreateCompany)
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.ProjectAdminister))
		r.Patch("/companies/{companyID}", s.handleUpdateCompany)
		r.Delete("/companies/{companyID}", s.handleDeleteCompany)
		r.Post("/companies/{companyID}/members", s.handleAddCompanyMember)
		r.Delete("/companies/{companyID}/members/{userID}", s.handleRemoveCompanyMember)
	})
}

// issueRoutes registers fields, labels, time, files, and the issue itself.
func (s *Server) issueRoutes(r chi.Router) {
	// Custom fields: defined by a project's administrators, answered
	// by anyone who may edit the issue.
	r.Get("/field-kinds", s.handleFieldKinds)
	r.Get("/projects/{projectKey}/fields", s.handleListFields)
	// How this project draws an issue of each type, which anyone who may read
	// the project needs in order to draw one at all.
	r.Get("/projects/{projectKey}/issue-arrangement", s.handleProjectArrangement)
	r.With(requireProjectPerm(perm.ProjectAdminister)).
		Post("/projects/{projectKey}/fields", s.handleCreateField)
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.ProjectAdminister))
		r.Patch("/fields/{fieldID}", s.handleUpdateField)
		r.Delete("/fields/{fieldID}", s.handleDeleteField)
	})
	r.Get("/issues/{issueKey}/fields", s.handleIssueFields)
	r.With(requireIssuePerm(perm.IssueWrite)).
		Put("/issues/{issueKey}/fields/{fieldID}", s.handleSetIssueField)

	// Labels: anyone who edits issues may coin a word by tagging
	// with it; renaming or removing a word for everyone is
	// administration of the installation.
	r.Get("/labels", s.handleListLabels)
	r.With(requireObjectPerm(perm.IssueWrite)).
		Post("/labels", s.handleCreateLabel)
	r.Group(func(r chi.Router) {
		r.Use(requirePerm(perm.OrgAdminister))
		r.Patch("/labels/{labelID}", s.handleUpdateLabel)
		r.Delete("/labels/{labelID}", s.handleDeleteLabel)
	})
	r.With(requireIssuePerm(perm.IssueWrite)).
		Put("/issues/{issueKey}/labels", s.handleSetIssueLabels)

	// Time logged on an issue. An entry is its author's, or an
	// administrator's, to change; the service checks which.
	r.Get("/issues/{issueKey}/worklogs", s.handleListWorklogs)
	r.With(requireIssuePerm(perm.IssueWrite)).
		Post("/issues/{issueKey}/worklogs", s.handleLogWork)
	r.Group(func(r chi.Router) {
		r.Use(requireObjectPerm(perm.IssueWrite))
		r.Patch("/worklogs/{worklogID}", s.handleUpdateWorklog)
		r.Delete("/worklogs/{worklogID}", s.handleDeleteWorklog)
	})

	// Attachments. Reading one is reading the issue; adding and
	// removing are editing it.
	r.Get("/issues/{issueKey}/attachments", s.handleListAttachments)
	r.With(requireIssuePerm(perm.IssueWrite)).
		Post("/issues/{issueKey}/attachments", s.handleUploadAttachment)
	r.Get("/attachments/{attachmentID}", s.handleDownloadAttachment)
	r.With(requireObjectPerm(perm.IssueWrite)).
		Delete("/attachments/{attachmentID}", s.handleDeleteAttachment)

	// Issues.
	r.Get("/issues", s.handleListIssues)
	r.Get("/issues/{issueKey}", s.handleGetIssue)
	r.With(requireObjectPerm(perm.IssueWrite)).
		Post("/issues", s.handleCreateIssue)
	r.With(requireIssuePerm(perm.IssueWrite)).
		Patch("/issues/{issueKey}", s.handleUpdateIssue)
	r.With(requireIssuePerm(perm.IssueWrite)).
		Delete("/issues/{issueKey}", s.handleDeleteIssue)
	r.Get("/issues/{issueKey}/children", s.handleIssueChildren)
	r.Get("/issues/{issueKey}/hierarchy", s.handleIssueHierarchy)
	r.Get("/issues/{issueKey}/history", s.handleIssueHistory)

	// Reparenting is its own endpoint rather than a field on the
	// edit: it moves the issue in the tree, and it is refused for
	// reasons an ordinary field edit never has.
	r.With(requireIssuePerm(perm.IssueWrite)).
		Put("/issues/{issueKey}/parent", s.handleSetParent)

	// Scheduling is its own endpoint for the same reason: both ends
	// of a range move together or the range is briefly nonsense.
	r.With(requireIssuePerm(perm.IssueWrite)).
		Put("/issues/{issueKey}/schedule", s.handleScheduleIssue)

	// So are committing work to a sprint and sizing it: both are
	// refused for reasons an ordinary field edit never has.
	r.With(requireIssuePerm(perm.SprintManage)).
		Put("/issues/{issueKey}/sprint", s.handleSetIssueSprint)
	r.With(requireIssuePerm(perm.IssueWrite)).
		Put("/issues/{issueKey}/estimate", s.handleSetIssueEstimate)
	// Which milestone work counts towards is a matter of writing the
	// issue, not of running the project.
	r.With(requireIssuePerm(perm.IssueWrite)).
		Put("/issues/{issueKey}/milestone", s.handleSetIssueMilestone)
	r.With(requireIssuePerm(perm.TeamManage)).
		Put("/issues/{issueKey}/team", s.handleSetIssueTeam)

	r.Get("/issues/{issueKey}/links", s.handleListLinks)
	r.With(requireIssuePerm(perm.IssueWrite)).
		Post("/issues/{issueKey}/links", s.handleAddLink)
	r.With(requireIssuePerm(perm.IssueWrite)).
		Delete("/issues/{issueKey}/links/{linkID}", s.handleDeleteLink)

	// Pages elsewhere about the issue. Putting one on it or taking one off is
	// editing the issue, though the issue itself does not change.
	r.Get("/issues/{issueKey}/remote-links", s.handleListRemoteLinks)
	r.Group(func(r chi.Router) {
		r.Use(requireIssuePerm(perm.IssueWrite))
		r.Post("/issues/{issueKey}/remote-links", s.handlePutRemoteLink)
		r.Delete("/issues/{issueKey}/remote-links/{remoteLinkID}", s.handleDeleteRemoteLink)
	})

	// Moving an issue goes through the workflow, never by editing
	// the status field directly.
	r.Get("/issues/{issueKey}/transitions", s.handleListTransitions)
	r.With(requireIssuePerm(perm.IssueTransition)).
		Post("/issues/{issueKey}/transitions", s.handleTransitionIssue)

	r.Get("/issues/{issueKey}/comments", s.handleListComments)
	r.Group(func(r chi.Router) {
		r.Use(requireIssuePerm(perm.CommentWrite))
		r.Post("/issues/{issueKey}/comments", s.handleAddComment)
		r.Patch("/issues/{issueKey}/comments/{commentID}", s.handleUpdateComment)
		r.Delete("/issues/{issueKey}/comments/{commentID}", s.handleDeleteComment)
	})

	r.Group(func(r chi.Router) {
		r.Use(requirePerm(perm.OrgAdminister))
		r.Get("/invites", s.handleListInvites)
		r.Post("/invites", s.handleCreateInvite)
		r.Delete("/invites/{inviteID}", s.handleRevokeInvite)
	})
}
