import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { request } from "./client";
import type { OrgRole } from "./auth";
import { MENTION_MAX_SUGGESTIONS, PEOPLE_PAGE_SIZE, PERSON_PICKER_RESULTS, QUERY_STALE_MS } from "@/config";

/** Somebody with an active account here, as a picker offers them. */
export interface Member {
  id: string;
  name: string;
  email: string;
  role: OrgRole;
  /** Where the picture is served from; absent means initials. */
  avatarUrl?: string;
}

/** One page of the members that matched, and how many did in all. */
export interface MemberPage {
  members: Member[];
  total: number;
  limit: number;
  offset: number;
}

export interface MemberListParams {
  /** Text the name or the address contains. */
  q?: string;
  /** Leave the service desk's customers out. */
  staff?: boolean;
  offset?: number;
  limit?: number;
}

export const membersQueryKey = ["members"] as const;

function membersQuery({ q = "", staff = false, offset = 0, limit = PEOPLE_PAGE_SIZE }: MemberListParams) {
  const text = q.trim();
  return {
    queryKey: [...membersQueryKey, { q: text, staff, offset, limit }],
    queryFn: () => {
      const params = new URLSearchParams({ limit: String(limit) });
      if (text) params.set("q", text);
      if (staff) params.set("staff", "true");
      if (offset) params.set("offset", String(offset));
      return request<MemberPage>(`/members?${params}`);
    },
  };
}

/** A page of members by name; the server never sends them all at once. */
export function useMembers(params: MemberListParams = {}) {
  return useQuery({ ...membersQuery(params), placeholderData: keepPreviousData });
}

/** The first members whose name or address holds the text, for a picker. */
export function useMemberSearch(q: string, { staff = false, limit = PERSON_PICKER_RESULTS }: { staff?: boolean; limit?: number } = {}) {
  return useMembers({ q, staff, limit });
}

/** Asks the server for who an at sign may name: the staff whose name or address holds the text. */
export function useFindMentionable() {
  const queryClient = useQueryClient();
  return useCallback(
    (q: string) =>
      queryClient
        .fetchQuery({ ...membersQuery({ q, staff: true, limit: MENTION_MAX_SUGGESTIONS }), staleTime: QUERY_STALE_MS })
        .then((page) => page.members.map((m) => ({ id: m.id, name: m.name, email: m.email }))),
    [queryClient],
  );
}
