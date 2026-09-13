import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { useIssues } from "@/api/issues";
import { useProjects } from "@/api/projects";
import { EmptyState, Page, PageHeader, SectionTitle, Table, Td, Th } from "@/components/ui";
import { IssueTable } from "@/features/issues/IssueTable";

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
      <PageHeader
        title={firstName ? `${firstName}'s work` : "Your work"}
        meta={
          mine
            ? `${mine.total} open issue${mine.total === 1 ? "" : "s"} assigned to you`
            : undefined
        }
      />

      <section className="mb-8">
        <SectionTitle className="mb-2">Assigned to you</SectionTitle>
        {mine && issues.length === 0 ? (
          <EmptyState
            title="Nothing assigned to you"
            description="Issues land here when somebody assigns them to you, or when you start progress on one."
          />
        ) : (
          <IssueTable issues={issues} showProject />
        )}
        {mine && mine.total > issues.length && (
          <p className="mt-2 text-sm text-ink-subtle">
            Showing {issues.length} of {mine.total}. The rest are in their projects.
          </p>
        )}
      </section>

      <section>
        <div className="mb-2 flex items-baseline justify-between">
          <SectionTitle>Projects</SectionTitle>
          <Link to="/projects" className="text-sm text-ink-muted hover:text-ink">
            All projects
          </Link>
        </div>
        {projectData && projects.length === 0 ? (
          <EmptyState title="No projects yet" description="Projects hold issues. Make one from the projects page." />
        ) : (
          <Table>
            <thead>
              <tr>
                <Th className="w-20">Key</Th>
                <Th>Project</Th>
                <Th className="w-24 text-right">Open</Th>
              </tr>
            </thead>
            <tbody>
              {projects.map((project) => (
                <tr key={project.id} className="group hover:bg-surface-raised/60">
                  <Td className="font-mono text-sm text-ink-muted">{project.key}</Td>
                  <Td>
                    <Link
                      to="/projects/$projectKey"
                      params={{ projectKey: project.key }}
                      className="text-ink group-hover:text-accent"
                    >
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
      </section>
    </Page>
  );
}
