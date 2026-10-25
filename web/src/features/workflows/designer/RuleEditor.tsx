import { useState } from "react";
import type { RuleKind, RuleType } from "@/api/workflows";
import { Button, Checkbox, Field, IconButton, Select } from "@/components/ui";
import { Icon } from "@/components/icons";
import { describeRule, type DraftRule } from "./graph";
import { t } from "@/i18n";

// The kinds in the order the engine asks them; their words are read when the editor draws.
const kindOrder: RuleKind[] = ["condition", "validator", "postfunction"];
const kinds = (): Array<{ kind: RuleKind; title: string; hint: string }> => kindOrder.map((kind) => ({ kind, ...t.workflows.rules.kinds[kind] }));

/**
 * The rules on one transition, grouped the way the engine asks them: whether
 * the move is offered, whether the input passes, and what happens afterwards.
 * A rule is picked from what the server says it can run, never typed.
 */
export function RuleEditor({ rules, catalogue, onChange }: { rules: DraftRule[]; catalogue: RuleType[]; onChange: (rules: DraftRule[]) => void }) {
  const [picked, setPicked] = useState("");
  const [config, setConfig] = useState<Record<string, unknown>>({});
  const chosen = catalogue.find((type) => type.type === picked);

  const complete =
    chosen !== undefined &&
    chosen.options.every((option) => {
      if (!option.required) return true;
      const value = config[option.name];
      return Array.isArray(value) ? value.length > 0 : typeof value === "string" && value.trim() !== "";
    });

  function add() {
    if (!chosen) return;
    onChange([...rules, { kind: chosen.kind, type: chosen.type, config }]);
    setPicked("");
    setConfig({});
  }

  return (
    <div className="space-y-3" data-rule-editor>
      {kinds().map(({ kind, title, hint }) => {
        const ofKind = rules.map((rule, index) => ({ rule, index })).filter(({ rule }) => rule.kind === kind);
        return (
          <div key={kind}>
            <p className="text-sm font-medium text-ink">{title}</p>
            <p className="text-xs text-ink-subtle">{hint}</p>
            {ofKind.length > 0 && (
              <ul className="mt-1.5 space-y-1">
                {ofKind.map(({ rule, index }) => {
                  const text = describeRule(rule, catalogue);
                  return (
                    <li
                      key={`${rule.type}-${index}`}
                      data-rule={rule.type}
                      className="flex items-center justify-between gap-2 rounded-md bg-surface-raised px-2 py-1 text-sm text-ink"
                    >
                      <span className="min-w-0 truncate" title={text}>
                        {text}
                      </span>
                      <IconButton
                        size="xs"
                        icon={<Icon.X />}
                        label={t.workflows.rules.remove(text)}
                        onClick={() => onChange(rules.filter((_, at) => at !== index))}
                        className="text-ink-subtle hover:text-danger"
                      />
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        );
      })}

      <div className="space-y-2 border-t border-border pt-3">
        <Select
          label={t.workflows.rules.add}
          id="field-rule"
          value={picked}
          onChange={(event) => {
            setPicked(event.target.value);
            setConfig({});
          }}
        >
          <option value="">{t.workflows.rules.choose}</option>
          {kinds().map(({ kind, title }) => (
            <optgroup key={kind} label={title}>
              {catalogue
                .filter((type) => type.kind === kind)
                .map((type) => (
                  <option key={type.type} value={type.type}>
                    {type.label}
                  </option>
                ))}
            </optgroup>
          ))}
        </Select>
        {chosen && <p className="text-xs text-ink-muted">{chosen.description}</p>}

        {chosen?.options.map((option) =>
          option.kind === "roles" ? (
            <fieldset key={option.name} className="space-y-1">
              <legend className="text-sm font-medium text-ink-muted">{option.label}</legend>
              {option.choices.map((choice) => {
                const current = Array.isArray(config[option.name]) ? (config[option.name] as string[]) : [];
                return (
                  <Checkbox
                    key={choice}
                    label={choice}
                    className="flex"
                    checked={current.includes(choice)}
                    onChange={(event) =>
                      setConfig({
                        ...config,
                        [option.name]: event.target.checked ? [...current, choice] : current.filter((each) => each !== choice),
                      })
                    }
                  />
                );
              })}
            </fieldset>
          ) : (
            <Field
              key={option.name}
              label={option.label}
              id={`field-rule-${option.name}`}
              value={typeof config[option.name] === "string" ? (config[option.name] as string) : ""}
              onChange={(event) => setConfig({ ...config, [option.name]: event.target.value })}
            />
          ),
        )}

        <Button size="sm" variant="secondary" disabled={!complete} onClick={add}>
          {t.workflows.rules.addButton}
        </Button>
      </div>
    </div>
  );
}
