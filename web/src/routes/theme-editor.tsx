import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useTheme } from "@/api/themes";
import { ErrorBanner, Page, PageHeader } from "@/components/ui";
import { ThemeEditor } from "@/features/themes/ThemeEditor";

export const themeNewRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/themes/new",
  component: () => <EditorPage />,
});

export const themeEditRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/themes/$themeId",
  component: () => {
    const { themeId } = themeEditRoute.useParams();
    return <EditorPage themeId={themeId} />;
  },
});

function EditorPage({ themeId }: { themeId?: string }) {
  const { data, error, isLoading } = useTheme(themeId);
  const crumb = (
    <>
      <Link to="/settings" className="hover:text-ink">
        Settings
      </Link>
      <span className="mx-1 text-ink-subtle">/</span>
      <Link to="/settings/themes" className="hover:text-ink">
        Themes
      </Link>
    </>
  );
  if (themeId && error) {
    return (
      <Page width="content">
        <PageHeader crumb={crumb} title="Theme" />
        <ErrorBanner>{(error as Error).message}</ErrorBanner>
      </Page>
    );
  }
  if (themeId && (isLoading || !data)) return null;
  return (
    <Page width="content">
      <PageHeader crumb={crumb} title={data?.theme.name ?? "New theme"} />
      <ThemeEditor key={data?.theme.id ?? "new"} theme={data?.theme} />
    </Page>
  );
}
