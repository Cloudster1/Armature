import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useChooseTheme, useCreateTheme, useDeleteThemeAsset, useUpdateTheme, useUploadThemeAsset, type Theme, type ThemeAsset, type ThemeSpec } from "@/api/themes";
import { Button, Card, ColorField, ErrorBanner, Field, IconButton, SectionTitle, Segmented, SelectInput, Switch, Tabs, Tag, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { KILOBYTE, THEME_ASSET_MAX_BYTES, THEME_CSS_MAX_BYTES, THEME_CSS_ROWS, THEME_CURSOR_PX, THEME_ICON_PREVIEW_PX, THEME_MAX_HOTSPOT, THEME_MAX_RADIUS, THEME_PREVIEW_DEBOUNCE_MS } from "@/config";
import { compileTheme, emptySpec } from "@/lib/theme-css";
import { CURSOR_KINDS, SHADOW_KEYS, TOKEN_GROUPS } from "@/lib/theme-tokens";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { setThemePreview } from "./ThemeLoader";

type TabID = "colours" | "type" | "shape" | "cursors" | "icons" | "backdrop" | "files" | "advanced";
type Mode = "light" | "dark";

const tabs: Array<{ value: TabID; label: string }> = [
  { value: "colours", label: "Colours" },
  { value: "type", label: "Type" },
  { value: "shape", label: "Shape" },
  { value: "cursors", label: "Cursors" },
  { value: "icons", label: "Icons" },
  { value: "backdrop", label: "Backdrop" },
  { value: "files", label: "Files" },
  { value: "advanced", label: "Advanced" },
];

const UPLOAD_ACCEPT = "image/png,image/jpeg,image/webp,image/gif,image/svg+xml,.svg,.woff,.woff2,font/woff,font/woff2";

/** The glyphs the kit draws, by the name a theme keys them on. */
const glyphs: Array<{ name: string; Glyph: (typeof Icon)[keyof typeof Icon] }> = Object.values(Icon)
  .map((Glyph) => ({ name: (Glyph as unknown as { glyph: string }).glyph, Glyph }))
  .sort((a, b) => a.name.localeCompare(b.name));

function isFont(a: ThemeAsset): boolean {
  return a.contentType.startsWith("font/");
}
function isImage(a: ThemeAsset): boolean {
  return a.contentType.startsWith("image/");
}

/**
 * One theme, part by part. Nothing is written until Save; Preview shows the
 * draft on this very page, and leaving the page shows the chosen theme again.
 */
export function ThemeEditor({ theme }: { theme?: Theme }) {
  const navigate = useNavigate();
  const toast = useToast();
  const create = useCreateTheme();
  const update = useUpdateTheme();
  const choose = useChooseTheme();
  const [name, setName] = useState(theme?.name ?? "");
  const [shared, setShared] = useState(theme?.shared ?? false);
  const [spec, setSpec] = useState<ThemeSpec>(() => (theme ? structuredClone(theme.spec) : emptySpec()));
  const [tab, setTab] = useState<TabID>("colours");
  const [preview, setPreview] = useState(false);
  const assets = theme?.assets ?? [];
  const saving = create.isPending || update.isPending;
  const patch = (change: (draft: ThemeSpec) => void) =>
    setSpec((current) => {
      const draft = structuredClone(current);
      change(draft);
      return draft;
    });

  // The draft is compiled after it settles, so typing a colour does not
  // restyle the page on every keystroke.
  useEffect(() => {
    if (!preview) {
      setThemePreview(null);
      return;
    }
    const timer = window.setTimeout(() => setThemePreview(compileTheme({ id: theme?.id ?? "new", spec, assets })), THEME_PREVIEW_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [preview, spec, theme?.id, assets]);
  useEffect(() => () => setThemePreview(null), []);

  function save(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    if (theme) {
      update.mutate({ id: theme.id, name: name.trim(), shared, spec }, { onSuccess: (saved) => toast.success(`Saved ${saved.theme.name}`) });
    } else {
      create.mutate(
        { name: name.trim(), shared, spec },
        {
          onSuccess: (made) => {
            toast.success(`Made ${made.theme.name}`);
            navigate({ to: "/settings/themes/$themeId", params: { themeId: made.theme.id } });
          },
        },
      );
    }
  }

  return (
    <form onSubmit={save} className="space-y-4" noValidate data-theme-editor={theme?.id ?? "new"}>
      {(create.error || update.error || choose.error) && <ErrorBanner>{((create.error ?? update.error ?? choose.error) as Error).message}</ErrorBanner>}
      <Card className="p-4">
        <div className="flex flex-wrap items-end gap-4">
          <Field label="Theme name" id="field-theme-name" value={name} onChange={(e) => setName(e.target.value)} required className="w-64" />
          <label className="flex items-center gap-2 pb-2 text-sm text-ink-muted">
            <Switch checked={shared} onChange={setShared} label="Shared with the organization" data-theme-shared={shared ? "true" : "false"} />
            Shared with the organization
          </label>
          <label className="flex items-center gap-2 pb-2 text-sm text-ink-muted">
            <Switch checked={preview} onChange={setPreview} label="Preview on this page" data-action="preview-theme" />
            Preview on this page
          </label>
          <span className="ml-auto flex gap-2 pb-1">
            {theme && !theme.active && (
              <Button type="button" variant="secondary" loading={choose.isPending} onClick={() => choose.mutate(theme.id, { onSuccess: () => toast.success(`Now using ${theme.name}`) })} data-action="use-theme">
                Use this theme
              </Button>
            )}
            <Button type="submit" loading={saving} disabled={!name.trim()} data-action="save-theme">
              Save theme
            </Button>
          </span>
        </div>
      </Card>

      <Tabs<TabID> label="Theme" value={tab} onChange={setTab} tabs={tabs.map((each) => ({ ...each, attrs: { "data-theme-tab": each.label } }))} />

      {tab === "colours" && <ColoursTab spec={spec} patch={patch} />}
      {tab === "type" && <TypeTab spec={spec} patch={patch} assets={assets} />}
      {tab === "shape" && <ShapeTab spec={spec} patch={patch} />}
      {tab === "cursors" && <CursorsTab spec={spec} patch={patch} assets={assets} />}
      {tab === "icons" && <IconsTab spec={spec} patch={patch} assets={assets} />}
      {tab === "backdrop" && <BackdropTab spec={spec} patch={patch} assets={assets} />}
      {tab === "files" && <FilesTab theme={theme} spec={spec} />}
      {tab === "advanced" && <AdvancedTab spec={spec} patch={patch} />}
    </form>
  );
}

type Patch = (change: (draft: ThemeSpec) => void) => void;

function stockValue(token: string): string {
  try {
    return getComputedStyle(document.documentElement).getPropertyValue(`--color-${token}`).trim();
  } catch {
    return "";
  }
}

function ColoursTab({ spec, patch }: { spec: ThemeSpec; patch: Patch }) {
  const [mode, setMode] = useState<Mode>("light");
  const set = spec.colors[mode];
  const overridden = Object.keys(set).length;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Segmented<Mode>
          label="Palette"
          value={mode}
          onChange={setMode}
          options={[
            { value: "light", label: "Light", attrs: { "data-theme-mode": "light" } },
            { value: "dark", label: "Dark", attrs: { "data-theme-mode": "dark" } },
          ]}
        />
        <span className="text-sm text-ink-muted">{overridden === 0 ? "Nothing changed yet: every colour is the stylesheet's." : `${overridden} of ${TOKEN_GROUPS.reduce((n, g) => n + g.tokens.length, 0)} colours changed.`}</span>
      </div>
      {TOKEN_GROUPS.map((group) => (
        <Card key={group.title} className="p-4">
          <SectionTitle className="mb-3">{group.title}</SectionTitle>
          <div className="grid gap-3 sm:grid-cols-2">
            {group.tokens.map((token) => {
              const value = set[token.name] ?? "";
              return (
                <ColorField
                  key={token.name}
                  id={`field-token-${token.name}`}
                  label={token.label}
                  value={value}
                  placeholder={stockValue(token.name) || "#000000"}
                  onChange={(next) => patch((draft) => {
                    if (next.trim() === "") delete draft.colors[mode][token.name];
                    else draft.colors[mode][token.name] = next.trim();
                  })}
                  data-token={token.name}
                  data-token-mode={mode}
                >
                  {value && <IconButton icon={<Icon.X />} label={`Reset ${token.label}`} size="sm" onClick={() => patch((draft) => { delete draft.colors[mode][token.name]; })} data-action="reset-token" />}
                </ColorField>
              );
            })}
          </div>
        </Card>
      ))}
    </div>
  );
}

function AssetSelect({ label, assets, value, onChange, attrs }: { label: string; assets: ThemeAsset[]; value: string | undefined; onChange: (id: string | undefined) => void; attrs?: Record<string, string> }) {
  return (
    <SelectInput aria-label={label} controlSize="sm" value={value ?? ""} onChange={(e) => onChange(e.target.value || undefined)} className="w-56" {...attrs}>
      <option value="">{assets.length === 0 ? "No files yet: add one under Files" : "Choose a file..."}</option>
      {assets.map((a) => (
        <option key={a.id} value={a.id}>
          {a.name}
        </option>
      ))}
    </SelectInput>
  );
}

function TypeTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const fonts = assets.filter(isFont);
  return (
    <Card className="space-y-5 p-4">
      {(["sans", "mono"] as const).map((role) => {
        const font = spec.fonts[role];
        return (
          <div key={role} className="space-y-2" data-theme-font={role}>
            <SectionTitle>{role === "sans" ? "Text face" : "Code face"}</SectionTitle>
            <div className="flex flex-wrap items-end gap-3">
              <Field
                label={role === "sans" ? "Text family" : "Code family"}
                value={font?.family ?? ""}
                placeholder={role === "sans" ? "Inter" : "JetBrains Mono"}
                onChange={(e) => patch((draft) => {
                  const family = e.target.value;
                  if (family.trim() === "") delete draft.fonts[role];
                  else draft.fonts[role] = { ...(draft.fonts[role] ?? {}), family };
                })}
                className="w-64"
                hint="A family the reader has installed, or the name to give an uploaded file."
              />
              <span className="space-y-1 pb-5">
                <span className="block text-sm font-medium text-ink-muted">File</span>
                <AssetSelect label={`${role} font file`} assets={fonts} value={font?.assetId} onChange={(id) => patch((draft) => { if (draft.fonts[role]) draft.fonts[role] = { family: draft.fonts[role]!.family, ...(id ? { assetId: id } : {}) }; })} />
              </span>
              {font && (
                <Button type="button" variant="ghost" size="sm" className="mb-5" onClick={() => patch((draft) => { delete draft.fonts[role]; })}>
                  Reset
                </Button>
              )}
            </div>
          </div>
        );
      })}
    </Card>
  );
}

function NumberField({ label, value, onChange, max, placeholder, attrs }: { label: string; value: number | undefined; onChange: (value: number | undefined) => void; max: number; placeholder: string; attrs?: Record<string, string> }) {
  return (
    <Field
      label={label}
      type="number"
      min={0}
      max={max}
      value={value ?? ""}
      placeholder={placeholder}
      onChange={(e) => onChange(e.target.value === "" ? undefined : Math.max(0, Math.min(max, Number(e.target.value))))}
      className="w-28"
      {...attrs}
    />
  );
}

function ShapeTab({ spec, patch }: { spec: ThemeSpec; patch: Patch }) {
  return (
    <div className="space-y-4">
      <Card className="p-4">
        <SectionTitle className="mb-3">Corners</SectionTitle>
        <div className="flex flex-wrap gap-4">
          <NumberField label="Control radius" value={spec.shape.radiusControl} max={THEME_MAX_RADIUS} placeholder="6" onChange={(v) => patch((d) => { if (v === undefined) delete d.shape.radiusControl; else d.shape.radiusControl = v; })} />
          <NumberField label="Overlay radius" value={spec.shape.radiusOverlay} max={THEME_MAX_RADIUS} placeholder="8" onChange={(v) => patch((d) => { if (v === undefined) delete d.shape.radiusOverlay; else d.shape.radiusOverlay = v; })} />
        </div>
      </Card>
      <Card className="p-4">
        <SectionTitle className="mb-3">Shadows</SectionTitle>
        <div className="space-y-3">
          {SHADOW_KEYS.map((key) => (
            <Field
              key={key}
              label={key === "1" ? "Raised" : key === "2" ? "Overlays" : "Elevated glass"}
              value={spec.shadows[key] ?? ""}
              placeholder="0 1px 2px rgb(0 0 0 / 0.1)"
              onChange={(e) => patch((d) => { if (e.target.value.trim() === "") delete d.shadows[key]; else d.shadows[key] = e.target.value; })}
              className="font-mono"
              data-shadow={key}
            />
          ))}
        </div>
      </Card>
    </div>
  );
}

function CursorsTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const images = assets.filter(isImage);
  return (
    <Card className="p-4">
      <p className="mb-4 text-sm text-ink-muted">{`A cursor is a picture of up to ${THEME_CURSOR_PX} by ${THEME_CURSOR_PX} pixels, PNG or SVG, with the point it clicks from. The browser keeps its own when a picture cannot be read.`}</p>
      <div className="space-y-3">
        {CURSOR_KINDS.map((each) => {
          const cursor = spec.cursors[each.kind];
          return (
            <div key={each.kind} className="flex flex-wrap items-end gap-3" data-theme-cursor={each.kind}>
              <span className="w-28 pb-2 text-sm text-ink">{each.label}</span>
              <span className="space-y-1">
                <span className="block text-sm font-medium text-ink-muted">Picture</span>
                <AssetSelect label={`${each.label} cursor picture`} assets={images} value={cursor?.assetId} onChange={(id) => patch((d) => { if (!id) delete d.cursors[each.kind]; else d.cursors[each.kind] = { assetId: id, hotspotX: cursor?.hotspotX ?? 0, hotspotY: cursor?.hotspotY ?? 0 }; })} />
              </span>
              {cursor && (
                <>
                  <NumberField label="Point x" value={cursor.hotspotX} max={THEME_MAX_HOTSPOT} placeholder="0" onChange={(v) => patch((d) => { d.cursors[each.kind]!.hotspotX = v ?? 0; })} />
                  <NumberField label="Point y" value={cursor.hotspotY} max={THEME_MAX_HOTSPOT} placeholder="0" onChange={(v) => patch((d) => { d.cursors[each.kind]!.hotspotY = v ?? 0; })} />
                </>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}

function IconsTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const images = assets.filter(isImage);
  const [query, setQuery] = useState("");
  const shown = useMemo(() => glyphs.filter((g) => g.name.includes(query.trim().toLowerCase())), [query]);
  return (
    <Card className="p-4">
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <Field label="Find an icon" value={query} onChange={(e) => setQuery(e.target.value)} className="w-56" />
        <p className="pb-2 text-sm text-ink-muted">A picture is drawn in the text's colour through a mask, so an SVG's own colours do not matter. Path data is drawn on the kit's 16 pixel grid, one path per line.</p>
      </div>
      <div className="divide-y divide-border">
        {shown.map(({ name, Glyph }) => {
          const icon = spec.icons[name];
          return (
            <div key={name} className="flex flex-wrap items-start gap-3 py-2" data-theme-icon={name}>
              <span className="flex w-40 items-center gap-2 pt-2 text-sm text-ink">
                <Glyph size={THEME_ICON_PREVIEW_PX} />
                {name}
              </span>
              <span className="space-y-1">
                <span className="block text-sm font-medium text-ink-muted">Picture</span>
                <AssetSelect label={`${name} icon picture`} assets={images} value={icon?.assetId} onChange={(id) => patch((d) => { if (!id && !(icon?.paths?.length)) delete d.icons[name]; else d.icons[name] = { ...(id ? { assetId: id } : {}), ...(icon?.paths?.length ? { paths: icon.paths } : {}) }; })} />
              </span>
              <Field
                label={`${name} path data`}
                id={`field-icon-${name}`}
                rows={2}
                value={icon?.paths?.join("\n") ?? ""}
                placeholder="M2 8h12"
                onChange={(e) => patch((d) => {
                  const paths = e.target.value.split("\n").map((line) => line.trim()).filter(Boolean);
                  if (paths.length === 0 && !icon?.assetId) delete d.icons[name];
                  else d.icons[name] = { ...(icon?.assetId ? { assetId: icon.assetId } : {}), ...(paths.length ? { paths } : {}) };
                })}
                className="w-72 font-mono"
              />
              {icon && (
                <Button type="button" variant="ghost" size="sm" className="mt-6" onClick={() => patch((d) => { delete d.icons[name]; })}>
                  Reset
                </Button>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}

function BackdropTab({ spec, patch, assets }: { spec: ThemeSpec; patch: Patch; assets: ThemeAsset[] }) {
  const images = assets.filter(isImage);
  const backdrop = spec.backdrop ?? null;
  return (
    <Card className="p-4">
      <p className="mb-4 text-sm text-ink-muted">A picture behind the content, where the soft gradient is. The two gradient colours under Colours are what shows when there is none.</p>
      <div className="flex flex-wrap items-end gap-4">
        <span className="space-y-1">
          <span className="block text-sm font-medium text-ink-muted">Picture</span>
          <AssetSelect label="Backdrop picture" assets={images} value={backdrop?.assetId} onChange={(id) => patch((d) => { d.backdrop = id ? { assetId: id, fit: backdrop?.fit ?? "cover" } : null; })} />
        </span>
        {backdrop && (
          <span className="pb-0.5">
            <Segmented<"cover" | "tile">
              label="Fit"
              size="sm"
              value={backdrop.fit}
              onChange={(fit) => patch((d) => { if (d.backdrop) d.backdrop.fit = fit; })}
              options={[
                { value: "cover", label: "Cover" },
                { value: "tile", label: "Tile" },
              ]}
            />
          </span>
        )}
      </div>
    </Card>
  );
}

function FilesTab({ theme, spec }: { theme?: Theme; spec: ThemeSpec }) {
  const uploadAsset = useUploadThemeAsset();
  const deleteAsset = useDeleteThemeAsset();
  const confirm = useConfirm();
  const toast = useToast();
  const fileInput = useRef<HTMLInputElement>(null);
  const [tooBig, setTooBig] = useState(false);
  if (!theme) {
    return (
      <Card className="p-4">
        <p className="text-sm text-ink-muted">Save the theme first; then pictures and fonts can be added to it here.</p>
      </Card>
    );
  }
  const used = new Set<string>([
    ...[spec.fonts.sans?.assetId, spec.fonts.mono?.assetId, spec.backdrop?.assetId].filter((id): id is string => Boolean(id)),
    ...Object.values(spec.cursors).map((c) => c.assetId),
    ...Object.values(spec.icons).map((i) => i.assetId).filter((id): id is string => Boolean(id)),
  ]);
  function onFile(file: File | undefined) {
    if (!file) return;
    if (file.size > THEME_ASSET_MAX_BYTES) {
      setTooBig(true);
      return;
    }
    setTooBig(false);
    uploadAsset.mutate({ id: theme!.id, file }, { onSuccess: (made) => toast.success(`Added ${made.asset.name}`) });
  }
  return (
    <Card className="space-y-3 p-4">
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" variant="secondary" icon={<Icon.Upload />} loading={uploadAsset.isPending} onClick={() => fileInput.current?.click()} data-action="add-theme-file">
          Add a file
        </Button>
        <input ref={fileInput} type="file" accept={UPLOAD_ACCEPT} className="hidden" aria-label="Choose a file for the theme" data-theme-file-input onChange={(e) => onFile(e.target.files?.[0])} />
        <p className="text-sm text-ink-subtle">PNG, JPEG, WebP, GIF or SVG pictures and WOFF or WOFF2 fonts, up to 2 MB each. An SVG with script or links elsewhere is refused.</p>
      </div>
      {tooBig && <ErrorBanner>That file is over 2 MB. Make it smaller and try again.</ErrorBanner>}
      {uploadAsset.error && <ErrorBanner>{(uploadAsset.error as Error).message}</ErrorBanner>}
      {deleteAsset.error && <ErrorBanner>{(deleteAsset.error as Error).message}</ErrorBanner>}
      {theme.assets.length === 0 ? (
        <p className="text-sm text-ink-muted">No files yet.</p>
      ) : (
        <ul className="divide-y divide-border">
          {theme.assets.map((a) => (
            <li key={a.id} className="flex items-center gap-3 py-2 text-sm" data-theme-asset={a.name}>
              <span className="min-w-0 flex-1 truncate text-ink">{a.name}</span>
              <Tag>{a.contentType}</Tag>
              <span className="text-ink-subtle tabular-nums">{Math.max(1, Math.round(a.size / KILOBYTE))} KB</span>
              {used.has(a.id) && <Tag>In use</Tag>}
              <IconButton
                icon={<Icon.Trash />}
                label={`Remove ${a.name}`}
                size="sm"
                disabled={used.has(a.id) || deleteAsset.isPending}
                onClick={async () => {
                  if (await confirm({ noun: "file", verb: "Remove", body: `${a.name} goes from the theme's files.` })) {
                    deleteAsset.mutate({ id: theme.id, assetId: a.id }, { onSuccess: () => toast.success(`Removed ${a.name}`) });
                  }
                }}
                data-action="remove-theme-file"
              />
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function AdvancedTab({ spec, patch }: { spec: ThemeSpec; patch: Patch }) {
  const size = new TextEncoder().encode(spec.css).length;
  return (
    <Card className="p-4">
      <Field
        label="Extra CSS"
        rows={THEME_CSS_ROWS}
        value={spec.css}
        onChange={(e) => patch((d) => { d.css = e.target.value; })}
        className="font-mono"
        hint={`Anything the parts above do not reach. It may load only the theme's own files and inline images, never @import or another site. ${Math.round(size / KILOBYTE)} of ${THEME_CSS_MAX_BYTES / KILOBYTE} KB.`}
        data-theme-css=""
      />
    </Card>
  );
}
