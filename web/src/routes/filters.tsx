import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { useDeleteSavedFilter, useSavedFilters, useStarFilter, useSubscribeFilter, useUpdateSavedFilter, type SavedFilter } from "@/api/filters";
import { EmptyState, IconButton, Menu, Page, PageHeader, Segmented, SelectInput, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";

type View = "mine" | "shared" | "starred";

/** Every saved search the reader may see, and what each does for them. */
export const filtersRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/filters",
  component: FiltersPage,
});

/** Which filters a view shows. */
export function inFilterView(f: SavedFilter, view: View, me: string | undefined): boolean {
  if (view === "starred") return f.starred;
  if (view === "mine") return f.ownerId === me;
  return f.shared && f.ownerId !== me;
}

function FiltersPage() {
  const { data, isLoading } = useSavedFilters();
  const { data: meData } = useMe();
  const me = meData?.principal?.user.id;
  const star = useStarFilter();
  const update = useUpdateSavedFilter();
  const remove = useDeleteSavedFilter();
  const subscribe = useSubscribeFilter();
  const confirm = useConfirm();
  const toast = useToast();
  const [view, setView] = useState<View>("mine");
  const filters = (data?.filters ?? []).filter((f) => inFilterView(f, view, me));

  return (
    <Page width="content">
      <PageHeader
        title="Saved searches"
        meta="A query with a name: yours, shared, or starred; mailed to you on a schedule if you like"
        actions={
          <Link to="/search" search={{ q: "" }} className="text-sm text-accent hover:underline">
            New search
          </Link>
        }
      />
      <div className="mb-4">
        <Segmented<View>
          label="Show"
          value={view}
          onChange={setView}
          options={[
            { value: "mine", label: "Mine", attrs: { "data-filters-view": "mine" } },
            { value: "shared", label: "Shared with me", attrs: { "data-filters-view": "shared" } },
            { value: "starred", label: "Starred", attrs: { "data-filters-view": "starred" } },
          ]}
        />
      </div>
      {isLoading ? null : filters.length === 0 ? (
        <EmptyState title={view === "mine" ? "Nothing saved yet" : view === "shared" ? "Nothing shared with you" : "Nothing starred"} description="Run a search, then Save this search under the query. A saved search can be shared, starred and mailed to you daily or weekly." />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Query</Th>
              <Th>Owner</Th>
              <Th>Mail me</Th>
              <Th className="w-24" />
            </tr>
          </thead>
          <tbody>
            {filters.map((f) => (
              <tr key={f.id} data-filter-row={f.name}>
                <Td>
                  <Link to="/search" search={{ q: f.query, f: f.id }} className="font-medium text-ink hover:text-accent">
                    {f.name}
                  </Link>
                  {f.shared && <Tag className="ml-2">Shared</Tag>}
                </Td>
                <Td className="max-w-sm truncate font-mono text-xs text-ink-muted" title={f.query}>
                  {f.query}
                </Td>
                <Td className="text-sm text-ink-muted">{f.ownerId === me ? "you" : f.ownerName}</Td>
                <Td>
                  <label htmlFor={`field-subscription-${f.id}`} className="sr-only">
                    Mail me {f.name}
                  </label>
                  <SelectInput
                    id={`field-subscription-${f.id}`}
                    controlSize="sm"
                    value={f.subscription?.schedule ?? ""}
                    onChange={(e) => {
                      const schedule = e.target.value as "" | "daily" | "weekly";
                      subscribe.mutate({ id: f.id, subscription: schedule ? { schedule, hour: 8, weekday: schedule === "weekly" ? 1 : undefined } : null }, { onSuccess: () => toast.success(schedule ? `You will get ${f.name} ${schedule}` : "No more mail") });
                    }}
                    data-filter-subscription={f.name}
                  >
                    <option value="">Never</option>
                    <option value="daily">Daily at 08:00</option>
                    <option value="weekly">Mondays at 08:00</option>
                  </SelectInput>
                </Td>
                <Td>
                  <span className="flex items-center justify-end gap-1">
                    <IconButton icon={f.starred ? <Icon.Check /> : <Icon.Plus />} label={f.starred ? `Unstar ${f.name}` : `Star ${f.name}`} size="sm" onClick={() => star.mutate({ id: f.id, on: !f.starred })} data-filter-star={f.name} data-starred={f.starred ? "true" : "false"} />
                    {f.ownerId === me && (
                      <Menu
                        label={`Actions for ${f.name}`}
                        align="end"
                        trigger={(props) => <IconButton icon={<Icon.More />} label={`Actions for ${f.name}`} size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} data-filter-menu={f.name} />}
                        items={[
                          { label: f.shared ? "Stop sharing" : "Share with the organization", icon: <Icon.Share />, onSelect: () => update.mutate({ id: f.id, shared: !f.shared }), attrs: { "data-action": "filter-share" } },
                          {
                            label: "Delete",
                            icon: <Icon.Trash />,
                            danger: true,
                            onSelect: async () => (await confirm({ noun: "saved search", verb: "Delete", body: `${f.name} goes for everyone it was shared with.` })) && remove.mutate(f.id),
                            attrs: { "data-action": "filter-delete" },
                          },
                        ]}
                      />
                    )}
                  </span>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Page>
  );
}
