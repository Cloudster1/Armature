import { useEndOtherSessions, useEndSession, useSessions, type ActiveSession } from "@/api/auth";
import { Button, Card, ErrorBanner, SectionTitle, Tag, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { deviceName } from "@/lib/device";
import { useFormat } from "@/lib/format";
import { t } from "@/i18n";

// Where the person is signed in, so a session left open on a shared machine
// ends without a password change, which a provider's account cannot make.
export function ActiveSessions() {
  const { data, error } = useSessions();
  const end = useEndSession();
  const endOthers = useEndOtherSessions();
  const confirm = useConfirm();
  const toast = useToast();
  const format = useFormat();
  const sessions = data?.sessions ?? [];
  const others = sessions.filter((s) => !s.current);

  async function signOut(session: ActiveSession) {
    const device = deviceName(session.userAgent);
    const where = session.ip ? t.doors.sessions.at(device, session.ip) : device;
    if (await confirm({ noun: device, verb: t.kit.confirm.verbs.signOut, body: t.doors.sessions.signOutBody(where) })) {
      end.mutate(session.id, { onSuccess: () => toast.success(t.doors.sessions.signedOutOf(device)) });
    }
  }

  async function signOutOthers() {
    if (await confirm({ noun: t.kit.confirm.things.everywhereElse, verb: t.kit.confirm.verbs.signOut, body: t.doors.sessions.othersBody(others.length) })) {
      endOthers.mutate(undefined, { onSuccess: () => toast.success(t.doors.sessions.signedOutElsewhere) });
    }
  }

  return (
    <section className="mt-8">
      <SectionTitle className="mb-2">{t.doors.sessions.title}</SectionTitle>
      <Card className="divide-y divide-border" data-sessions>
        {(error || end.error || endOthers.error) && (
          <div className="p-4">
            <ErrorBanner>{((error ?? end.error ?? endOthers.error) as Error).message}</ErrorBanner>
          </div>
        )}
        {sessions.map((session) => (
          <div key={session.id} className="flex items-center gap-3 px-4 py-3 text-sm" data-session={session.current ? "current" : session.id}>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="truncate text-ink" title={session.userAgent}>
                  {deviceName(session.userAgent)}
                </span>
                {session.current && <Tag>{t.doors.sessions.thisDevice}</Tag>}
              </div>
              <p className="truncate text-ink-subtle">
                {[session.ip, session.current ? t.doors.sessions.activeNow : t.doors.sessions.lastActive(format.relative(session.lastSeenAt))]
                  .filter(Boolean)
                  .join(" · ")}
              </p>
            </div>
            {!session.current && (
              <Button variant="ghost" size="sm" onClick={() => signOut(session)} data-action="end-session">
                {t.doors.common.signOut}
              </Button>
            )}
          </div>
        ))}
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <p className="text-sm text-ink-muted">{others.length ? t.doors.sessions.endOthers : t.doors.sessions.alone}</p>
          <Button
            variant="secondary"
            size="sm"
            disabled={others.length === 0}
            loading={endOthers.isPending}
            onClick={signOutOthers}
            data-action="end-other-sessions"
          >
            {t.doors.sessions.signOutEverywhere}
          </Button>
        </div>
      </Card>
    </section>
  );
}
