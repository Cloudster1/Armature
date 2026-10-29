import { useState } from "react";
import { useProvider, useSaveProvider } from "@/api/access";
import { SSO_START } from "@/api/auth";
import { BASE } from "@/api/client";
import { Button, Card, Checkbox, ErrorBanner, Field, Skeleton } from "@/components/ui";
import { t } from "@/i18n";

/**
 * The identity provider people sign in through.
 *
 * The client secret is write-only: it is never sent back, and leaving the field
 * empty keeps the one already stored, so saving the rest of the form does not
 * quietly erase it.
 */
export function ProviderSettings() {
  const { data, isLoading } = useProvider();
  const save = useSaveProvider();

  const provider = data?.provider;

  const [issuer, setIssuer] = useState<string | null>(null);
  const [clientId, setClientId] = useState<string | null>(null);
  const [secret, setSecret] = useState("");
  const [claim, setClaim] = useState<string | null>(null);
  const [admit, setAdmit] = useState<boolean | null>(null);

  if (isLoading) return <Skeleton lines={4} />;

  const values = {
    issuer: issuer ?? provider?.issuer ?? "",
    clientId: clientId ?? provider?.clientId ?? "",
    groupsClaim: claim ?? provider?.groupsClaim ?? "groups",
    admitByGroup: admit ?? provider?.admitByGroup ?? false,
  };

  return (
    <Card className="space-y-4 p-4">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field
          label={t.access.provider.issuer}
          value={values.issuer}
          placeholder="https://id.example.com"
          hint={t.access.provider.issuerHint}
          onChange={(event) => setIssuer(event.target.value)}
        />
        <Field label={t.access.provider.clientId} value={values.clientId} onChange={(event) => setClientId(event.target.value)} />
        <Field
          label={t.access.provider.secret}
          type="password"
          value={secret}
          placeholder={provider?.hasSecret ? t.access.provider.secretStored : ""}
          onChange={(event) => setSecret(event.target.value)}
        />
        <Field
          label={t.access.provider.groupsClaim}
          value={values.groupsClaim}
          hint={t.access.provider.groupsClaimHint}
          onChange={(event) => setClaim(event.target.value)}
        />
      </div>

      <div className="space-y-1">
        <Checkbox label={t.access.provider.admit} checked={values.admitByGroup} onChange={(event) => setAdmit(event.target.checked)} data-admit-by-group />
        <p className="pl-6 text-xs text-ink-muted">{t.access.provider.admitDetail}</p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Button
          loading={save.isPending}
          onClick={() =>
            save.mutate({
              issuer: values.issuer,
              clientId: values.clientId,
              clientSecret: secret,
              groupsClaim: values.groupsClaim,
              scopes: provider?.scopes ?? "openid profile email",
              createGroups: provider?.createGroups ?? false,
              admitByGroup: values.admitByGroup,
              enabled: true,
            })
          }
        >
          {provider ? t.access.save : t.access.provider.setUp}
        </Button>

        {provider?.enabled && (
          <Button
            variant="ghost"
            loading={save.isPending}
            onClick={() =>
              save.mutate({
                issuer: provider.issuer,
                clientId: provider.clientId,
                clientSecret: "",
                groupsClaim: provider.groupsClaim,
                scopes: provider.scopes,
                createGroups: provider.createGroups,
                admitByGroup: provider.admitByGroup,
                enabled: false,
              })
            }
          >
            {t.access.provider.turnOff}
          </Button>
        )}

        {provider && (
          <span className="text-xs text-ink-muted">
            {provider.enabled ? t.access.provider.signInAt : t.access.provider.offAddress}{" "}
            <code className="rounded bg-surface-raised px-1" data-sso-start>
              {BASE}
              {SSO_START}
            </code>
          </span>
        )}
      </div>

      {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
    </Card>
  );
}
