import { useState, type FormEvent } from "react";
import { useAskForAccess } from "@/api/accessRequests";
import { ApiError } from "@/api/client";
import { Button, Card, ErrorBanner, Field } from "@/components/ui";
import { ACCESS_REQUEST_MESSAGE_MAX } from "@/config";
import { t } from "@/i18n";

/** Whether an error is the API's way of saying the issue is not there for us. */
export function isUnavailable(error: unknown): error is ApiError {
  return error instanceof ApiError && (error.status === 404 || error.code === "not_permitted");
}

// An issue the reader may not see, or that is not there: the two look alike
// unless its project said otherwise, and either way the reader may ask (#279).
export function IssueUnavailable({ issueKey, error }: { issueKey: string; error: ApiError }) {
  const ask = useAskForAccess(issueKey);
  const [message, setMessage] = useState("");
  const restricted = error.code === "not_permitted" && Boolean(error.project);
  const words = t.issueView.unavailable;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    ask.mutate(message.trim());
  }

  return (
    <Card className="mx-auto max-w-xl p-6" data-issue-unavailable={restricted ? "restricted" : "not-found"}>
      <h1 className="text-lg font-semibold text-ink">{restricted ? words.restricted(issueKey) : words.notFound(issueKey)}</h1>
      <p className="mt-1 text-sm text-ink-muted">{restricted ? words.restrictedDetail(error.project ?? "") : words.notFoundDetail}</p>
      {ask.isSuccess ? (
        <p className="mt-4 text-sm text-ink" role="status" data-access-asked>
          {words.asked}
        </p>
      ) : (
        <form onSubmit={onSubmit} className="mt-4 space-y-3" noValidate>
          {ask.error && <ErrorBanner>{(ask.error as Error).message}</ErrorBanner>}
          <Field
            label={words.message}
            rows={3}
            value={message}
            maxLength={ACCESS_REQUEST_MESSAGE_MAX}
            placeholder={words.messagePlaceholder}
            onChange={(e) => setMessage(e.target.value)}
          />
          <Button type="submit" loading={ask.isPending} data-action="ask-for-access">
            {words.ask}
          </Button>
        </form>
      )}
    </Card>
  );
}
