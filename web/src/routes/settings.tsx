import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { Card, Page, PageHeader, SectionTitle } from "@/components/ui";
import { Icon, type IconName } from "@/components/icons";

/** One door to everything that is set up rather than worked on: yours, then the organization's. */
export const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings",
  component: SettingsHub,
});

const you: Array<{ to: string; label: string; detail: string; icon: IconName }> = [
  { to: "/settings/profile", label: "Profile", detail: "Your name, your picture, and how dates are written for you.", icon: "User" },
  { to: "/settings/tokens", label: "API tokens", detail: "Call the API from a script or a CI job.", icon: "Command" },
];

const organization: Array<{ to: string; label: string; detail: string; icon: IconName }> = [
  { to: "/settings/organization", label: "Organization", detail: "Its name and address, and deleting it with everything in it.", icon: "Settings" },
  { to: "/settings/access", label: "Access", detail: "Who holds which role, the groups, and the identity provider.", icon: "Key" },
  { to: "/settings/workflows", label: "Workflows", detail: "The organization's workflows and the schemes that hand them to issue types.", icon: "Workflow" },
  { to: "/settings/labels", label: "Labels", detail: "Words shared by every project.", icon: "Tag" },
  { to: "/settings/automation", label: "Automation", detail: "Rules that watch every project: when something happens, check it, do things.", icon: "Bolt" },
  { to: "/settings/webhooks", label: "Webhooks", detail: "Where events are posted, signed, with a log of every try.", icon: "Hook" },
  { to: "/settings/fields", label: "Fields", detail: "What every project records about an issue beyond the standard fields.", icon: "Field" },
  { to: "/settings/audit", label: "Audit log", detail: "Who did what to the organization, kept for a year.", icon: "Shield" },
];

function SettingsHub() {
  const { data } = useMe();
  return (
    <Page width="narrow">
      <PageHeader title="Settings" meta={data?.principal?.org?.name} />
      <Group title="You" links={you} />
      <Group title="Organization" links={organization} />
    </Page>
  );
}

function Group({ title, links }: { title: string; links: typeof you }) {
  return (
    <section className="mb-8">
      <SectionTitle className="mb-2">{title}</SectionTitle>
      <Card className="divide-y divide-border">
        {links.map((link) => {
          const Glyph = Icon[link.icon];
          return (
            <Link key={link.to} to={link.to} className="flex items-center gap-3 px-4 py-3 hover:bg-surface-raised" data-settings-link={link.label}>
              <Glyph className="shrink-0 text-ink-muted" />
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium text-ink">{link.label}</span>
                <span className="block text-sm text-ink-muted">{link.detail}</span>
              </span>
              <Icon.ChevronRight className="shrink-0 text-ink-subtle" />
            </Link>
          );
        })}
      </Card>
    </section>
  );
}
