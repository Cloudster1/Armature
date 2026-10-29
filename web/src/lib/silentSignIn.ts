import { BASE, request } from "@/api/client";
import { SSO_START } from "@/api/auth";

/** The start of a silent sign-in that returns to next, the path with its query. */
export function silentStart(next: string): string {
  return `${BASE}${SSO_START}?${new URLSearchParams({ prompt: "none", next })}`;
}

/**
 * Sends the browser to ask the identity provider quietly, when the server says
 * one is set up and this browser has not tried yet (#325), and says whether it did.
 */
export async function trySilentSignIn(next: string): Promise<boolean> {
  try {
    const offer = await request<{ silentSignIn?: boolean }>("/auth/signup");
    if (!offer.silentSignIn) return false;
  } catch {
    // Not knowing is not trying: the sign-in page still works.
    return false;
  }
  window.location.assign(silentStart(next));
  return true;
}
