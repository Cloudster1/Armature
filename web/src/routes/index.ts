import { createRouter } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { rootRoute } from "./root";
import { loginRoute, signupRoute } from "./auth";
import { deskEntryRoute } from "./desk-entry";
import { unwatchRoute } from "./unwatch";
import { inviteRoute } from "./invite";
import { rateRoute } from "./rate";
import { sharedRoute } from "./shared";
import { appRoute } from "./app";
import { projectRoute } from "./project";
import { overviewRoute } from "./overview";
import { tokensRoute } from "./tokens";
import { settingsRoute } from "./settings";
import { profileRoute } from "./profile";
import { inboxRoute } from "./inbox";
import { automationRoute, orgAutomationRoute } from "./automation";
import { componentsRoute, releasesRoute } from "./releases";
import { filtersRoute } from "./filters";
import { importRoute } from "./import";
import { webhooksRoute } from "./webhooks";
import { auditRoute } from "./audit";
import { orgFieldsRoute } from "./org-fields";
import { orgIssueArrangementRoute } from "./org-issue-arrangement";
import { calendarRoute } from "./calendar";
import { projectsRoute } from "./projects";
import { projectDetailRoute } from "./project-detail";
import { projectSettingsRoute } from "./project-settings";
import { issueDetailRoute } from "./issue-detail";
import { boardRoute } from "./board";
import { sprintBoardRoute } from "./sprint-board";
import { hierarchyRoute } from "./hierarchy";
import { planRoute } from "./plan";
import { searchRoute } from "./search";
import { workflowSchemesRoute, workflowStatusesRoute, workflowsIndexRoute, workflowsRoute } from "./workflows";
import { workflowDesignRoute, workflowNewRoute } from "./workflow-design";
import { projectWorkflowsRoute } from "./project-workflows";
import { sprintsRoute } from "./sprints";
import { milestonesRoute } from "./milestones";
import { teamsRoute } from "./teams";
import { accessRoute } from "./access";
import { organizationRoute } from "./organization";
import { repositoriesRoute } from "./repositories";
import { fieldsRoute } from "./fields";
import { issueArrangementRoute } from "./issue-arrangement";
import { labelsRoute } from "./labels";
import { queuesRoute } from "./queues";
import { dashboardRoute } from "./dashboard";
import { serviceDeskRoute } from "./service-desk";
import { portalArticleRoute, portalNewRoute, portalRequestRoute, portalRoute } from "./portal";

// A project's pages hang off projectRoute, so the project is loaded once and
// the sidebar and crumbs read one parameter.
const routeTree = rootRoute.addChildren([
  loginRoute,
  signupRoute,
  inviteRoute,
  deskEntryRoute,
  unwatchRoute,
  rateRoute,
  sharedRoute,
  appRoute.addChildren([
    overviewRoute,
    projectsRoute,
    searchRoute,
    projectRoute.addChildren([
      projectDetailRoute,
      boardRoute,
      sprintBoardRoute,
      hierarchyRoute,
      planRoute,
      calendarRoute,
      projectWorkflowsRoute,
      sprintsRoute,
      milestonesRoute,
      teamsRoute,
      repositoriesRoute,
      automationRoute,
      releasesRoute,
      componentsRoute,
      importRoute,
      fieldsRoute,
      issueArrangementRoute,
      queuesRoute,
      dashboardRoute,
      serviceDeskRoute,
      projectSettingsRoute,
    ]),
    portalRoute,
    portalNewRoute,
    portalRequestRoute,
    portalArticleRoute,
    issueDetailRoute,
    settingsRoute,
    profileRoute,
    inboxRoute,
    filtersRoute,
    orgAutomationRoute,
    webhooksRoute,
    auditRoute,
    orgFieldsRoute,
    orgIssueArrangementRoute,
    tokensRoute,
    workflowsRoute.addChildren([workflowsIndexRoute, workflowStatusesRoute, workflowSchemesRoute]),
    workflowNewRoute,
    workflowDesignRoute,
    accessRoute,
    organizationRoute,
    labelsRoute,
  ]),
]);

export function buildRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
  });
}

export type AppRouter = ReturnType<typeof buildRouter>;

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
