import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { AuthLayout } from "./auth";
import { useUnwatch } from "@/api/watchers";
import { Button, ErrorBanner } from "@/components/ui";

/**
 * The way out that a mail carries. It needs no session, because being added
 * by somebody else must be undoable by anyone the mail reached; and it is a
 * button rather than acting on arrival, because a link must not change
 * anything just by being opened.
 */
export const unwatchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/unwatch",
  validateSearch: (search: Record<string, unknown>): { token?: string } =>
    typeof search.token === "string" ? { token: search.token } : {},
  component: UnwatchPage,
});

function UnwatchPage() {
  const { token } = unwatchRoute.useSearch();
  const unwatch = useUnwatch();
  const [done, setDone] = useState(false);

  return (
    <AuthLayout
      title="Stop following"
      footer={
        <Link to="/login" className="font-medium text-accent hover:underline">
          Sign in
        </Link>
      }
    >
      {done ? (
        <p className="text-sm text-ink" data-unwatched>
          You will hear no more about this request.
        </p>
      ) : (
        <div className="space-y-4">
          <p className="text-sm text-ink-muted">Somebody added you to follow a request. Press the button and the mails stop.</p>
          {unwatch.error && <ErrorBanner>{(unwatch.error as Error).message}</ErrorBanner>}
          <Button className="w-full" loading={unwatch.isPending} disabled={!token} onClick={() => token && unwatch.mutate(token, { onSuccess: () => setDone(true) })}>
            Stop following
          </Button>
        </div>
      )}
    </AuthLayout>
  );
}
