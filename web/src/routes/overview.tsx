import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { useIssues } from "@/api/issues";
import { useProjects } from "@/api/projects";
import { EmptyState, Page, PageHeader, SectionTitle, Table, Td, Th } from "@/components/ui";
import { IssueTable } from "@/features/issues/IssueTable";
import { t } from "@/i18n";

export const overviewRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/",
  component: Home,
});

/** How many of your own issues the home page lists before pointing elsewhere. */
const HOME_ISSUE_LIMIT = 15;

/**
 * Home is the work in front of you: what is assigned to you and still open,
 * and the projects it lives in. Nothing here is about the product.
 */
function Home() {
  const { data } = useMe();
  const { data: mine } = useIssues({
    assignee: "me",
    category: ["todo", "in_progress"],
    orderBy: "updated",
    limit: HOME_ISSUE_LIMIT,
  });
  const { data: projectData } = useProjects();

  const issues = mine?.issues ?? [];
  const projects = projectData?.projects ?? [];
  const firstName = data?.principal?.user.name.split(" ")[0];

  return (
    <Page width="content">
      <PageHeader title={firstName ? t.search.home.titleOf(firstName) : t.search.home.title} meta={mine ? t.search.home.openAssigned(mine.total) : undefined} />

      <section className="mb-8">
        <SectionTitle className="mb-2">{t.search.home.assigned}</SectionTitle>
        {mine && issues.length === 0 ? (
          <EmptyState title={t.search.home.nothingAssigned} description={t.search.home.nothingAssignedHint} />
        ) : (
          <IssueTable issues={issues} showProject />
        )}
        {mine && mine.total > issues.length && <p className="mt-2 text-sm text-ink-subtle">{t.search.home.showingIssues(issues.length, mine.total)}</p>}
      </section>

      <section>
        <div className="mb-2 flex items-baseline justify-between">
          <SectionTitle>{t.search.home.projects}</SectionTitle>
          <Link to="/projects" className="text-sm text-ink-muted hover:text-ink">
            {t.search.home.allProjects}
          </Link>
        </div>
        {projectData && projects.length === 0 ? (
          <EmptyState title={t.search.home.noProjects} description={t.search.home.noProjectsHint} />
        ) : (
          <Table>
            <thead>
              <tr>
                <Th className="w-20">{t.search.home.key}</Th>
                <Th>{t.search.home.project}</Th>
                <Th className="w-24 text-right">{t.search.home.open}</Th>
              </tr>
            </thead>
            <tbody>
              {projects.map((project) => (
                <tr key={project.id} className="group hover:bg-surface-raised/60">
                  <Td className="font-mono text-sm text-ink-muted">{project.key}</Td>
                  <Td>
                    <Link to="/projects/$projectKey" params={{ projectKey: project.key }} className="text-ink group-hover:text-accent">
                      {project.name}
                    </Link>
                  </Td>
                  <Td className="text-right tabular-nums">
                    {project.openIssueCount}
                    <span className="text-ink-subtle"> / {project.issueCount}</span>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
        {projectData && projectData.total > projects.length && (
          <p className="mt-2 text-sm text-ink-subtle" data-projects-more>
            {t.search.home.showingProjects(projects.length, projectData.total)}
          </p>
        )}
      </section>
    </Page>
  );
}
