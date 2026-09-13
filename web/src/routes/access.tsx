import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { GroupList } from "@/features/access/GroupList";
import { MemberList } from "@/features/access/MemberList";
import { ProviderSettings } from "@/features/access/ProviderSettings";
import { RoleTable } from "@/features/access/RoleTable";
import { Page, PageHeader, Tabs } from "@/components/ui";

export const accessRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/access",
  component: AccessPage,
});

const tabs = [
  { id: "roles", label: "Roles" },
  { id: "members", label: "Members" },
  { id: "groups", label: "Groups" },
  { id: "sso", label: "Single sign-on" },
] as const;

function AccessPage() {
  const [tab, setTab] = useState<(typeof tabs)[number]["id"]>("roles");

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Access" />

      <Tabs label="Access" value={tab} onChange={setTab} className="mb-4" tabs={tabs.map((each) => ({ value: each.id, label: each.label, attrs: { "data-access-tab": each.label } }))} />

      {tab === "roles" && <RoleTable />}
      {tab === "members" && <MemberList />}
      {tab === "groups" && <GroupList />}
      {tab === "sso" && <ProviderSettings />}
    </Page>
  );
}
