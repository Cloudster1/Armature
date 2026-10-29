import type { ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useAccess } from "@/api/access";
import { useLogout, useMe } from "@/api/auth";
import { useConnectedTools, type ConnectedTool } from "@/api/installation";
import { Avatar, IconButton, Menu, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { adminEntry, personalPlaces, type Place } from "./places";
import { t } from "@/i18n";

const toolIcons: Record<string, ReactNode> = { stator: <Icon.Stator />, exciter: <Icon.Exciter /> };

/** The "Switch to" group: each connected tool, a plain link to its home, under one heading. */
export function switchToItems(tools: ConnectedTool[] | undefined): MenuItem[] {
  return (tools ?? []).map((tool, i) => ({
    label: tool.name,
    icon: toolIcons[tool.product] ?? <Icon.External />,
    href: tool.url,
    section: i === 0 ? t.shell.account.switchTo : undefined,
    attrs: { "data-switch-to": tool.product },
  }));
}

/** The seat a person holds here, as the menu's header and the drawer's foot word it. */
export function roleWord(role: string | undefined): string {
  if (!role) return "";
  return t.shell.account.roles[role] ?? role[0]!.toUpperCase() + role.slice(1);
}

/**
 * What the avatar menu holds, in its order: the person's own pages, the other
 * tools, Admin for administrators and the way out. The drawer lists the same.
 */
export function useAccountItems(): MenuItem[] {
  const navigate = useNavigate();
  const logout = useLogout();
  const { data: tools } = useConnectedTools();
  const { data: access } = useAccess();
  const go = (place: Place) => navigate({ to: place.to, search: place.search as never, hash: place.hash });
  return [
    ...personalPlaces.map((place) => ({
      label: place.label,
      icon: <PlaceGlyph place={place} />,
      onSelect: () => go(place),
      attrs: { "data-account-place": place.label },
    })),
    ...switchToItems(tools),
    ...(access?.canAdministerOrg
      ? [
          {
            label: adminEntry.label,
            icon: <Icon.Settings />,
            separated: true,
            onSelect: () => navigate({ to: adminEntry.to }),
            attrs: { "data-action": "admin" },
          },
        ]
      : []),
    {
      label: t.shell.account.signOut,
      icon: <Icon.External />,
      separated: true,
      onSelect: () => logout.mutate(undefined, { onSettled: () => navigate({ to: "/login" }) }),
      attrs: { "data-action": "sign-out" },
    },
  ];
}

function PlaceGlyph({ place }: { place: Place }) {
  const Glyph = Icon[place.icon];
  return <Glyph />;
}

/** The person the menu belongs to: name, address and seat. */
function AccountHeader() {
  const { data } = useMe();
  const principal = data?.principal;
  return (
    <div className="mb-1 flex items-center gap-2 border-b border-border px-2 pt-1 pb-2" data-account-header>
      <Avatar name={principal?.user.name ?? ""} src={principal?.user.avatarUrl} size="md" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium text-ink">{principal?.user.name}</span>
        <span className="block truncate text-xs text-ink-muted">{principal?.user.email}</span>
        <span className="block text-2xs text-ink-subtle" data-account-role>
          {roleWord(principal?.role)}
        </span>
      </span>
    </div>
  );
}

/** The avatar's menu, "Your account", which opens to the right of the rail whether the sidebar is open or folded. */
export function AccountMenu({ userName, avatar }: { userName: string; avatar?: string }) {
  const items = useAccountItems();
  return (
    <Menu
      label={t.shell.account.yours}
      align="start"
      side="right"
      header={<AccountHeader />}
      trigger={(props) => (
        <IconButton
          icon={<Avatar name={userName} src={avatar} size="sm" />}
          label={t.shell.account.yours}
          size="sm"
          onClick={props.toggle}
          aria-haspopup={props["aria-haspopup"]}
          aria-expanded={props["aria-expanded"]}
          className="rounded-full"
          data-action="account"
        />
      )}
      items={items}
    />
  );
}
