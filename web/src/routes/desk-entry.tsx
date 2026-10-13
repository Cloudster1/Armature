import { useEffect } from "react";
import { Link, createRoute, redirect } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { rootRoute } from "./root";
import { AuthLayout } from "./auth";
import { meQueryKey, useMe } from "@/api/auth";
import { useDeskEntry } from "@/api/desk";
import { EmptyState } from "@/components/ui";
import { DeskEntryForm, OpenDoorForm } from "@/features/desk/DeskEntryForm";
import { safePath } from "@/lib/path";

type DoorSearch = { next?: string; desk?: string };

function doorSearch(search: Record<string, unknown>): DoorSearch {
  // Only a path of this application is followed; a mail must not be able to
  // send a reader elsewhere.
  const next = safePath(search.next);
  const desk = typeof search.desk === "string" && search.desk ? search.desk : undefined;
  return { ...(next ? { next } : {}), ...(desk ? { desk } : {}) };
}

/**
 * The portal's public door, named by the desk in its address, or the
 * installation's when it names none. A visitor with a session is sent on at
 * once; anyone else proves a mail address and becomes a customer.
 */
export const deskEntryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/desk",
  validateSearch: doorSearch,
  component: DeskEntryPage,
});

/** The address that named an organization, sent on to the desk's own until the next release. */
export const deskEntryMovedRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/desk/$orgSlug",
  validateSearch: doorSearch,
  beforeLoad: ({ search }) => {
    throw redirect({ to: "/desk", search, replace: true });
  },
});

function DeskEntryPage() {
  const { next, desk: deskKey } = deskEntryRoute.useSearch();
  const queryClient = useQueryClient();
  const me = useMe();
  const door = useDeskEntry(deskKey);
  const destination = next ?? "/portal";

  if (me.data?.principal) return <Forward to={destination} />;
  if (me.isLoading || door.isLoading) return null;

  if (door.error || !door.data) {
    return (
      <AuthLayout
        title="No desk here"
        footer={
          <Link to="/login" className="font-medium text-accent hover:underline">
            Sign in instead
          </Link>
        }
      >
        <EmptyState title="There is no service desk at this address" description="Check the link you were given, or ask whoever gave it to you." />
      </AuthLayout>
    );
  }

  // A desk named in the address whose door is open takes a name and an
  // address; every other way in is the code.
  const desk = door.data.desk;
  const name = desk?.name ?? door.data.installation;
  const onEntered = async () => {
    await queryClient.invalidateQueries({ queryKey: meQueryKey });
    window.location.assign(destination);
  };

  return (
    <AuthLayout
      title={name}
      subtitle="Raise a request, or follow one you raised."
      footer={
        <>
          Have an account?{" "}
          <Link to="/login" className="font-medium text-accent hover:underline">
            Sign in
          </Link>
        </>
      }
    >
      {desk?.open ? <OpenDoorForm door={desk} onEntered={onEntered} /> : <DeskEntryForm desk={desk?.key} deskName={name} onEntered={onEntered} />}
    </AuthLayout>
  );
}

/**
 * Leaves for a path the mail named. It is a whole navigation rather than a
 * router link because the destination is text from a search parameter, which
 * the router's typed links cannot take.
 */
function Forward({ to }: { to: string }) {
  useEffect(() => {
    window.location.replace(to);
  }, [to]);
  return null;
}
