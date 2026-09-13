import { useEffect, useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { AuthLayout } from "./auth";
import { ReportSourceProvider, narrowingKinds } from "@/api/reports";
import { sharedPdfHref, useShared } from "@/api/shares";
import { ButtonLink, ErrorBanner, IconButton, Skeleton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { applyTheme, readTheme, type Theme } from "@/lib/theme";
import { WidgetBody } from "@/features/dashboard/WidgetBody";
import { WidgetCard } from "@/features/dashboard/WidgetCard";

/**
 * A dashboard opened by a link, with no sign-in and nothing to press: the
 * arrangement as it was shared, the numbers as they are now, on a screen
 * that may hang in a lobby for months.
 */
export const sharedRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/shared/$token",
  validateSearch: (search: Record<string, unknown>): { theme?: "light"; print?: boolean } => ({
    theme: search.theme === "light" ? "light" : undefined,
    print: search.print === true || search.print === 1 || search.print === "1" ? true : undefined,
  }),
  component: SharedDashboardPage,
});

const themeLabels: Record<Theme, string> = { system: "Auto", light: "Light", dark: "Dark" };

function SharedDashboardPage() {
  const { token } = sharedRoute.useParams();
  const { theme: forced, print } = sharedRoute.useSearch();
  const { data, error, isLoading, dataUpdatedAt } = useShared(token);
  const [theme, setTheme] = useState<Theme>(readTheme);

  // A renderer asks for the light theme and gets it whatever this browser remembers.
  useEffect(() => {
    if (forced) applyTheme(forced);
  }, [forced]);

  function cycleTheme() {
    const next: Theme = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
    setTheme(next);
    applyTheme(next);
  }

  if (error) {
    return (
      <AuthLayout title="This link is no longer valid." subtitle="The dashboard it opened may have been unshared, or the link expired." footer={<Link to="/login" className="font-medium text-accent hover:underline">Sign in</Link>}>
        <p className="text-sm text-ink-muted" data-share-gone="">
          Ask whoever sent it for a new one.
        </p>
      </AuthLayout>
    );
  }
  if (isLoading || !data) {
    return (
      <div className="px-8 py-6">
        <Skeleton lines={6} />
      </div>
    );
  }
  const narrows = narrowingKinds(data.kinds);
  const widgets = data.dashboard.widgets.filter((w) => w.kind !== "filter");
  const updated = new Date(dataUpdatedAt || Date.now()).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });

  return (
    <ReportSourceProvider value={{ share: token, readOnly: true }}>
      <div className="flex min-h-full flex-col" data-shared-dashboard={data.dashboard.name}>
        {!print && (
          <header className="flex h-12 shrink-0 items-center gap-4 border-b border-border bg-surface px-6" data-print-hide="">
            <span className="font-mono text-sm font-medium tracking-wide text-ink-muted">armature</span>
            <span className="text-sm text-ink-muted">{data.projectName}</span>
            <h1 className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">{data.dashboard.name}</h1>
            <span className="text-xs text-ink-subtle tabular-nums" data-shared-updated="">
              Updated {updated}
            </span>
            <ButtonLink href={sharedPdfHref(token)} download size="sm" icon={<Icon.Download />} data-action="download-pdf">
              Download PDF
            </ButtonLink>
            <IconButton icon={theme === "system" ? <Icon.Monitor /> : theme === "light" ? <Icon.Sun /> : <Icon.Moon />} label={themeLabels[theme]} size="sm" onClick={cycleTheme} data-action="theme" />
          </header>
        )}
        <main className="min-h-0 flex-1 overflow-auto">
          <div className={cx("mx-auto max-w-[72rem] px-8", print ? "py-2" : "py-6")}>
            {data.share.query && (
              <p className="mb-4 text-xs text-ink-subtle" data-shared-query="">
                Narrowed to <code className="font-mono text-ink-muted">{data.share.query}</code>
              </p>
            )}
            {widgets.length === 0 ? (
              <ErrorBanner>This dashboard has nothing on it yet.</ErrorBanner>
            ) : (
              <div className="grid gap-4 md:grid-cols-2" data-testid="dashboard">
                {widgets.map((widget) => (
                  <WidgetCard key={widget.id} widget={widget} notNarrowed={Boolean(data.share.query) && !narrows.has(widget.kind)}>
                    <WidgetBody projectKey={data.dashboard.projectKey} widget={widget} narrow={undefined} />
                  </WidgetCard>
                ))}
              </div>
            )}
          </div>
        </main>
      </div>
    </ReportSourceProvider>
  );
}
