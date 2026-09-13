import { useState } from "react";
import { useProvider, useSaveProvider } from "@/api/access";
import { useMe } from "@/api/auth";
import { Button, Card, ErrorBanner, Field } from "@/components/ui";

/**
 * The identity provider people sign in through.
 *
 * The client secret is write-only: it is never sent back, and leaving the field
 * empty keeps the one already stored, so saving the rest of the form does not
 * quietly erase it.
 */
export function ProviderSettings() {
  const { data, isLoading } = useProvider();
  const { data: me } = useMe();
  const save = useSaveProvider();

  const provider = data?.provider;
  const slug = me?.principal?.org?.slug;

  const [issuer, setIssuer] = useState<string | null>(null);
  const [clientId, setClientId] = useState<string | null>(null);
  const [secret, setSecret] = useState("");
  const [claim, setClaim] = useState<string | null>(null);

  if (isLoading) return <p className="text-sm text-ink-muted">Loading...</p>;

  const values = {
    issuer: issuer ?? provider?.issuer ?? "",
    clientId: clientId ?? provider?.clientId ?? "",
    groupsClaim: claim ?? provider?.groupsClaim ?? "groups",
  };

  return (
    <Card className="space-y-4 p-4">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field
          label="Issuer"
          value={values.issuer}
          placeholder="https://id.example.com"
          hint="Where its discovery document lives."
          onChange={(event) => setIssuer(event.target.value)}
        />
        <Field
          label="Client ID"
          value={values.clientId}
          onChange={(event) => setClientId(event.target.value)}
        />
        <Field
          label="Client secret"
          type="password"
          value={secret}
          placeholder={provider?.hasSecret ? "Stored. Leave blank to keep it." : ""}
          onChange={(event) => setSecret(event.target.value)}
        />
        <Field
          label="Groups claim"
          value={values.groupsClaim}
          hint="The claim listing somebody's groups."
          onChange={(event) => setClaim(event.target.value)}
        />
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
              enabled: true,
            })
          }
        >
          {provider ? "Save" : "Set up single sign-on"}
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
                enabled: false,
              })
            }
          >
            Turn it off
          </Button>
        )}

        {provider && (
          <span className="text-xs text-ink-muted">
            {provider.enabled ? "People can sign in at" : "Turned off. The address would be"}{" "}
            <code className="rounded bg-surface-raised px-1">/api/v1/auth/oidc/{slug}/start</code>
          </span>
        )}
      </div>

      {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
    </Card>
  );
}
