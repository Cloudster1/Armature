import { useRef, useState } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { useAccess } from "@/api/access";
import {
  themeExportHref,
  useActiveTheme,
  useChooseTheme,
  useDeleteTheme,
  useImportTheme,
  useSetDefaultTheme,
  useThemes,
  useUpdateTheme,
  type Theme,
} from "@/api/themes";
import { Button, EmptyState, ErrorBanner, IconButton, Menu, Page, PageHeader, Segmented, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
// Named apart: this page calls each theme t.
import { t as words } from "@/i18n";

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
    seen && source === "everyone"
      ? words.looks.themes.usingDefault(seen.name)
      : seen
        ? words.looks.themes.using(seen.name)
        : orgDefault
          ? words.looks.themes.builtInOver(orgDefault.name)
          : words.looks.themes.builtIn;

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            {words.settings.crumb}
          </Link>
        }
        title={words.looks.themes.title}
        meta={meta}
        actions={
          <>
            {seen && source === "everyone" && (
              <Button
                variant="secondary"
                onClick={() => choose.mutate({ builtIn: true }, { onSuccess: () => toast.success(words.looks.themes.backToBuiltIn) })}
                data-action="built-in-theme"
              >
                {words.looks.themes.useBuiltIn}
              </Button>
            )}
            {!seen && orgDefault && (
              <Button
                variant="secondary"
                onClick={() => choose.mutate(null, { onSuccess: () => toast.success(words.looks.themes.nowUsing(orgDefault.name)) })}
                data-action="org-default-theme"
              >
                {words.looks.themes.useDefault}
              </Button>
            )}
            <Button
              variant="secondary"
              icon={<Icon.Upload />}
              loading={importTheme.isPending}
              onClick={() => fileInput.current?.click()}
              data-action="import-theme"
            >
              {words.looks.themes.importTheme}
            </Button>
            <input
              ref={fileInput}
              type="file"
              accept="application/json,.json"
              className="sr-only"
              aria-label={words.looks.themes.themeFile}
              data-theme-file
              onChange={(event) => {
                const file = event.target.files?.[0];
                event.target.value = "";
                if (file) importTheme.mutate(file, { onSuccess: (made) => toast.success(words.looks.themes.imported(made.theme.name)) });
              }}
            />
            <Button icon={<Icon.Plus />} onClick={() => navigate({ to: "/settings/themes/new" })} data-action="new-theme">
              {words.looks.themes.newTheme}
            </Button>
          </>
        }
      />
      <div className="mb-4">
        <Segmented<View>
          label={words.looks.themes.show}
          value={view}
          onChange={setView}
          options={[
            { value: "mine", label: words.looks.themes.mine, attrs: { "data-themes-view": "mine" } },
            { value: "shared", label: words.looks.themes.sharedWithMe, attrs: { "data-themes-view": "shared" } },
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
          title={view === "mine" ? words.looks.themes.noneOfYourOwn : words.looks.themes.noneShared}
          description={words.looks.themes.emptyDetail}
          action={
            view === "mine" ? (
              <Button variant="secondary" onClick={() => navigate({ to: "/settings/themes/new" })}>
                {words.looks.themes.makeOne}
              </Button>
            ) : undefined
          }
        />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>{words.looks.themes.columnTheme}</Th>
              <Th>{words.looks.themes.columnOwner}</Th>
              <Th>{words.looks.themes.columnInUse}</Th>
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
                    {t.shared && <Tag className="ml-2">{words.looks.themes.shared}</Tag>}
                    {t.default && <Tag className="ml-2">{words.looks.themes.everyonesDefault}</Tag>}
                    {seen?.id === t.id && <Tag className="ml-2 text-accent">{words.looks.themes.inUse}</Tag>}
                  </Td>
                  <Td className="text-ink-muted">{t.ownerId === me ? words.looks.themes.you : t.ownerName}</Td>
                  <Td className="text-ink-muted tabular-nums">{words.looks.themes.people(t.inUse)}</Td>
                  <Td className="text-right">
                    <Menu
                      label={words.looks.themes.actionsFor(t.name)}
                      align="end"
                      trigger={(props) => (
                        <IconButton
                          icon={<Icon.More />}
                          label={words.looks.themes.actionsFor(t.name)}
                          size="sm"
                          onClick={props.toggle}
                          aria-haspopup={props["aria-haspopup"]}
                          aria-expanded={props["aria-expanded"]}
                          data-action="theme-menu"
                        />
                      )}
                      items={[
                        t.active
                          ? {
                              label: words.looks.themes.stopUsing,
                              icon: <Icon.X />,
                              onSelect: () =>
                                choose.mutate(null, {
                                  onSuccess: () => toast.success(orgDefault ? words.looks.themes.backToDefault : words.looks.themes.backToBuiltIn),
                                }),
                              attrs: { "data-action": "stop-theme" },
                            }
                          : {
                              label: words.looks.themes.useThis,
                              icon: <Icon.Check />,
                              onSelect: () => choose.mutate(t.id, { onSuccess: () => toast.success(words.looks.themes.nowUsing(t.name)) }),
                              attrs: { "data-action": "use-theme" },
                            },
                        ...(administers && t.shared
                          ? [
                              t.default
                                ? {
                                    label: words.looks.themes.noLongerDefault,
                                    icon: <Icon.X />,
                                    onSelect: () => setDefault.mutate(null, { onSuccess: () => toast.success(words.looks.themes.builtInAgain) }),
                                    attrs: { "data-action": "undefault-theme" },
                                  }
                                : {
                                    label: words.looks.themes.makeDefault,
                                    icon: <Icon.Users />,
                                    onSelect: () => setDefault.mutate(t.id, { onSuccess: () => toast.success(words.looks.themes.nowDefault(t.name)) }),
                                    attrs: { "data-action": "default-theme" },
                                  },
                            ]
                          : []),
                        {
                          label: words.looks.themes.edit,
                          icon: <Icon.Edit />,
                          disabled: !editable,
                          onSelect: () => navigate({ to: "/settings/themes/$themeId", params: { themeId: t.id } }),
                          attrs: { "data-action": "edit-theme" },
                        },
                        {
                          label: words.looks.themes.exportFile,
                          icon: <Icon.Download />,
                          onSelect: () => {
                            window.location.href = themeExportHref(t.id);
                          },
                          attrs: { "data-action": "export-theme" },
                        },
                        {
                          label: t.shared ? words.looks.themes.stopSharing : words.looks.themes.share,
                          icon: <Icon.Share />,
                          disabled: !editable,
                          onSelect: () =>
                            update.mutate(
                              { id: t.id, shared: !t.shared },
                              { onSuccess: () => toast.success(t.shared ? words.looks.themes.aloneAgain(t.name) : words.looks.themes.sharedNow(t.name)) },
                            ),
                          attrs: { "data-action": "share-theme" },
                        },
                        {
                          label: words.looks.themes.delete,
                          icon: <Icon.Trash />,
                          danger: true,
                          disabled: !editable,
                          onSelect: async () => {
                            const using =
                              t.inUse === 0
                                ? words.looks.themes.nobodyUsing
                                : t.inUse === 1
                                  ? words.looks.themes.oneUsing
                                  : words.looks.themes.manyUsing(t.inUse);
                            if (
                              await confirm({
                                noun: words.kit.confirm.things.theme,
                                verb: words.kit.confirm.verbs.delete,
                                body: words.looks.themes.deleteBody(t.name, using),
                              })
                            ) {
                              remove.mutate(t.id, { onSuccess: () => toast.success(words.looks.themes.deleted(t.name)) });
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
