import { createRoute, notFound, redirect } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { MOVED_PROJECT_PAGES, movedProjectAddress } from "@/features/projects/settingsTabs";

/** One old project address, such as /teams, sent on to its tab of the settings with its search and hash kept. */
function movedRoute(path: string) {
  return createRoute({
    getParentRoute: () => projectRoute,
    path,
    beforeLoad: ({ params, location }) => {
      const to = movedProjectAddress((params as { projectKey: string }).projectKey, path.slice(1), location.searchStr, location.hash);
      if (!to) throw notFound();
      throw redirect({ href: to, replace: true });
    },
  });
}

export const projectMovedRoutes = MOVED_PROJECT_PAGES.map(movedRoute);
