import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { AuthLayout } from "./auth";
import { useUnwatch } from "@/api/watchers";
import { Button, ErrorBanner } from "@/components/ui";
import { t } from "@/i18n";

/**
 * The way out that a mail carries. It needs no session, because being added
 * by somebody else must be undoable by anyone the mail reached; and it is a
 * button rather than acting on arrival, because a link must not change
 * anything just by being opened.
 */
export const unwatchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/unwatch",
  validateSearch: (search: Record<string, unknown>): { token?: string } => (typeof search.token === "string" ? { token: search.token } : {}),
  component: UnwatchPage,
});

function UnwatchPage() {
  const { token } = unwatchRoute.useSearch();
  const unwatch = useUnwatch();
  const [done, setDone] = useState(false);

  return (
    <AuthLayout
      title={t.doors.unwatch.title}
      footer={
        <Link to="/login" className="font-medium text-accent hover:underline">
          {t.doors.common.signIn}
        </Link>
      }
    >
      {done ? (
        <p className="text-sm text-ink" data-unwatched>
          {t.doors.unwatch.done}
        </p>
      ) : (
        <div className="space-y-4">
          <p className="text-sm text-ink-muted">{t.doors.unwatch.explain}</p>
          {unwatch.error && <ErrorBanner>{(unwatch.error as Error).message}</ErrorBanner>}
          <Button
            className="w-full"
            loading={unwatch.isPending}
            disabled={!token}
            onClick={() => token && unwatch.mutate(token, { onSuccess: () => setDone(true) })}
          >
            {t.doors.unwatch.submit}
          </Button>
        </div>
      )}
    </AuthLayout>
  );
}
