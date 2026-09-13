import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, request } from "./client";

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** Builds a Response-like object without depending on a full fetch polyfill. */
function reply(status: number, body: string, ok = status >= 200 && status < 300) {
  return { status, ok, text: () => Promise.resolve(body) };
}

describe("request", () => {
  it("returns the parsed body on success", async () => {
    fetchMock.mockResolvedValue(reply(200, JSON.stringify({ hello: "world" })));

    await expect(request<{ hello: string }>("/thing")).resolves.toEqual({ hello: "world" });
  });

  it("sends credentials so the session cookie is included", async () => {
    fetchMock.mockResolvedValue(reply(200, "{}"));

    await request("/thing");

    const [, init] = fetchMock.mock.calls[0]!;
    expect(init.credentials).toBe("same-origin");
  });

  it("serialises a body and sets the content type, but not on a bodyless request", async () => {
    fetchMock.mockResolvedValue(reply(200, "{}"));
    await request("/thing", { method: "POST", body: { a: 1 } });
    expect(fetchMock.mock.calls[0]![1].body).toBe('{"a":1}');
    expect(fetchMock.mock.calls[0]![1].headers["Content-Type"]).toBe("application/json");

    fetchMock.mockResolvedValue(reply(200, "{}"));
    await request("/thing");
    expect(fetchMock.mock.calls[1]![1].headers).toEqual({});
  });

  it("handles a 204 with no body", async () => {
    fetchMock.mockResolvedValue(reply(204, ""));

    await expect(request("/thing", { method: "DELETE" })).resolves.toBeUndefined();
  });

  it("unwraps the API error envelope", async () => {
    fetchMock.mockResolvedValue(
      reply(
        422,
        JSON.stringify({
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { email: "That address is not valid." },
            requestId: "abc-123",
          },
        }),
      ),
    );

    const error = await request("/thing").catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    const apiError = error as ApiError;
    expect(apiError.status).toBe(422);
    expect(apiError.code).toBe("validation_failed");
    expect(apiError.message).toBe("Some fields need attention.");
    expect(apiError.fields).toEqual({ email: "That address is not valid." });
    expect(apiError.requestId).toBe("abc-123");
  });

  it("flags a 401 so the router can send the user to sign in", async () => {
    fetchMock.mockResolvedValue(
      reply(401, JSON.stringify({ error: { code: "unauthorized", message: "Sign in to continue." } })),
    );

    const error = (await request("/thing").catch((e: unknown) => e)) as ApiError;
    expect(error.isUnauthenticated).toBe(true);

    fetchMock.mockResolvedValue(
      reply(403, JSON.stringify({ error: { code: "forbidden", message: "No." } })),
    );
    const forbidden = (await request("/thing").catch((e: unknown) => e)) as ApiError;
    expect(forbidden.isUnauthenticated).toBe(false);
  });

  // A gateway or proxy in front of the API can return HTML. That must surface
  // as a normal ApiError rather than a JSON parse crash inside the app.
  it("survives a non-JSON error response", async () => {
    fetchMock.mockResolvedValue(reply(502, "<html>Bad Gateway</html>"));

    const error = (await request("/thing").catch((e: unknown) => e)) as ApiError;
    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(502);
    expect(error.code).toBe("unexpected_response");
    expect(error.message).toContain("502");
  });

  it("survives an error response with no envelope", async () => {
    fetchMock.mockResolvedValue(reply(500, JSON.stringify({ something: "else" })));

    const error = (await request("/thing").catch((e: unknown) => e)) as ApiError;
    expect(error.code).toBe("unexpected_response");
  });

  it("has empty fields when the envelope omits them", async () => {
    fetchMock.mockResolvedValue(
      reply(409, JSON.stringify({ error: { code: "email_taken", message: "Taken." } })),
    );

    const error = (await request("/thing").catch((e: unknown) => e)) as ApiError;
    expect(error.fields).toEqual({});
  });

  it("prefixes paths with the API version", async () => {
    fetchMock.mockResolvedValue(reply(200, "{}"));
    await request("/auth/me");
    expect(fetchMock.mock.calls[0]![0]).toBe("/api/v1/auth/me");
  });

  it("lets a network failure propagate", async () => {
    fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));
    await expect(request("/thing")).rejects.toThrow("Failed to fetch");
  });
});
