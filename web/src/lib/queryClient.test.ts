import { describe, expect, it, vi } from "vitest";
import { MutationObserver } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import { meQueryKey } from "@/api/auth";
import { createQueryClient } from "./queryClient";

// A session can end behind the page's back: signed out in another tab, or
// expired. The next request that is refused must take the person to sign in.

const signedIn = { principal: { user: { id: "u1", name: "Ada" } } };

// The codes the API answers with: "unauthorized" is a request with no session.
function refused(status: number, code = status === 401 ? "unauthorized" : "forbidden") {
  return () => Promise.reject(new ApiError(status, { code, message: "Sign in to continue." }));
}

describe("a session that ends while the app is open", () => {
  it("forgets who was signed in and asks for sign-in when a query is refused with 401", async () => {
    const ended = vi.fn();
    const client = createQueryClient(ended);
    client.setQueryData(meQueryKey, signedIn);

    await client.fetchQuery({ queryKey: ["issues", "list"], queryFn: refused(401) }).catch(() => {});

    expect(client.getQueryData(meQueryKey)).toBeUndefined();
    expect(ended).toHaveBeenCalledTimes(1);
  });

  it("does the same when a change is refused with 401", async () => {
    const ended = vi.fn();
    const client = createQueryClient(ended);
    client.setQueryData(meQueryKey, signedIn);

    await new MutationObserver(client, { mutationFn: refused(401) }).mutate().catch(() => {});

    expect(client.getQueryData(meQueryKey)).toBeUndefined();
    expect(ended).toHaveBeenCalledTimes(1);
  });

  it("leaves a refused sign-in alone, because nobody was signed in to lose", async () => {
    const ended = vi.fn();
    const client = createQueryClient(ended);

    await new MutationObserver(client, { mutationFn: refused(401) }).mutate().catch(() => {});

    expect(ended).not.toHaveBeenCalled();
  });

  it("keeps the session on any other refusal", async () => {
    const ended = vi.fn();
    const client = createQueryClient(ended);
    client.setQueryData(meQueryKey, signedIn);

    await client.fetchQuery({ queryKey: ["issues", "list"], queryFn: refused(403) }).catch(() => {});

    expect(client.getQueryData(meQueryKey)).toEqual(signedIn);
    expect(ended).not.toHaveBeenCalled();
  });

  it("keeps the session when a 401 refuses something typed, such as a wrong code", async () => {
    const ended = vi.fn();
    const client = createQueryClient(ended);
    client.setQueryData(meQueryKey, signedIn);

    await new MutationObserver(client, { mutationFn: refused(401, "code_invalid") }).mutate().catch(() => {});

    expect(client.getQueryData(meQueryKey)).toEqual(signedIn);
    expect(ended).not.toHaveBeenCalled();
  });

  it("asks for sign-in once, not once for every request that was in flight", async () => {
    const ended = vi.fn();
    const client = createQueryClient(ended);
    client.setQueryData(meQueryKey, signedIn);

    await Promise.all(["a", "b", "c"].map((key) => client.fetchQuery({ queryKey: ["issues", key], queryFn: refused(401) }).catch(() => {})));

    expect(ended).toHaveBeenCalledTimes(1);
  });
});
