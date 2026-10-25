import { useEffect, useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { AuthLayout } from "./auth";
import { ReportSourceProvider, narrowingKinds } from "@/api/reports";
import { sharedPdfHref, useShared } from "@/api/shares";
import { ButtonLink, ErrorBanner, IconButton, Skeleton, cx } from "@/components/ui";
import { LogoMark } from "@/components/Logo";
import { APP_NAME } from "@/config";
import { Icon } from "@/components/icons";
import { applyTheme, readTheme, type Theme } from "@/lib/theme";
import { useFormat } from "@/lib/format";
import { WidgetBody } from "@/features/dashboard/WidgetBody";
import { WidgetCard } from "@/features/dashboard/WidgetCard";
import { t } from "@/i18n";

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

// Read while rendering, so the words follow the language.
const themeWord = (theme: Theme): string => (theme === "system" ? t.shell.theme.auto : theme === "light" ? t.shell.theme.light : t.shell.theme.dark);

function SharedDashboardPage() {
  const { token } = sharedRoute.useParams();
  const { theme: forced, print } = sharedRoute.useSearch();
  const { data, error, isLoading, dataUpdatedAt } = useShared(token);
  const [theme, setTheme] = useState<Theme>(readTheme);
  const format = useFormat();

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
      <AuthLayout
        title={t.dashboard.shared.goneTitle}
        subtitle={t.dashboard.shared.goneSubtitle}
        footer={
          <Link to="/login" className="font-medium text-accent hover:underline">
            {t.dashboard.shared.signIn}
          </Link>
        }
      >
        <p className="text-sm text-ink-muted" data-share-gone="">
          {t.dashboard.shared.askNew}
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
  const updated = format.time(new Date(dataUpdatedAt || Date.now()).toISOString());

  return (
    <ReportSourceProvider value={{ share: token, readOnly: true }}>
      <div className="flex min-h-full flex-col" data-shared-dashboard={data.dashboard.name}>
        {!print && (
          <header className="flex h-12 shrink-0 items-center gap-4 border-b border-border bg-surface px-6" data-print-hide="">
            <span className="inline-flex items-center gap-2" data-shared-brand>
              <LogoMark className="rounded-control" />
              <span className="font-mono text-sm font-medium tracking-wide text-ink-muted">{APP_NAME}</span>
            </span>
            <span className="text-sm text-ink-muted">{data.projectName}</span>
            <h1 className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">{data.dashboard.name}</h1>
            <span className="text-xs text-ink-subtle tabular-nums" data-shared-updated="">
              {t.dashboard.shared.updated(updated)}
            </span>
            <ButtonLink href={sharedPdfHref(token)} download size="sm" icon={<Icon.Download />} data-action="download-pdf">
              {t.dashboard.shared.downloadPdf}
            </ButtonLink>
            <IconButton
              icon={theme === "system" ? <Icon.Monitor /> : theme === "light" ? <Icon.Sun /> : <Icon.Moon />}
              label={themeWord(theme)}
              size="sm"
              onClick={cycleTheme}
              data-action="theme"
            />
          </header>
        )}
        <main className="min-h-0 flex-1 overflow-auto">
          <div className={cx("mx-auto max-w-dashboard px-8", print ? "py-2" : "py-6")}>
            {data.share.query && (
              <p className="mb-4 text-xs text-ink-subtle" data-shared-query="">
                {t.dashboard.shared.narrowedTo} <code className="font-mono text-ink-muted">{data.share.query}</code>
              </p>
            )}
            {widgets.length === 0 ? (
              <ErrorBanner>{t.dashboard.shared.nothingOn}</ErrorBanner>
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
