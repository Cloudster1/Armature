import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { EmptyState, Page, PageHeader } from "@/components/ui";
import { NewUserForm } from "@/features/users/NewUserForm";
import { UserTable } from "@/features/users/UserTable";

export const usersRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/users",
  component: UsersPage,
});

/** The organization's own accounts: made here with a password, and managed here. */
function UsersPage() {
  const { data: access } = useAccess();
  const canAdminister = access?.canAdministerOrg ?? false;

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Users"
        meta="Accounts that sign in here with a password. Somebody with an account elsewhere is invited instead, under Access."
      />
      {!access ? null : !canAdminister ? (
        <EmptyState title="Administrators only" description="Only a global administrator makes and manages accounts." />
      ) : (
        <div className="space-y-4">
          <NewUserForm />
          <UserTable />
        </div>
      )}
    </Page>
  );
}
