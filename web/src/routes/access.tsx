import { useCallback, useId } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { GroupList } from "@/features/access/GroupList";
import { MemberList, type MembersShown } from "@/features/access/MemberList";
import { ProviderSettings } from "@/features/access/ProviderSettings";
import { SecondFactorRequirement } from "@/features/access/SecondFactorRequirement";
import { RoleMatrix } from "@/features/access/RoleMatrix";
import { RoleTable } from "@/features/access/RoleTable";
import { Page, PageHeader, SectionTitle, TabPanel, Tabs, isPageOffset } from "@/components/ui";

const tabs = [
  { id: "roles", label: "Roles" },
  { id: "matrix", label: "Matrix" },
  { id: "members", label: "Members" },
  { id: "groups", label: "Groups" },
  { id: "sign-in", label: "Single sign-on" },
] as const;

type Tab = (typeof tabs)[number]["id"];

interface AccessSearch extends MembersShown {
  tab?: Tab;
}

/** The tab, and the members' search and page, are in the address, so a link opens the same view. */
export const accessRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/access",
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
  const setTab = (next: Tab) => navigate({ to: "/settings/access", search: { tab: next === "roles" ? undefined : next } });
  const showMembers = useCallback(
    (next: MembersShown, replace = false) =>
      navigate({ to: "/settings/access", search: { tab: "members", q: next.q || undefined, offset: next.offset || undefined }, replace }),
    [navigate],
  );

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Access"
      />

      <Tabs
        label="Access"
        value={tab}
        onChange={setTab}
        panelId={panelId}
        className="mb-4"
        tabs={tabs.map((each) => ({ value: each.id, label: each.label, attrs: { "data-access-tab": each.label } }))}
      />

      <TabPanel id={panelId} label={tabs.find((each) => each.id === tab)?.label}>
        {tab === "roles" && <RoleTable />}
        {tab === "matrix" && <RoleMatrix />}
        {tab === "members" && <MemberList q={q} offset={offset} show={showMembers} />}
        {tab === "groups" && <GroupList />}
        {tab === "sign-in" && (
          <div className="space-y-4">
            <SecondFactorRequirement />
            <section>
              <SectionTitle className="mb-2">Single sign-on</SectionTitle>
              <ProviderSettings />
            </section>
          </div>
        )}
      </TabPanel>
    </Page>
  );
}
