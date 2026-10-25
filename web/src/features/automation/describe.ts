import type { Action, Condition, Rule, RuleDescriptor, Trigger } from "@/api/automation";
import { t } from "@/i18n";

/** The label the catalogue gives a type, or the type itself when it is unknown. */
export function labelFor(catalog: RuleDescriptor[], kind: RuleDescriptor["kind"], type: string): string {
  return catalog.find((d) => d.kind === kind && d.type === type)?.label ?? type;
}

function triggerWords(trigger: Trigger, catalog: RuleDescriptor[]): string {
  const words = t.projectSetup.describe;
  const base = labelFor(catalog, "trigger", trigger.kind);
  if (trigger.kind === "issue.updated" && trigger.field) return words.fieldChanges(trigger.field);
  if (trigger.kind === "scheduled" && trigger.schedule) {
    const s = trigger.schedule;
    if (s.unit === "hours") return words.everyHours(s.every ?? 1, trigger.query ?? "");
    if (s.unit === "daily") return words.daily(s.at ?? "", trigger.query ?? "");
    return words.weekly(s.at ?? "", trigger.query ?? "");
  }
  return words.trigger(base);
}

function conditionWords(c: Condition): string {
  switch (c.kind) {
    case "nql":
      return t.projectSetup.describe.matches(c.query ?? "");
    case "field_equals":
      return t.projectSetup.describe.fieldIs(c.field ?? "", c.value ?? "");
    case "actor_role":
      return t.projectSetup.describe.actorIs((c.roles ?? []).join(t.projectSetup.describe.or));
  }
  return c.kind;
}

function actionWords(a: Action): string {
  switch (a.kind) {
    case "set_field":
      return t.projectSetup.describe.setField(a.field ?? "", a.value ?? "");
    case "transition":
      return t.projectSetup.describe.take(a.value ?? "");
    case "assign":
      return t.projectSetup.describe.assignTo(a.value ?? "");
    case "add_label":
      return t.projectSetup.describe.addLabel(a.value ?? "");
    case "add_comment":
      return t.projectSetup.describe.addComment;
    case "create_subissue":
      return t.projectSetup.describe.createSubtask;
    case "create_issue":
      return t.projectSetup.describe.createIssue;
    case "send_webhook":
      return t.projectSetup.describe.sendWebhook;
    case "send_mail":
      return t.projectSetup.describe.mail(a.to ?? "");
  }
  return a.kind;
}

/** One sentence for a rule: when, if, then. */
export function describeRule(rule: Pick<Rule, "trigger" | "conditions" | "actions">, catalog: RuleDescriptor[]): string {
  const words = t.projectSetup.describe;
  const parts = [words.when(triggerWords(rule.trigger, catalog))];
  if (rule.conditions.length) parts.push(words.if(rule.conditions.map(conditionWords).join(words.and)));
  parts.push(words.then(rule.actions.map(actionWords).join(words.thenNext)));
  return parts.join(", ") + ".";
}
