import { useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { useRate, useRatingPage } from "@/api/desk";
import { Button, Card, ErrorBanner, Textarea } from "@/components/ui";
import { CSAT_SCORES } from "@/config";
import { t } from "@/i18n";

/** The one click a resolution mail asks for: no sign-in, the token in the address is the whole of it. */
export const rateRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/rate/$token",
  component: RatePage,
});

/** What each score means, said in a word, in the reader's language. */
export function scoreWord(score: number): string {
  return t.desk.rate.scores[score] ?? "";
}

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
            <p className="text-xs text-ink-subtle">{page.desk}</p>
            <h1 className="mt-1 text-lg font-semibold text-ink">{t.desk.rate.question(page.issueKey)}</h1>
            <p className="mt-1 text-sm text-ink-muted">{page.summary}</p>
            {done ? (
              <p className="mt-6 text-sm text-ink" data-rated>
                {t.desk.rate.thanks}
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
                <div className="flex gap-2" role="radiogroup" aria-label={t.desk.rate.yourRating}>
                  {CSAT_SCORES.map((n) => (
                    <Button
                      key={n}
                      type="button"
                      variant={score === n ? "primary" : "secondary"}
                      onClick={() => setScore(n)}
                      aria-pressed={score === n}
                      data-score={n}
                      className="flex-1"
                    >
                      {n}
                    </Button>
                  ))}
                </div>
                <p className="text-center text-sm text-ink-muted">{score ? scoreWord(score) : t.desk.rate.scale}</p>
                <label htmlFor="rate-comment" className="sr-only">
                  {t.desk.rate.anythingToAdd}
                </label>
                <Textarea
                  id="rate-comment"
                  rows={3}
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                  placeholder={t.desk.rate.commentPlaceholder}
                />
                <Button type="submit" loading={rate.isPending} disabled={!score} className="w-full" data-action="send-rating">
                  {t.desk.rate.send}
                </Button>
              </form>
            )}
          </>
        )}
      </Card>
    </main>
  );
}
