import { ErrorBanner, Skeleton } from "@/components/ui";

/** Every widget waits with the same skeleton, so a dashboard that is still counting reads as one thing. */
export function Loading() {
  return (
    <div data-widget-loading="">
      <Skeleton lines={3} />
    </div>
  );
}

/** What a widget shows instead of numbers: the server's refusal, or that it is still counting. */
export function WidgetState({ error }: { error: unknown }) {
  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  return <Loading />;
}
