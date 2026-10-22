import type { ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useLogout } from "@/api/auth";
import { useConnectedTools, type ConnectedTool } from "@/api/installation";
import { Avatar, IconButton, Menu, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";

const toolIcons: Record<string, ReactNode> = { stator: <Icon.Stator />, exciter: <Icon.Exciter /> };

/** The "Switch to" group: each connected tool, a plain link to its home, under one heading. */
export function switchToItems(tools: ConnectedTool[] | undefined): MenuItem[] {
  return (tools ?? []).map((tool, i) => ({
    label: tool.name,
    icon: toolIcons[tool.product] ?? <Icon.External />,
    href: tool.url,
    section: i === 0 ? "Switch to" : undefined,
    attrs: { "data-switch-to": tool.product },
  }));
}

/** The avatar's menu, "Your account": the person's own pages, the other tools and the way out. */
export function AccountMenu({ userName, avatar }: { userName: string; avatar?: string }) {
  const navigate = useNavigate();
  const logout = useLogout();
  const { data: tools } = useConnectedTools();
  return (
    <Menu
      label="Your account"
      align="start"
      trigger={(props) => (
        <IconButton icon={<Avatar name={userName} src={avatar} size="sm" />} label="Your account" size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} className="rounded-full" data-action="account" />
      )}
      items={[
        { label: "Your profile", icon: <Icon.User />, onSelect: () => navigate({ to: "/settings/profile" }), attrs: { "data-action": "profile" } },
        ...switchToItems(tools),
        { label: "Sign out", icon: <Icon.External />, separated: true, onSelect: () => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) }), attrs: { "data-action": "sign-out" } },
      ]}
    />
  );
}
