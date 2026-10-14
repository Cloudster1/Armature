import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useDraft } from "./draft";

type Fields = { name: string; docsUrl: string };

describe("useDraft", () => {
  it("starts from what the server has", () => {
    const { result } = renderHook(({ server }) => useDraft<Fields>(server), { initialProps: { server: { name: "Alpha", docsUrl: "" } as Fields | undefined } });
    expect(result.current[0]).toEqual({ name: "Alpha", docsUrl: "" });
  });

  it("waits for the server when it has nothing yet", () => {
    const { result, rerender } = renderHook(({ server }) => useDraft<Fields>(server), { initialProps: { server: undefined as Fields | undefined } });
    expect(result.current[0]).toBeUndefined();
    rerender({ server: { name: "Alpha", docsUrl: "" } });
    expect(result.current[0]).toEqual({ name: "Alpha", docsUrl: "" });
  });

  // A refetch that lands while somebody types must not take the typing back.
  it("keeps a field somebody edited when the server answers again, and follows it for the rest", () => {
    const { result, rerender } = renderHook(({ server }) => useDraft<Fields>(server), {
      initialProps: { server: { name: "Alpha", docsUrl: "https://wiki.example.com" } as Fields | undefined },
    });
    act(() => result.current[1]("docsUrl", ""));
    rerender({ server: { name: "Alpha renamed", docsUrl: "https://wiki.example.com" } });
    expect(result.current[0]).toEqual({ name: "Alpha renamed", docsUrl: "" });
  });

  it("follows the server again once it holds what was saved", () => {
    const { result, rerender } = renderHook(({ server }) => useDraft<Fields>(server), {
      initialProps: { server: { name: "Alpha", docsUrl: "https://wiki.example.com" } as Fields | undefined },
    });
    act(() => result.current[1]("docsUrl", ""));
    rerender({ server: { name: "Alpha", docsUrl: "" } });
    rerender({ server: { name: "Alpha", docsUrl: "https://elsewhere.example.com" } });
    expect(result.current[0]?.docsUrl).toBe("https://elsewhere.example.com");
  });
});
