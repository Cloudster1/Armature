import { useState } from "react";
import { useRequireSecondFactor, useSecondFactorRule, type SecondFactorRule } from "@/api/secondFactor";
import { Button, Card, ErrorBanner, SectionTitle, Select, useToast } from "@/components/ui";
import { t } from "@/i18n";

const RULES: SecondFactorRule[] = ["off", "admins", "everyone"];

const ruleWord = (rule: SecondFactorRule): string =>
  rule === "off" ? t.access.secondFactor.off : rule === "admins" ? t.access.secondFactor.admins : t.access.secondFactor.everyone;

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
      <SectionTitle className="mb-2">{t.access.secondFactor.title}</SectionTitle>
      <Card className="space-y-4 p-4">
        <p className="text-sm text-ink-muted">{t.access.secondFactor.explain}</p>
        {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
        {current && (
          <>
            <Select label={t.access.secondFactor.require} value={draft ?? current} onChange={(e) => setDraft(e.target.value as SecondFactorRule)}>
              {RULES.map((rule) => (
                <option key={rule} value={rule}>
                  {ruleWord(rule)}
                </option>
              ))}
            </Select>
            {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
            <Button
              loading={save.isPending}
              disabled={draft === null || draft === current}
              data-action="save-second-factor-rule"
              onClick={() => {
                if (draft)
                  save.mutate(draft, {
                    onSuccess: () => {
                      setDraft(null);
                      toast.success(t.access.secondFactor.saved);
                    },
                  });
              }}
            >
              {t.access.save}
            </Button>
          </>
        )}
      </Card>
    </section>
  );
}
