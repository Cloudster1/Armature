import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { useDeleteSavedFilter, useSavedFilters, useStarFilter, useSubscribeFilter, useUpdateSavedFilter, type SavedFilter } from "@/api/filters";
import { EmptyState, IconButton, Menu, Page, PageHeader, Segmented, SelectInput, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

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
        title={t.search.filters.title}
        meta={t.search.filters.meta}
        actions={
          <Link to="/search" search={{ q: "" }} className="text-sm text-accent hover:underline">
            {t.search.filters.newSearch}
          </Link>
        }
      />
      <div className="mb-4">
        <Segmented<View>
          label={t.search.filters.show}
          value={view}
          onChange={setView}
          options={[
            { value: "mine", label: t.search.filters.mine, attrs: { "data-filters-view": "mine" } },
            { value: "shared", label: t.search.filters.sharedWithMe, attrs: { "data-filters-view": "shared" } },
            { value: "starred", label: t.search.filters.starred, attrs: { "data-filters-view": "starred" } },
          ]}
        />
      </div>
      {isLoading ? null : filters.length === 0 ? (
        <EmptyState
          title={view === "mine" ? t.search.filters.nothingSaved : view === "shared" ? t.search.filters.nothingShared : t.search.filters.nothingStarred}
          description={t.search.filters.emptyHint}
        />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>{t.search.filters.name}</Th>
              <Th>{t.search.filters.query}</Th>
              <Th>{t.search.filters.owner}</Th>
              <Th>{t.search.filters.mailMe}</Th>
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
                  {f.shared && <Tag className="ml-2">{t.search.filters.shared}</Tag>}
                </Td>
                <Td className="max-w-sm truncate font-mono text-xs text-ink-muted" title={f.query}>
                  {f.query}
                </Td>
                <Td className="text-sm text-ink-muted">{f.ownerId === me ? t.search.filters.you : f.ownerName}</Td>
                <Td>
                  <label htmlFor={`field-subscription-${f.id}`} className="sr-only">
                    {t.search.filters.mailMeFor(f.name)}
                  </label>
                  <SelectInput
                    id={`field-subscription-${f.id}`}
                    controlSize="sm"
                    value={f.subscription?.schedule ?? ""}
                    onChange={(e) => {
                      const schedule = e.target.value as "" | "daily" | "weekly";
                      subscribe.mutate(
                        { id: f.id, subscription: schedule ? { schedule, hour: 8, weekday: schedule === "weekly" ? 1 : undefined } : null },
                        {
                          onSuccess: () =>
                            toast.success(
                              schedule ? t.search.filters.willGet(f.name, t.search.filters.schedules[schedule] ?? schedule) : t.search.filters.noMoreMail,
                            ),
                        },
                      );
                    }}
                    data-filter-subscription={f.name}
                  >
                    <option value="">{t.search.filters.never}</option>
                    <option value="daily">{t.search.filters.daily}</option>
                    <option value="weekly">{t.search.filters.weekly}</option>
                  </SelectInput>
                </Td>
                <Td>
                  <span className="flex items-center justify-end gap-1">
                    <IconButton
                      icon={f.starred ? <Icon.Check /> : <Icon.Plus />}
                      label={f.starred ? t.search.saved.unstar(f.name) : t.search.saved.star(f.name)}
                      size="sm"
                      onClick={() => star.mutate({ id: f.id, on: !f.starred })}
                      data-filter-star={f.name}
                      data-starred={f.starred ? "true" : "false"}
                    />
                    {f.ownerId === me && (
                      <Menu
                        label={t.search.filters.actionsFor(f.name)}
                        align="end"
                        trigger={(props) => (
                          <IconButton
                            icon={<Icon.More />}
                            label={t.search.filters.actionsFor(f.name)}
                            size="sm"
                            onClick={props.toggle}
                            aria-haspopup={props["aria-haspopup"]}
                            aria-expanded={props["aria-expanded"]}
                            data-filter-menu={f.name}
                          />
                        )}
                        items={[
                          {
                            label: f.shared ? t.search.filters.stopSharing : t.search.filters.shareWithEveryone,
                            icon: <Icon.Share />,
                            onSelect: () => update.mutate({ id: f.id, shared: !f.shared }),
                            attrs: { "data-action": "filter-share" },
                          },
                          {
                            label: t.search.filters.delete,
                            icon: <Icon.Trash />,
                            danger: true,
                            onSelect: async () =>
                              (await confirm({
                                noun: t.kit.confirm.things.savedSearch,
                                verb: t.kit.confirm.verbs.delete,
                                body: t.search.filters.deleteBody(f.name),
                              })) && remove.mutate(f.id),
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
