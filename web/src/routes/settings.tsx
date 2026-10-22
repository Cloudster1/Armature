import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { useMe } from "@/api/auth";
import { Card, Page, PageHeader, SectionTitle } from "@/components/ui";
import { Icon } from "@/components/icons";
import { personalPlaces, placeKey, settingsFor, type Place } from "@/features/shell/places";

/** One door to everything that is set up rather than worked on: yours, then the installation's for its administrators. */
export const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings",
  component: SettingsHub,
});

function SettingsHub() {
  const { data } = useMe();
  const { data: access } = useAccess();
  const settings = settingsFor(access?.canAdministerOrg ?? false);
  return (
    <Page width="narrow">
      <PageHeader title="Settings" meta={data?.principal?.installation?.name} />
      <Group title="You" places={personalPlaces} />
      {settings.length > 0 && <Group title="Settings" places={settings} />}
    </Page>
  );
}

function Group({ title, places }: { title: string; places: Place[] }) {
  return (
    <section className="mb-8" data-settings-group={title}>
      <SectionTitle className="mb-2">{title}</SectionTitle>
      <Card className="divide-y divide-border">
        {places.map((place) => {
          const Glyph = Icon[place.icon];
          return (
            <Link key={placeKey(place)} to={place.to} search={place.search as never} hash={place.hash} className="flex items-center gap-3 px-4 py-3 hover:bg-surface-raised" data-settings-link={place.label}>
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
  );
}
