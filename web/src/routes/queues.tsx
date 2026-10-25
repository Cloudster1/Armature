import { useEffect, useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { useCompanies, useQueue, type QueueFilter } from "@/api/desk";
import { EmptyState, Page, PageHeader, Pager, Segmented, SelectInput, Table, Td, Th, clampOffset, countOf } from "@/components/ui";
import { Avatar, StatusBadge, TypeBadge, RelativeTime } from "@/features/issues/badges";
import { SlaBadge } from "@/features/desk/SlaBadge";
import { companyLine } from "@/features/desk/company";
import { QUEUE_PAGE_SIZE } from "@/config";
import { t } from "@/i18n";

export const queuesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/queues",
  component: QueuesPage,
});

const filterValues: QueueFilter[] = ["open", "unassigned", "mine", "breached", "all"];

// Read while rendering, so the words follow the language.
const filters = (): Array<{ value: QueueFilter; label: string }> => filterValues.map((value) => ({ value, label: t.desk.queues.filters[value]! }));

/** The agents' view of a desk: what is waiting, and how long it has left. */
function QueuesPage() {
  const { projectKey } = queuesRoute.useParams();
  const { data: projectData } = useProject(projectKey);
  const [filter, setFilter] = useState<QueueFilter>("open");
  const [offset, setOffset] = useState(0);
  const [company, setCompany] = useState("");
  const { data, isLoading } = useQueue(projectKey, filter, offset, company);
  const { data: companyData } = useCompanies(projectKey);
  const companies = companyData?.companies ?? [];
  const rows = data?.rows ?? [];
  const total = data?.total ?? 0;

  // Requests leave a queue while it is open; a page past the end steps back.
  useEffect(() => {
    if (data) setOffset((current) => clampOffset(current, data.total, QUEUE_PAGE_SIZE));
  }, [data]);

  function choose(next: QueueFilter) {
    setFilter(next);
    setOffset(0);
  }

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {projectData?.project?.name ?? projectKey}
          </Link>
        }
        title={t.desk.queues.title}
        meta={data ? countOf(total, t.kit.counted.request) : undefined}
      />
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <Segmented label={t.desk.queues.queue} value={filter} onChange={choose} options={filters()} />
        {companies.length > 0 && (
          <SelectInput
            aria-label={t.desk.queues.company}
            value={company}
            onChange={(e) => {
              setCompany(e.target.value);
              setOffset(0);
            }}
            data-queue-company-filter
            className="w-48"
          >
            <option value="">{t.desk.queues.everyCompany}</option>
            {companies.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </SelectInput>
        )}
      </div>

      {isLoading ? null : rows.length === 0 ? (
        <EmptyState title={t.desk.queues.empty} description={t.desk.queues.emptyDetail} />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th className="w-8" aria-label={t.desk.queues.type} />
              <Th className="w-24">{t.desk.queues.key}</Th>
              <Th>{t.desk.queues.request}</Th>
              <Th className="w-36">{t.desk.queues.kind}</Th>
              <Th className="w-32">{t.desk.queues.company}</Th>
              <Th className="w-32">{t.desk.queues.team}</Th>
              <Th className="w-40">{t.desk.queues.status}</Th>
              <Th className="w-10" aria-label={t.desk.queues.assignee} />
              <Th className="w-44">{t.desk.queues.firstResponse}</Th>
              <Th className="w-44">{t.desk.queues.resolution}</Th>
              <Th className="w-16">{t.desk.queues.rated}</Th>
              <Th className="w-20 text-right">{t.desk.queues.raised}</Th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const first = row.timers.find((timer) => timer.metric === "first_response");
              const resolution = row.timers.find((timer) => timer.metric === "resolution");
              return (
                <tr key={row.issue.id} className="group hover:bg-surface-raised/60" data-queue-row={row.issue.key}>
                  <Td>
                    <TypeBadge icon={row.issue.type.icon} name={row.issue.type.name} />
                  </Td>
                  <Td className="font-mono text-sm text-ink-muted">{row.issue.key}</Td>
                  <Td className="max-w-0">
                    <Link to="/issues/$issueKey" params={{ issueKey: row.issue.key }} className="block min-w-48 truncate text-ink group-hover:text-accent">
                      {row.issue.summary}
                    </Link>
                  </Td>
                  <Td className="text-sm text-ink-muted">{row.requestTypeName ?? ""}</Td>
                  <Td className="text-sm text-ink-muted" data-queue-company={row.issue.company?.name ?? ""}>
                    {row.issue.company ? companyLine(row.issue.company.name, Boolean(row.issue.sharedWithCompany)) : ""}
                  </Td>
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
                    {row.csat ? t.desk.queues.outOfFive(row.csat) : ""}
                  </Td>
                  <Td className="text-right text-sm whitespace-nowrap text-ink-subtle">
                    <RelativeTime iso={row.issue.createdAt} />
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}
      <Pager label={t.desk.queues.pages} total={total} offset={offset} pageSize={QUEUE_PAGE_SIZE} onChange={setOffset} />
    </Page>
  );
}
