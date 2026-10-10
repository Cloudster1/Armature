import { describe, expect, it } from "vitest";
import { act, fireEvent, screen } from "@testing-library/react";
import { ApiError } from "@/api/client";
import { useMutation } from "@/api/mutation";
import { UNSAVED_MESSAGE } from "@/lib/queryClient";
import { renderInShell } from "@/test/shell";

const refused = "Archive the project's open sprint before archiving the project.";
const refuse = () => Promise.reject(new ApiError(409, { code: "conflict", message: refused }));

/** A change that fails, and shows nothing about it unless told to. */
function Change({ run = refuse, handlesError = false, onError }: { run?: () => Promise<unknown>; handlesError?: boolean; onError?: () => void }) {
  const change = useMutation({ mutationFn: run, meta: handlesError ? { handlesError } : undefined });
  return (
    <>
      <button type="button" onClick={() => change.mutate(undefined, onError ? { onError } : undefined)}>
        Change
      </button>
      <span data-testid="status">{change.status}</span>
    </>
  );
}

/** The same change, drawing its own error the way the inline fields do. */
function ChangeShowingError() {
  const change = useMutation({ mutationFn: refuse });
  return (
    <>
      <button type="button" onClick={() => change.mutate()}>
        Change
      </button>
      {change.error && <p>{change.error.message}</p>}
    </>
  );
}

async function failOnce() {
  fireEvent.click(screen.getByRole("button", { name: "Change" }));
  await screen.findByText("error");
  await act(() => new Promise((resolve) => setTimeout(resolve)));
}

const toasts = () => document.querySelectorAll("[data-toast]");

describe("a failed change nobody else reports", () => {
  it("is said in a toast, in the server's words", async () => {
    renderInShell(<Change />);
    await failOnce();
    expect(screen.getByRole("alert")).toHaveTextContent(refused);
    expect(toasts()).toHaveLength(1);
  });

  it("says what to do when the server gave no sentence", async () => {
    renderInShell(<Change run={() => Promise.reject(new TypeError("Failed to fetch"))} />);
    await failOnce();
    expect(screen.getByRole("alert")).toHaveTextContent(UNSAVED_MESSAGE);
  });
});

describe("a failed change that reports itself", () => {
  it("is left to a mutation that says it handles its errors", async () => {
    renderInShell(<Change handlesError />);
    await failOnce();
    expect(toasts()).toHaveLength(0);
  });

  it("is left to a caller that passes its own onError", async () => {
    let told = 0;
    renderInShell(<Change onError={() => (told += 1)} />);
    await failOnce();
    expect(told).toBe(1);
    expect(toasts()).toHaveLength(0);
  });

  it("is said once by a component that draws the error itself", async () => {
    renderInShell(<ChangeShowingError />);
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    await screen.findByText(refused);
    await act(() => new Promise((resolve) => setTimeout(resolve)));
    expect(screen.getAllByText(refused)).toHaveLength(1);
    expect(toasts()).toHaveLength(0);
  });
});

// A mutation built straight on TanStack's hook would never tell the toast that
// its caller shows the error, and would be said twice.
describe("every mutation", () => {
  it("is built on the hook that knows whether its caller shows the error", () => {
    const sources = import.meta.glob<string>(["/src/**/*.{ts,tsx}", "!/src/**/*.test.{ts,tsx}"], { query: "?raw", import: "default", eager: true });
    const direct = Object.entries(sources)
      .filter(([path]) => path !== "/src/api/mutation.ts")
      .filter(([, source]) => /import\s*\{[^}]*\buseMutation\b[^}]*\}\s*from\s*"@tanstack\/react-query"/.test(source))
      .map(([path]) => path);
    expect(direct).toEqual([]);
  });
});
