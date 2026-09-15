import { afterEach, describe, expect, expectTypeOf, it, vi } from "vitest";
import { ApiError } from "./client";
import { createApi, type Schemas } from "./generated";
import type { Issue, Comment, HistoryEntry } from "./issues";
import type { Project } from "./projects";
import type { Field, FieldValue } from "./fields";
import type { Attachment } from "./attachments";
import type { ManagedUser } from "./users";

// The hand written types are what the components render; the generated ones
// are what the server promises. Each hand written type has to be satisfied by
// the generated one, or the screen is reading a field the server never sends.
describe("the hand written types agree with the document", () => {
  it("for issues and what hangs off them", () => {
    expectTypeOf<Schemas["Issue"]>().toMatchTypeOf<Issue>();
    expectTypeOf<Schemas["Comment"]>().toMatchTypeOf<Comment>();
    expectTypeOf<Schemas["HistoryEntry"]>().toMatchTypeOf<HistoryEntry>();
  });

  it("for projects, fields and attachments", () => {
    expectTypeOf<Schemas["Project"]>().toMatchTypeOf<Project>();
    expectTypeOf<Schemas["Field"]>().toMatchTypeOf<Field>();
    expectTypeOf<Schemas["Value"]>().toMatchTypeOf<FieldValue>();
    expectTypeOf<Schemas["Attachment"]>().toMatchTypeOf<Attachment>();
  });

  it("for the organization's accounts", () => {
    expectTypeOf<Schemas["ManagedUser"]>().toMatchTypeOf<ManagedUser>();
  });
});

describe("the generated client", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("calls the documented path with the cookie and unwraps the body", async () => {
    const fetchMock = vi.fn(async (input: Request) => {
      expect(input.url).toBe("http://app.test/api/v1/issues/CP-1");
      expect(input.credentials).toBe("same-origin");
      return new Response(JSON.stringify({ issue: { key: "CP-1", summary: "one" } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const api = createApi("http://app.test/api/v1");
    const { data } = await api.GET("/issues/{issueKey}", { params: { path: { issueKey: "CP-1" } } });
    expect(data?.issue.key).toBe("CP-1");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("sends a body as JSON", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        expect(input.method).toBe("POST");
        expect(await input.json()).toEqual({ summary: "filed" });
        return new Response(JSON.stringify({ issue: { key: "CP-2", summary: "filed" } }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    const api = createApi("http://app.test/api/v1");
    const { data } = await api.POST("/projects/{projectKey}/issues", {
      params: { path: { projectKey: "CP" } },
      body: { summary: "filed" },
    });
    expect(data?.issue.key).toBe("CP-2");
  });

  // Every endpoint fails in the one shape; the client turns it into the one error.
  it("throws the shared ApiError from the error envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(JSON.stringify({ error: { code: "not_found", message: "That issue was not found." } }), {
          status: 404,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const api = createApi("http://app.test/api/v1");
    await expect(api.GET("/issues/{issueKey}", { params: { path: { issueKey: "CP-9" } } })).rejects.toMatchObject({
      name: "ApiError",
      status: 404,
      code: "not_found",
      message: "That issue was not found.",
    } satisfies Partial<ApiError>);
  });
});
