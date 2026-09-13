import type { Action, Condition, Rule, RuleDescriptor, Trigger } from "@/api/automation";

/** The label the catalogue gives a type, or the type itself when it is unknown. */
export function labelFor(catalog: RuleDescriptor[], kind: RuleDescriptor["kind"], type: string): string {
  return catalog.find((d) => d.kind === kind && d.type === type)?.label ?? type;
}

function triggerWords(t: Trigger, catalog: RuleDescriptor[]): string {
  const base = labelFor(catalog, "trigger", t.kind);
  if (t.kind === "issue.updated" && t.field) return `${t.field} changes`;
  if (t.kind === "scheduled" && t.schedule) {
    const s = t.schedule;
    if (s.unit === "hours") return `every ${s.every} hour${s.every === 1 ? "" : "s"}, over ${t.query}`;
    if (s.unit === "daily") return `daily at ${s.at}, over ${t.query}`;
    return `weekly at ${s.at}, over ${t.query}`;
  }
  return base.charAt(0).toLowerCase() + base.slice(1);
}

function conditionWords(c: Condition): string {
  switch (c.kind) {
    case "nql":
      return `it matches ${c.query}`;
    case "field_equals":
      return `${c.field} is ${c.value}`;
    case "actor_role":
      return `the actor is ${(c.roles ?? []).join(" or ")}`;
  }
  return c.kind;
}

function actionWords(a: Action): string {
  switch (a.kind) {
    case "set_field":
      return `set ${a.field} to ${a.value}`;
    case "transition":
      return `take ${a.value}`;
    case "assign":
      return `assign to ${a.value}`;
    case "add_label":
      return `add the label ${a.value}`;
    case "add_comment":
      return "add a comment";
    case "create_subissue":
      return "create a subtask";
    case "create_issue":
      return "create an issue";
    case "send_webhook":
      return "send a webhook";
    case "send_mail":
      return `mail ${a.to}`;
  }
  return a.kind;
}

/** One sentence for a rule: when, if, then. */
export function describeRule(rule: Pick<Rule, "trigger" | "conditions" | "actions">, catalog: RuleDescriptor[]): string {
  const parts = [`When ${triggerWords(rule.trigger, catalog)}`];
  if (rule.conditions.length) parts.push(`if ${rule.conditions.map(conditionWords).join(" and ")}`);
  parts.push(`then ${rule.actions.map(actionWords).join(", then ")}`);
  return parts.join(", ") + ".";
}
