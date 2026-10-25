import { Link } from "@tanstack/react-router";
import { Button, ErrorBanner } from "@/components/ui";
import { t } from "@/i18n";

/** Files still on their way after the request itself was made. */
export interface Attaching {
  done: number;
  total: number;
}

/** A request that was made while some of its files were refused. */
export interface NotAttached {
  key: string;
  names: string[];
}

/** Send and what became of it, together: on a phone the top of the form is a screen above. */
export function PortalSendRow({
  refusal,
  failed,
  sending,
  attaching,
  ready,
}: {
  refusal?: string;
  failed: NotAttached | null;
  sending: boolean;
  attaching: Attaching | null;
  ready: boolean;
}) {
  if (failed) {
    return (
      <ErrorBanner>
        {t.desk.portal.sentNotAttached(failed.key, failed.names.length)} {failed.names.join(", ")}.{" "}
        <Link to="/portal/requests/$issueKey" params={{ issueKey: failed.key }} className="underline">
          {t.desk.portal.openRequest}
        </Link>{" "}
        {t.desk.portal.attachThere(failed.names.length)}
      </ErrorBanner>
    );
  }
  return (
    <div className="space-y-2" data-portal-send="">
      {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
      <div className="flex gap-2">
        <Button type="submit" loading={sending} disabled={!ready}>
          {attaching ? t.desk.portal.attaching(attaching.done + 1, attaching.total) : t.desk.portal.send}
        </Button>
        <Link to="/portal" className="inline-flex h-8 items-center px-3 text-sm text-ink-muted hover:text-ink">
          {t.desk.portal.cancel}
        </Link>
      </div>
    </div>
  );
}
