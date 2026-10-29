import { useCallback, useId } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { adminRoute } from "./admin";
import { GroupList } from "@/features/access/GroupList";
import { MemberList, type MembersShown } from "@/features/access/MemberList";
import { ProviderSettings } from "@/features/access/ProviderSettings";
import { SecondFactorRequirement } from "@/features/access/SecondFactorRequirement";
import { RoleMatrix } from "@/features/access/RoleMatrix";
import { RoleTable } from "@/features/access/RoleTable";
import { Page, PageHeader, SectionTitle, TabPanel, Tabs, isPageOffset } from "@/components/ui";
import { t } from "@/i18n";

const tabs = [{ id: "roles" }, { id: "matrix" }, { id: "members" }, { id: "groups" }, { id: "sign-in" }] as const;

type Tab = (typeof tabs)[number]["id"];

// The tests find a tab by these words, whatever language the page speaks.
const tabHooks: Record<Tab, string> = { roles: "Roles", matrix: "Matrix", members: "Members", groups: "Groups", "sign-in": "Single sign-on" };

const tabLabel = (tab: Tab): string =>
  tab === "roles"
    ? t.access.tabs.roles
    : tab === "matrix"
      ? t.access.tabs.matrix
      : tab === "members"
        ? t.access.tabs.members
        : tab === "groups"
          ? t.access.tabs.groups
          : t.access.tabs.signIn;

interface AccessSearch extends MembersShown {
  tab?: Tab;
}

/** The tab, and the members' search and page, are in the address, so a link opens the same view. */
export const accessRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/access",
  validateSearch: (search: Record<string, unknown>): AccessSearch => ({
    ...(tabs.some((each) => each.id === search.tab && each.id !== "roles") ? { tab: search.tab as Tab } : {}),
    // A search of digits alone comes back from the address as a number.
    ...(typeof search.q === "number" ? { q: String(search.q) } : typeof search.q === "string" && search.q !== "" ? { q: search.q } : {}),
    ...(isPageOffset(search.offset) ? { offset: search.offset } : {}),
  }),
  component: AccessPage,
});

function AccessPage() {
  const { tab = "roles", q, offset } = accessRoute.useSearch();
  const navigate = useNavigate();
  const panelId = useId();
  const setTab = (next: Tab) => navigate({ to: "/admin/access", search: { tab: next === "roles" ? undefined : next } });
  const showMembers = useCallback(
    (next: MembersShown, replace = false) =>
      navigate({ to: "/admin/access", search: { tab: "members", q: next.q || undefined, offset: next.offset || undefined }, replace }),
    [navigate],
  );

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/admin" className="hover:text-ink">
            {t.shell.admin.crumb}
          </Link>
        }
        title={t.access.title}
      />

      <Tabs
        label={t.access.title}
        value={tab}
        onChange={setTab}
        panelId={panelId}
        className="mb-4"
        tabs={tabs.map((each) => ({ value: each.id, label: tabLabel(each.id), attrs: { "data-access-tab": tabHooks[each.id] } }))}
      />

      <TabPanel id={panelId} label={tabLabel(tab)}>
        {tab === "roles" && <RoleTable />}
        {tab === "matrix" && <RoleMatrix />}
        {tab === "members" && <MemberList q={q} offset={offset} show={showMembers} />}
        {tab === "groups" && <GroupList />}
        {tab === "sign-in" && (
          <div className="space-y-4">
            <SecondFactorRequirement />
            <section>
              <SectionTitle className="mb-2">{t.access.tabs.signIn}</SectionTitle>
              <ProviderSettings />
            </section>
          </div>
        )}
      </TabPanel>
    </Page>
  );
}
