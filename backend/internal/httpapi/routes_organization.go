package httpapi

import (
	"github.com/go-chi/chi/v5"

	"github.com/armature/armature/backend/internal/perm"
)

// organizationRoutes registers the organization itself: its record, inbox, tokens, members, roles and workflows.
func (s *Server) organizationRoutes(r chi.Router) {
	// The organization's record of who did what is its
	// administrators' to read. Fields every project shares are
	// theirs to define; a project's own may be promoted by them.
	r.Group(func(r chi.Router) {
		r.Use(requirePerm(perm.OrgAdminister))
		r.Get("/audit", s.handleAuditLog)
		r.Get("/audit/export", s.handleAuditExport)
		r.Post("/fields", s.handleCreateOrgField)
		r.Post("/fields/{fieldID}/promote", s.handlePromoteField)
		// The arrangement every project follows until it disagrees.
		r.Get("/issue-arrangement", s.handleOrgArrangement)
		r.Put("/issue-arrangement", s.handleSetOrgArrangement)
	})
	r.Get("/fields", s.handleListOrgFields)
	// How a project says it is doing, and its month.
	r.Get("/projects/{projectKey}/status-updates", s.handleListStatusUpdates)
	r.With(requireProjectPerm(perm.ProjectAdminister)).Post("/projects/{projectKey}/status-updates", s.handlePostStatusUpdate)
	r.Get("/projects/{projectKey}/calendar", s.handleCalendarMonth)

	// The inbox and how it is fed. Both are the caller's own.
	r.Get("/notifications", s.handleInbox)
	r.Get("/notifications/unread-count", s.handleUnreadCount)
	r.Post("/notifications/read", s.handleMarkRead)
	r.Get("/notification-preferences", s.handleNotificationPreferences)
	r.Put("/notification-preferences", s.handleSaveNotificationPreferences)

	// Themes: the caller's own and the shared ones. The active one is read
	// before the id routes so "active" is never taken for a theme's id.
	r.Get("/themes", s.handleListThemes)
	r.Post("/themes", s.handleCreateTheme)
	r.Get("/themes/examples", s.handleThemeExamples)
	r.Get("/themes/active", s.handleActiveTheme)
	r.Put("/themes/active", s.handleChooseTheme)
	r.With(requirePerm(perm.OrgAdminister)).Put("/themes/default", s.handleSetDefaultTheme)
	r.Post("/themes/import", s.handleImportTheme)
	r.Get("/themes/{themeID}/export", s.handleExportTheme)
	r.Get("/themes/{themeID}", s.handleGetTheme)
	r.Patch("/themes/{themeID}", s.handleUpdateTheme)
	r.Delete("/themes/{themeID}", s.handleDeleteTheme)
	r.Post("/themes/{themeID}/assets", s.handleUploadThemeAsset)
	r.Get("/themes/{themeID}/assets/{assetID}", s.handleThemeAsset)
	r.Delete("/themes/{themeID}/assets/{assetID}", s.handleDeleteThemeAsset)

	r.Get("/tokens", s.handleListAPITokens)
	// Making one is session only, so a leaked key cannot mint a longer lived
	// key and outlive the revocation of the key it was made with.
	r.With(requireSession).Post("/tokens", s.handleCreateAPIToken)
	r.Delete("/tokens/{tokenID}", s.handleRevokeAPIToken)

	// Metadata behind the client's pickers.
	r.Get("/members", s.handleListMembers)
	r.With(requirePerm(perm.OrgAdminister)).Delete("/members/{userID}", s.handleRemoveMember)
	// The whole organization. Owners only, which the service decides, since
	// ownership is standing in the organization rather than a granted role.
	r.Delete("/organization", s.handleDeleteOrganization)
	r.Get("/users/{userID}/avatar", s.handleAvatar)
	// The organization's own accounts are its administrators' to make and manage.
	r.Group(func(r chi.Router) {
		r.Use(requirePerm(perm.OrgAdminister))
		r.Get("/users", s.handleListUsers)
		r.Post("/users", s.handleCreateUser)
		r.Patch("/users/{userID}", s.handleUpdateUser)
		r.Put("/users/{userID}/password", s.handleSetUserPassword)
		// People the identity provider vouched for who are waiting to be let in.
		r.Get("/users/requests", s.handleListJoinRequests)
		r.Post("/users/requests/{userID}/admit", s.handleAdmitJoinRequest)
		r.Delete("/users/requests/{userID}", s.handleDeclineJoinRequest)
	})
	// Which days people can work. Every agent reads the calendars, since
	// planning shows whose days are off; administrators change them.
	r.Get("/holiday-calendars", s.handleListHolidayCalendars)
	r.Get("/holiday-calendars/{calendarID}", s.handleGetHolidayCalendar)
	// A person reads their own week; the handler lets an administrator read anybody's.
	r.Get("/users/{userID}/schedule", s.handleGetWorkingWeek)
	r.Group(func(r chi.Router) {
		r.Use(requirePerm(perm.OrgAdminister))
		r.Post("/holiday-calendars", s.handleCreateHolidayCalendar)
		r.Patch("/holiday-calendars/{calendarID}", s.handleUpdateHolidayCalendar)
		r.Delete("/holiday-calendars/{calendarID}", s.handleDeleteHolidayCalendar)
		r.Put("/holiday-calendars/{calendarID}/days", s.handleSetHolidays)
		r.Post("/holiday-calendars/{calendarID}/import", s.handleImportHolidays)
		r.Put("/users/{userID}/schedule", s.handleSetWorkingWeek)
	})
	// Who is away, for every agent to plan around. Whether a caller may
	// write one down depends on whose teams the person is on: the service decides.
	r.Get("/absences", s.handleListAbsences)
	r.Post("/absences", s.handleRecordAbsence)
	r.Patch("/absences/{absenceID}", s.handleUpdateAbsence)
	r.Delete("/absences/{absenceID}", s.handleRemoveAbsence)
	r.Get("/issue-types", s.handleListIssueTypes)
	r.Get("/statuses", s.handleListStatuses)
	r.Get("/link-types", s.handleListLinkTypes)
	r.Get("/workflows", s.handleListWorkflows)
	r.Get("/workflows/rule-types", s.handleWorkflowRuleTypes)
	r.Get("/workflows/{workflowID}", s.handleGetWorkflow)
	r.Get("/workflow-schemes", s.handleListSchemes)

	// What the roles mean, and what this caller holds. Both are
	// readable by anyone: a client that cannot ask what it may do
	// has to draw every button and refuse on click.
	r.Get("/roles", s.handleListRoles)
	r.Get("/permissions", s.handleListPermissions)
	// The matrix: what each of the organization's roles grants, its
	// administrators' to change.
	r.Group(func(r chi.Router) {
		r.Use(requirePerm(perm.OrgAdminister))
		r.Post("/roles", s.handleCreateRole)
		r.Patch("/roles/{roleKey}", s.handleUpdateRole)
		r.Delete("/roles/{roleKey}", s.handleDeleteRole)
	})
	r.Get("/access/me", s.handleMyAccess)

	// Granting access is administration of the tenant, even when
	// the grant is scoped to one project.
	r.Group(func(r chi.Router) {
		r.Use(requirePerm(perm.OrgAdminister))
		r.Get("/groups", s.handleListGroups)
		r.Post("/groups", s.handleCreateGroup)
		r.Get("/groups/{groupID}", s.handleGetGroup)
		r.Delete("/groups/{groupID}", s.handleDeleteGroup)
		r.Post("/groups/{groupID}/members", s.handleAddGroupMember)
		r.Delete("/groups/{groupID}/members/{userID}", s.handleRemoveGroupMember)

		r.Get("/oidc-provider", s.handleGetOIDCProvider)
		r.Put("/oidc-provider", s.handleSaveOIDCProvider)

		r.Get("/role-assignments", s.handleListAssignments)
		r.Post("/role-assignments", s.handleGrantRole)
		r.Delete("/role-assignments/{assignmentID}", s.handleRevokeRole)
	})

	// Authoring workflows and deciding which scheme answers for the
	// organization is administration of the tenant.
	r.Group(func(r chi.Router) {
		r.Use(requirePerm(perm.OrgAdminister))
		r.Post("/statuses", s.handleCreateStatus)
		r.Post("/workflows", s.handleCreateWorkflow)
		r.Put("/workflows/{workflowID}", s.handleSaveWorkflow)
		r.Post("/workflows/{workflowID}/copy", s.handleCopyWorkflow)
		r.Delete("/workflows/{workflowID}", s.handleDeleteWorkflow)

		r.Post("/workflow-schemes", s.handleCreateScheme)
		r.Put("/workflow-schemes/{schemeID}", s.handleSaveScheme)
		r.Put("/workflow-schemes/{schemeID}/default", s.handleSetDefaultScheme)
		r.Delete("/workflow-schemes/{schemeID}", s.handleDeleteScheme)
	})
}
