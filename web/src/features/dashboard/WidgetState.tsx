import { ErrorBanner } from "@/components/ui";

/** Every widget waits with the same line, so a dashboard that is still counting reads as one thing. */
export function Loading() {
  return (
    <p className="text-sm text-ink-subtle" data-widget-loading="">
      Counting...
    </p>
  );
}

/** What a widget shows instead of numbers: the server's refusal, or that it is still counting. */
export function WidgetState({ error }: { error: unknown }) {
  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  return <Loading />;
}
