import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request, upload } from "./client";
import type { ThemeSpec } from "@/lib/theme-css";

export type { ThemeSpec } from "@/lib/theme-css";
export { emptySpec, themeAssetURL } from "@/lib/theme-css";

/** One uploaded file of a theme. */
export interface ThemeAsset {
  id: string;
  name: string;
  contentType: string;
  size: number;
  createdAt: string;
}

/** A saved theme: the owner's, shared when they say so. Active is the reader's own mark. */
export interface Theme {
  id: string;
  ownerId: string;
  ownerName: string;
  name: string;
  shared: boolean;
  spec: ThemeSpec;
  assets: ThemeAsset[];
  inUse: number;
  active: boolean;
  /** The organization shows it to whoever has not chosen. */
  default: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ThemeInput {
  name?: string;
  shared?: boolean;
  spec?: ThemeSpec;
}

/** A theme shipped with the product, to start one's own from. */
export interface ThemeExample {
  key: string;
  name: string;
  description: string;
  spec: ThemeSpec;
}

export const themesQueryKey = ["themes"] as const;
export const themeExamplesQueryKey = ["themes", "examples"] as const;

export function useThemeExamples() {
  return useQuery({ queryKey: themeExamplesQueryKey, queryFn: () => request<{ examples: ThemeExample[] }>("/themes/examples"), staleTime: Infinity });
}

export const activeThemeQueryKey = ["themes", "active"] as const;

export function useThemes() {
  return useQuery({ queryKey: themesQueryKey, queryFn: () => request<{ themes: Theme[] }>("/themes") });
}

export function useTheme(id: string | undefined) {
  return useQuery({
    queryKey: [...themesQueryKey, id],
    queryFn: () => request<{ theme: Theme }>(`/themes/${id}`),
    enabled: Boolean(id),
  });
}

/** Where the theme the reader sees came from: their choice, the organization's default, or nothing. */
export type ThemeSource = "chosen" | "organization" | "";

/** The theme the reader sees; null is the built-in one. */
export function useActiveTheme() {
  return useQuery({ queryKey: activeThemeQueryKey, queryFn: () => request<{ theme: Theme | null; source: ThemeSource }>("/themes/active") });
}

function useThemeMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries({ queryKey: themesQueryKey }) });
}

export function useCreateTheme() {
  return useThemeMutation((input: ThemeInput) => request<{ theme: Theme }>("/themes", { method: "POST", body: input }));
}

export function useUpdateTheme() {
  return useThemeMutation(({ id, ...input }: ThemeInput & { id: string }) => request<{ theme: Theme }>(`/themes/${id}`, { method: "PATCH", body: input }));
}

export function useDeleteTheme() {
  return useThemeMutation((id: string) => request<void>(`/themes/${id}`, { method: "DELETE" }));
}

/** A choice: a theme, null for whatever the organization shows, or the built-in theme over it. */
export type ThemeChoice = string | null | { builtIn: true };

/** Uses a theme, returns to the organization's default with null, or keeps the built-in one over it. */
export function useChooseTheme() {
  return useThemeMutation((choice: ThemeChoice) => {
    const body = choice !== null && typeof choice === "object" ? { themeId: null, builtIn: true } : { themeId: choice };
    return request<{ theme: Theme | null }>("/themes/active", { method: "PUT", body });
  });
}

/** Names the shared theme everybody sees until they choose, or null for the built-in one. */
export function useSetDefaultTheme() {
  return useThemeMutation((themeId: string | null) => request<{ theme: Theme | null }>("/themes/default", { method: "PUT", body: { themeId } }));
}

/** Where a theme's export is fetched from, as a download the browser handles itself. */
export function themeExportHref(id: string): string {
  return `/api/v1/themes/${id}/export`;
}

/** Makes a theme of one's own from an exported theme file. */
export function useImportTheme() {
  return useThemeMutation((file: File) => {
    const form = new FormData();
    form.append("file", file, file.name);
    return upload<{ theme: Theme }>("/themes/import", form);
  });
}

export function useUploadThemeAsset() {
  return useThemeMutation(({ id, file }: { id: string; file: File }) => {
    const form = new FormData();
    form.append("file", file, file.name);
    return upload<{ asset: ThemeAsset }>(`/themes/${id}/assets`, form);
  });
}

export function useDeleteThemeAsset() {
  return useThemeMutation(({ id, assetId }: { id: string; assetId: string }) => request<void>(`/themes/${id}/assets/${assetId}`, { method: "DELETE" }));
}
