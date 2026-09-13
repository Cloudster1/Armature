import { useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { useRate, useRatingPage } from "@/api/desk";
import { Button, Card, ErrorBanner, Textarea } from "@/components/ui";
import { CSAT_SCORES } from "@/config";

/** The one click a resolution mail asks for: no sign-in, the token in the address is the whole of it. */
export const rateRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/rate/$token",
  component: RatePage,
});

/** What each score means, said in a word. */
export const SCORE_WORDS: Record<number, string> = { 1: "Bad", 2: "Poor", 3: "Fine", 4: "Good", 5: "Great" };

function RatePage() {
  const { token } = rateRoute.useParams();
  const { data, error } = useRatingPage(token);
  const rate = useRate(token);
  const [score, setScore] = useState(0);
  const [comment, setComment] = useState("");
  const page = data?.rating;
  const done = Boolean(page?.rated) || rate.isSuccess;

  return (
    <main className="mx-auto max-w-md px-6 py-16" data-rate-page>
      <Card className="p-6">
        {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
        {page && (
          <>
            <p className="text-xs text-ink-subtle">{page.orgName}</p>
            <h1 className="mt-1 text-lg font-semibold text-ink">How did we do with {page.issueKey}?</h1>
            <p className="mt-1 text-sm text-ink-muted">{page.summary}</p>
            {done ? (
              <p className="mt-6 text-sm text-ink" data-rated>
                Thank you. Your rating is with the desk.
              </p>
            ) : (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  if (score) rate.mutate({ score, comment: comment.trim() || undefined });
                }}
                className="mt-6 space-y-4"
                noValidate
              >
                {rate.error && <ErrorBanner>{(rate.error as Error).message}</ErrorBanner>}
                <div className="flex gap-2" role="radiogroup" aria-label="Your rating">
                  {CSAT_SCORES.map((n) => (
                    <Button key={n} type="button" variant={score === n ? "primary" : "secondary"} onClick={() => setScore(n)} aria-pressed={score === n} data-score={n} className="flex-1">
                      {n}
                    </Button>
                  ))}
                </div>
                <p className="text-center text-sm text-ink-muted">{score ? SCORE_WORDS[score] : "1 is bad, 5 is great."}</p>
                <label htmlFor="rate-comment" className="sr-only">
                  Anything to add
                </label>
                <Textarea id="rate-comment" rows={3} value={comment} onChange={(e) => setComment(e.target.value)} placeholder="Anything to add? Optional." />
                <Button type="submit" loading={rate.isPending} disabled={!score} className="w-full" data-action="send-rating">
                  Send
                </Button>
              </form>
            )}
          </>
        )}
      </Card>
    </main>
  );
}
