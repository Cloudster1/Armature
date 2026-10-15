import { useCallback } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { EmptyState, Page, PageHeader, isPageOffset } from "@/components/ui";
import { JoinRequests } from "@/features/users/JoinRequests";
import { NewUserForm } from "@/features/users/NewUserForm";
import { UserTable, type UsersShown } from "@/features/users/UserTable";

/** The search and the page are in the address, so a link reopens the same list. */
export const usersRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/users",
  validateSearch: (search: Record<string, unknown>): UsersShown => ({
    // A search of digits alone comes back from the address as a number.
    ...(typeof search.q === "number" ? { q: String(search.q) } : typeof search.q === "string" && search.q !== "" ? { q: search.q } : {}),
    ...(isPageOffset(search.offset) ? { offset: search.offset } : {}),
  }),
  component: UsersPage,
});

/** The organization's own accounts: made here with a password, and managed here. */
function UsersPage() {
  const { data: access } = useAccess();
  const canAdminister = access?.canAdministerOrg ?? false;
  const { q, offset } = usersRoute.useSearch();
  const navigate = useNavigate();
  const show = useCallback(
    (next: UsersShown, replace = false) => navigate({ to: "/settings/users", search: { q: next.q || undefined, offset: next.offset || undefined }, replace }),
    [navigate],
  );

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
          <JoinRequests />
          <NewUserForm />
          <UserTable q={q} offset={offset} show={show} />
        </div>
      )}
    </Page>
  );
}
