import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";
import { ApiError, BASE } from "./client";

/**
 * The typed client, generated from the server's own OpenAPI document.
 *
 * `schema.d.ts` is produced by `make openapi` from api/openapi.json, which the
 * server derives from its handler and domain types, so a path, a body or a
 * response that does not exist fails to compile here rather than at runtime.
 * The hand written hooks in the other api modules keep working; new code and
 * anything that wants exact types should reach for this.
 */

/** The server's schemas, by component name. */
export type Schemas = components["schemas"];

/** Turns the error envelope into the same ApiError the rest of the client throws. */
const raiseOnError: Middleware = {
  async onResponse({ response }) {
    if (response.ok) return response;
    let body: Schemas["ErrorEnvelope"] | undefined;
    try {
      body = (await response.clone().json()) as Schemas["ErrorEnvelope"];
    } catch {
      body = undefined;
    }
    throw new ApiError(
      response.status,
      body?.error ?? { code: "unexpected_response", message: `The server returned ${response.status}.` },
    );
  },
};

export function createApi(baseUrl: string = BASE) {
  const client = createClient<paths>({ baseUrl, credentials: "same-origin" });
  client.use(raiseOnError);
  return client;
}

/** The one client the application shares. */
export const api = createApi();
