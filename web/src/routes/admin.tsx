import { Link, Outlet, createRoute, notFound, redirect, useLocation, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { accessQueryOptions, useAccess } from "@/api/access";
import { useMe } from "@/api/auth";
import { Button, Card, EmptyState, Menu, Page, PageHeader, SectionTitle, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { NavItem } from "@/features/shell/nav";
import { useNarrow } from "@/features/shell/state";
import { ADMIN_PATH, ADMIN_SECTIONS, adminPlaces, placeKey, type AdminPlace, type AdminSection } from "@/features/shell/places";
import { t } from "@/i18n";

/**
 * Everything an installation administrator sets, on one page. It waits for the
 * person's access before it draws, and a member never mounts a child.
 */
export const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  path: ADMIN_PATH,
  beforeLoad: async ({ context }) => {
    await context.queryClient.ensureQueryData(accessQueryOptions());
  },
  component: AdminLayout,
});

export const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/",
  component: AdminIndex,
});

/** The /settings sections that moved to /admin, with the one whose name changed on the way; a page born under /admin is reached the same way. */
export const MOVED_SECTIONS: Record<string, string> = {
  installation: "installation",
  organization: "installation",
  access: "access",
  users: "users",
  holidays: "holidays",
  workflows: "workflows",
  "issue-types": "issue-types",
  "project-templates": "project-templates",
  labels: "labels",
  automation: "automation",
  fields: "fields",
  "issue-view": "issue-view",
  "connected-apps": "connected-apps",
  webhooks: "webhooks",
  audit: "audit",
};

/** Where an old /settings address now lives, or undefined for one that never moved. */
export function movedAddress(section: string, rest = "", searchStr = "", hash = ""): string | undefined {
  const moved = Object.hasOwn(MOVED_SECTIONS, section) ? MOVED_SECTIONS[section] : undefined;
  if (!moved) return undefined;
  return `${ADMIN_PATH}/${moved}${rest ? `/${rest}` : ""}${searchStr}${hash ? `#${hash}` : ""}`;
}

function sendOn(section: string, rest: string | undefined, location: { searchStr: string; hash: string }): never {
  const to = movedAddress(section, rest, location.searchStr, location.hash);
  if (!to) throw notFound();
  throw redirect({ href: to, replace: true });
}

/** An old /settings/<moved> address, sent on with its search and hash; any other unknown one is not found. */
export const settingsMovedRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/$section",
  beforeLoad: ({ params, location }) => sendOn(params.section, undefined, location),
});

export const settingsMovedDeepRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/$section/$",
  beforeLoad: ({ params, location }) => sendOn(params.section, params._splat, location),
});

function bySection(): Array<{ section: AdminSection; places: AdminPlace[] }> {
  return ADMIN_SECTIONS.map((section) => ({ section, places: adminPlaces.filter((place) => place.section === section) }));
}

function AdminLayout() {
  const { data: access } = useAccess();
  const narrow = useNarrow();
  if (!access?.canAdministerOrg) {
    return (
      <Page width="narrow">
        <EmptyState title={t.shell.admin.refusedTitle} description={t.shell.admin.refusedBody} icon={<Icon.Shield />} />
      </Page>
    );
  }
  return (
    <div className={narrow ? "flex flex-col" : "flex items-start gap-6"} data-admin-area>
      {narrow ? <AdminMenu /> : <AdminNav />}
      <div className="min-w-0 flex-1">
        <Outlet />
      </div>
    </div>
  );
}

/** The sections and their pages as a list beside the page, for wide screens. */
function AdminNav() {
  return (
    <nav aria-label={t.shell.admin.navLabel} className="sticky top-4 w-56 shrink-0 py-2" data-admin-nav>
      <NavItem to={ADMIN_PATH} exact icon="Settings" rail={false} data-admin-link="Admin">
        {t.shell.admin.allSections}
      </NavItem>
      {bySection().map(({ section, places }) => (
        <div key={section} className="mt-3">
          <SectionTitle className="mb-1 px-2">{t.shell.admin.sections[section]}</SectionTitle>
          {places.map((place) => (
            <NavItem key={place.to} to={place.to} icon={place.icon} rail={false} data-admin-link={place.label}>
              {place.label}
            </NavItem>
          ))}
        </div>
      ))}
    </nav>
  );
}

/** The same list behind one button, for narrow screens. */
function AdminMenu() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const current = adminPlaces.find((place) => pathname === place.to || pathname.startsWith(`${place.to}/`));
  const items: MenuItem[] = [
    { label: t.shell.admin.allSections, icon: <Icon.Settings />, onSelect: () => navigate({ to: ADMIN_PATH }), attrs: { "data-admin-link": "Admin" } },
    ...bySection().flatMap(({ section, places }) =>
      places.map((place, i) => {
        const Glyph = Icon[place.icon];
        return {
          label: place.label,
          icon: <Glyph />,
          section: i === 0 ? t.shell.admin.sections[section] : undefined,
          onSelect: () => navigate({ to: place.to as never }),
          attrs: { "data-admin-link": place.label },
        };
      }),
    ),
  ];
  return (
    <div className="mb-2" data-admin-menu>
      <Menu
        label={t.shell.admin.menuLabel}
        items={items}
        trigger={(props) => (
          <Button
            variant="secondary"
            size="sm"
            iconRight={<Icon.ChevronDown />}
            onClick={props.toggle}
            aria-haspopup={props["aria-haspopup"]}
            aria-expanded={props["aria-expanded"]}
            data-action="admin-sections"
          >
            {current ? current.label : t.shell.admin.title}
          </Button>
        )}
      />
    </div>
  );
}

/** The doors to every section, as a card each, grouped the way the navigation groups them. */
function AdminIndex() {
  const { data } = useMe();
  return (
    <Page width="narrow">
      <PageHeader title={t.shell.admin.title} meta={data?.principal?.installation?.name} />
      {bySection().map(({ section, places }) => (
        <section key={section} className="mb-8" data-admin-section={section}>
          <SectionTitle className="mb-2">{t.shell.admin.sections[section]}</SectionTitle>
          <Card className="divide-y divide-border">
            {places.map((place) => {
              const Glyph = Icon[place.icon];
              return (
                <Link key={placeKey(place)} to={place.to} className="flex items-center gap-3 px-4 py-3 hover:bg-surface-raised" data-admin-card={place.label}>
                  <Glyph className="shrink-0 text-ink-muted" />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-ink">{place.label}</span>
                    <span className="block text-sm text-ink-muted">{place.detail}</span>
                  </span>
                  <Icon.ChevronRight className="shrink-0 text-ink-subtle" />
                </Link>
              );
            })}
          </Card>
        </section>
      ))}
    </Page>
  );
}
