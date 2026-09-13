import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { useQueue, type QueueFilter } from "@/api/desk";
import { EmptyState, Page, PageHeader, Segmented, Table, Td, Th } from "@/components/ui";
import { Avatar, StatusBadge, TypeBadge, relativeTime } from "@/features/issues/badges";
import { SlaBadge } from "@/features/desk/SlaBadge";

export const queuesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/queues",
  component: QueuesPage,
});

const filters: Array<{ value: QueueFilter; label: string }> = [
  { value: "open", label: "Open" },
  { value: "unassigned", label: "Unassigned" },
  { value: "mine", label: "Mine" },
  { value: "breached", label: "Breached" },
  { value: "all", label: "All" },
];

/** The agents' view of a desk: what is waiting, and how long it has left. */
function QueuesPage() {
  const { projectKey } = queuesRoute.useParams();
  const { data: projectData } = useProject(projectKey);
  const [filter, setFilter] = useState<QueueFilter>("open");
  const { data, isLoading } = useQueue(projectKey, filter);
  const rows = data?.rows ?? [];

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {projectData?.project?.name ?? projectKey}
          </Link>
        }
        title="Queues"
        meta={data ? `${rows.length} request${rows.length === 1 ? "" : "s"}` : undefined}
      />
      <div className="mb-3">
        <Segmented label="Queue" value={filter} onChange={setFilter} options={filters} />
      </div>

      {isLoading ? null : rows.length === 0 ? (
        <EmptyState title="Nothing in this queue" description="Requests customers raise land in Open, least time left first." />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th className="w-8" aria-label="Type" />
              <Th className="w-24">Key</Th>
              <Th>Request</Th>
              <Th className="w-36">Kind</Th>
              <Th className="w-32">Team</Th>
              <Th className="w-40">Status</Th>
              <Th className="w-10" aria-label="Assignee" />
              <Th className="w-44">First response</Th>
              <Th className="w-44">Resolution</Th>
              <Th className="w-16">Rated</Th>
              <Th className="w-20 text-right">Raised</Th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const first = row.timers.find((t) => t.metric === "first_response");
              const resolution = row.timers.find((t) => t.metric === "resolution");
              return (
                <tr key={row.issue.id} className="group hover:bg-surface-raised/60" data-queue-row={row.issue.key}>
                  <Td>
                    <TypeBadge icon={row.issue.type.icon} name={row.issue.type.name} />
                  </Td>
                  <Td className="font-mono text-sm text-ink-muted">{row.issue.key}</Td>
                  <Td className="max-w-0">
                    <Link
                      to="/issues/$issueKey"
                      params={{ issueKey: row.issue.key }}
                      className="block truncate text-ink group-hover:text-accent"
                    >
                      {row.issue.summary}
                    </Link>
                  </Td>
                  <Td className="text-sm text-ink-muted">{row.requestTypeName ?? ""}</Td>
                  <Td className="text-sm text-ink-muted" data-queue-team={row.issue.team?.name ?? ""}>
                    {row.issue.team?.name ?? ""}
                  </Td>
                  <Td>
                    <StatusBadge name={row.issue.status.name} category={row.issue.status.category} />
                  </Td>
                  <Td>
                    <Avatar name={row.issue.assignee?.name} src={row.issue.assignee?.avatarUrl} size="sm" />
                  </Td>
                  <Td>{first && <SlaBadge timer={first} short />}</Td>
                  <Td>{resolution && <SlaBadge timer={resolution} short />}</Td>
                  <Td className="text-sm tabular-nums text-ink-muted" data-queue-csat={row.csat ?? ""}>
                    {row.csat ? `${row.csat} / 5` : ""}
                  </Td>
                  <Td className="text-right text-sm whitespace-nowrap text-ink-subtle">{relativeTime(row.issue.createdAt)}</Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}
    </Page>
  );
}
