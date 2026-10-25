import type { PortalRequest } from "@/api/desk";
import { cleanDomain } from "./DeskExtras";
import { t } from "@/i18n";

/** Domains typed as a list: commas or spaces between them, each as the server stores it. */
export function parseDomains(raw: string): string[] {
  return [
    ...new Set(
      raw
        .split(/[\s,]+/)
        .map(cleanDomain)
        .filter(Boolean),
    ),
  ];
}

/** Where a request came from, as an agent reads it: "Globex, shared" or "Globex". */
export function companyLine(name: string, shared: boolean): string {
  return shared ? t.desk.companyShared(name) : name;
}

/** The shared requests by the company they were shared with, in the order they came. */
export function sharedByCompany(requests: PortalRequest[]): Array<{ company: string; requests: PortalRequest[] }> {
  const groups: Array<{ company: string; requests: PortalRequest[] }> = [];
  for (const r of requests) {
    const company = r.company?.name ?? "";
    const group = groups.find((g) => g.company === company);
    if (group) group.requests.push(r);
    else groups.push({ company, requests: [r] });
  }
  return groups;
}
