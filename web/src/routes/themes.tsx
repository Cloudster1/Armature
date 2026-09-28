import { useRef, useState } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { useAccess } from "@/api/access";
import { themeExportHref, useActiveTheme, useChooseTheme, useDeleteTheme, useImportTheme, useSetDefaultTheme, useThemes, useUpdateTheme, type Theme } from "@/api/themes";
import { Button, EmptyState, ErrorBanner, IconButton, Menu, Page, PageHeader, Segmented, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";

type View = "mine" | "shared";

export const themesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/themes",
  component: ThemesPage,
});

/** Which themes a view shows. */
export function inThemeView(t: Theme, view: View, me: string | undefined): boolean {
  if (view === "mine") return t.ownerId === me;
  return t.shared && t.ownerId !== me;
}

/** Every theme the reader may use, and which one they do. */
function ThemesPage() {
  const { data, isLoading, error } = useThemes();
  const { data: meData } = useMe();
  const { data: access } = useAccess();
  const me = meData?.principal?.user.id;
  const administers = access?.canAdministerOrg ?? false;
  const { data: activeData } = useActiveTheme();
  const choose = useChooseTheme();
  const setDefault = useSetDefaultTheme();
  const importTheme = useImportTheme();
  const fileInput = useRef<HTMLInputElement>(null);
  const update = useUpdateTheme();
  const remove = useDeleteTheme();
  const confirm = useConfirm();
  const toast = useToast();
  const navigate = useNavigate();
  const [view, setView] = useState<View>("mine");
  const themes = (data?.themes ?? []).filter((t) => inThemeView(t, view, me));
  const seen = activeData?.theme ?? null;
  const source = activeData?.source ?? "";
  const orgDefault = (data?.themes ?? []).find((t) => t.default);
  const meta =
    seen && source === "organization"
      ? `You are using ${seen.name}, the organization's default.`
      : seen
        ? `You are using ${seen.name}.`
        : orgDefault
          ? `You are using the built-in theme, over the organization's default ${orgDefault.name}.`
          : "You are using the built-in theme.";

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Themes"
        meta={meta}
        actions={
          <>
            {seen && source === "organization" && (
              <Button variant="secondary" onClick={() => choose.mutate({ builtIn: true }, { onSuccess: () => toast.success("Back to the built-in theme") })} data-action="built-in-theme">
                Use the built-in theme
              </Button>
            )}
            {!seen && orgDefault && (
              <Button variant="secondary" onClick={() => choose.mutate(null, { onSuccess: () => toast.success(`Now using ${orgDefault.name}`) })} data-action="org-default-theme">
                Use the organization's default
              </Button>
            )}
            <Button variant="secondary" icon={<Icon.Upload />} loading={importTheme.isPending} onClick={() => fileInput.current?.click()} data-action="import-theme">
              Import theme
            </Button>
            <input
              ref={fileInput}
              type="file"
              accept="application/json,.json"
              className="sr-only"
              aria-label="Theme file"
              data-theme-file
              onChange={(event) => {
                const file = event.target.files?.[0];
                event.target.value = "";
                if (file) importTheme.mutate(file, { onSuccess: (made) => toast.success(`Imported ${made.theme.name}`) });
              }}
            />
            <Button icon={<Icon.Plus />} onClick={() => navigate({ to: "/settings/themes/new" })} data-action="new-theme">
              New theme
            </Button>
          </>
        }
      />
      <div className="mb-4">
        <Segmented<View>
          label="Show"
          value={view}
          onChange={setView}
          options={[
            { value: "mine", label: "Mine", attrs: { "data-themes-view": "mine" } },
            { value: "shared", label: "Shared with me", attrs: { "data-themes-view": "shared" } },
          ]}
        />
      </div>
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {choose.error && <ErrorBanner>{(choose.error as Error).message}</ErrorBanner>}
      {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      {setDefault.error && <ErrorBanner>{(setDefault.error as Error).message}</ErrorBanner>}
      {importTheme.error && <ErrorBanner>{(importTheme.error as Error).message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}
      {isLoading ? null : themes.length === 0 ? (
        <EmptyState
          title={view === "mine" ? "No themes of your own yet" : "Nothing shared with you"}
          description="A theme changes the colours, the type, the cursors, the icons and anything else about how this looks, for you, or for everyone once shared."
          action={
            view === "mine" ? (
              <Button variant="secondary" onClick={() => navigate({ to: "/settings/themes/new" })}>
                Make one
              </Button>
            ) : undefined
          }
        />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Theme</Th>
              <Th>Owner</Th>
              <Th>In use</Th>
              <Th className="w-10" />
            </tr>
          </thead>
          <tbody>
            {themes.map((t) => {
              const editable = t.ownerId === me || (administers && t.shared);
              return (
                <tr key={t.id} data-theme-row={t.name} data-theme-active={t.active ? "true" : "false"} data-theme-default={t.default ? "true" : "false"}>
                  <Td>
                    {editable ? (
                      <Link to="/settings/themes/$themeId" params={{ themeId: t.id }} className="font-medium text-ink hover:text-accent">
                        {t.name}
                      </Link>
                    ) : (
                      <span className="font-medium text-ink">{t.name}</span>
                    )}
                    {t.shared && <Tag className="ml-2">Shared</Tag>}
                    {t.default && <Tag className="ml-2">Organization's default</Tag>}
                    {seen?.id === t.id && <Tag className="ml-2 text-accent">In use</Tag>}
                  </Td>
                  <Td className="text-ink-muted">{t.ownerId === me ? "you" : t.ownerName}</Td>
                  <Td className="text-ink-muted tabular-nums">{t.inUse === 1 ? "1 person" : `${t.inUse} people`}</Td>
                  <Td className="text-right">
                    <Menu
                      label={`Actions for ${t.name}`}
                      align="end"
                      trigger={(props) => <IconButton icon={<Icon.More />} label={`Actions for ${t.name}`} size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} data-action="theme-menu" />}
                      items={[
                        t.active
                          ? { label: "Stop using", icon: <Icon.X />, onSelect: () => choose.mutate(null, { onSuccess: () => toast.success(orgDefault ? `Back to the organization's default` : "Back to the built-in theme") }), attrs: { "data-action": "stop-theme" } }
                          : { label: "Use this theme", icon: <Icon.Check />, onSelect: () => choose.mutate(t.id, { onSuccess: () => toast.success(`Now using ${t.name}`) }), attrs: { "data-action": "use-theme" } },
                        ...(administers && t.shared
                          ? [
                              t.default
                                ? { label: "No longer the organization's default", icon: <Icon.X />, onSelect: () => setDefault.mutate(null, { onSuccess: () => toast.success("The organization shows the built-in theme again") }), attrs: { "data-action": "undefault-theme" } }
                                : { label: "Make it the organization's default", icon: <Icon.Users />, onSelect: () => setDefault.mutate(t.id, { onSuccess: () => toast.success(`${t.name} is what everybody sees until they choose`) }), attrs: { "data-action": "default-theme" } },
                            ]
                          : []),
                        { label: "Edit", icon: <Icon.Edit />, disabled: !editable, onSelect: () => navigate({ to: "/settings/themes/$themeId", params: { themeId: t.id } }), attrs: { "data-action": "edit-theme" } },
                        { label: "Export as a file", icon: <Icon.Download />, onSelect: () => { window.location.href = themeExportHref(t.id); }, attrs: { "data-action": "export-theme" } },
                        {
                          label: t.shared ? "Stop sharing" : "Share with the organization",
                          icon: <Icon.Share />,
                          disabled: !editable,
                          onSelect: () => update.mutate({ id: t.id, shared: !t.shared }, { onSuccess: () => toast.success(t.shared ? `${t.name} is yours alone again` : `${t.name} is shared with everyone`) }),
                          attrs: { "data-action": "share-theme" },
                        },
                        {
                          label: "Delete",
                          icon: <Icon.Trash />,
                          danger: true,
                          disabled: !editable,
                          onSelect: async () => {
                            const using = t.inUse === 0 ? "Nobody is using it." : t.inUse === 1 ? "The one person using it goes back to the built-in theme." : `The ${t.inUse} people using it go back to the built-in theme.`;
                            if (await confirm({ noun: "theme", verb: "Delete", body: `${t.name} and its files go for good. ${using}` })) {
                              remove.mutate(t.id, { onSuccess: () => toast.success(`Deleted ${t.name}`) });
                            }
                          },
                          attrs: { "data-action": "delete-theme" },
                        },
                      ]}
                    />
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}
    </Page>
  );
}
