/**
 * Takes a mailed link's secret from after the #, off the address bar so history
 * does not keep it, and holds it for the tab so a reload or a detour still has it.
 */
export function takeLinkSecret(storageKey: string): string | undefined {
  const fromLink = window.location.hash.replace(/^#/, "");
  if (fromLink) {
    sessionStorage.setItem(storageKey, fromLink);
    window.history.replaceState(null, "", window.location.pathname);
    return fromLink;
  }
  return sessionStorage.getItem(storageKey) ?? undefined;
}
