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
  createdAt: string;
  updatedAt: string;
}

export interface ThemeInput {
  name?: string;
  shared?: boolean;
  spec?: ThemeSpec;
}

export const themesQueryKey = ["themes"] as const;
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

/** The theme the reader chose; null is the built-in one. */
export function useActiveTheme() {
  return useQuery({ queryKey: activeThemeQueryKey, queryFn: () => request<{ theme: Theme | null }>("/themes/active") });
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

/** Uses a theme, or null to return to the built-in one. */
export function useChooseTheme() {
  return useThemeMutation((themeId: string | null) => request<{ theme: Theme | null }>("/themes/active", { method: "PUT", body: { themeId } }));
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
