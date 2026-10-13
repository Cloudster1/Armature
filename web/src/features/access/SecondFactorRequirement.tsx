import { useState } from "react";
import { useRequireSecondFactor, useSecondFactorRule, type SecondFactorRule } from "@/api/secondFactor";
import { Button, Card, ErrorBanner, SectionTitle, Select, useToast } from "@/components/ui";

const ruleWords: Record<SecondFactorRule, string> = { off: "Off", admins: "For administrators", everyone: "For everyone" };

/**
 * Who must have two-step sign-in. Somebody without it is held to setting it up;
 * a sign-in through the identity provider brings its own.
 */
export function SecondFactorRequirement() {
  const { data, error } = useSecondFactorRule();
  const save = useRequireSecondFactor();
  const toast = useToast();
  const [draft, setDraft] = useState<SecondFactorRule | null>(null);
  const current = data?.require;

  return (
    <section data-second-factor-requirement>
      <SectionTitle className="mb-2">Two-step sign-in</SectionTitle>
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">
          Who must sign in with a code from an app on their phone as well as a password. Somebody without one is held to setting it up the next time they open
          Armature. Signing in through the identity provider below relies on the provider's own second step.
        </p>
        {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
        {current && (
          <>
            <Select label="Require two-step sign-in" value={draft ?? current} onChange={(e) => setDraft(e.target.value as SecondFactorRule)}>
              {(Object.keys(ruleWords) as SecondFactorRule[]).map((rule) => (
                <option key={rule} value={rule}>
                  {ruleWords[rule]}
                </option>
              ))}
            </Select>
            {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
            <Button
              loading={save.isPending}
              disabled={draft === null || draft === current}
              data-action="save-second-factor-rule"
              onClick={() => {
                if (draft) save.mutate(draft, { onSuccess: () => { setDraft(null); toast.success("Two-step sign-in requirement saved"); } });
              }}
            >
              Save
            </Button>
          </>
        )}
      </Card>
    </section>
  );
}
