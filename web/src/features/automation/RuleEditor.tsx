import { useState, type FormEvent } from "react";
import type { Action, Condition, Rule, RuleDescriptor, RuleInput, Trigger, Webhook } from "@/api/automation";
import { Button, Checkbox, Dialog, ErrorBanner, Field, IconButton, Labelled, Select } from "@/components/ui";
import { Icon } from "@/components/icons";
import { LabelInput } from "@/features/labels/LabelInput";
import { t } from "@/i18n";

function blankInput(): RuleInput {
  return { name: "", trigger: { kind: "issue.created" }, conditions: [], actions: [{ kind: "add_label" }], allowOwnEvents: false };
}

function fromRule(rule: Rule): RuleInput {
  return {
    name: rule.name,
    enabled: rule.enabled,
    trigger: rule.trigger,
    conditions: rule.conditions,
    actions: rule.actions,
    allowOwnEvents: rule.allowOwnEvents,
    hourlyCap: rule.hourlyCap,
  };
}

// The editor is driven by the catalogue: each trigger, condition and action
// says which options it takes, and the form draws exactly those.
export function RuleEditor({
  open,
  onClose,
  rule,
  catalog,
  webhooks,
  onSave,
  saving,
  error,
}: {
  open: boolean;
  onClose: () => void;
  rule: Rule | null;
  catalog: RuleDescriptor[];
  webhooks: Webhook[];
  onSave: (input: RuleInput) => void;
  saving: boolean;
  error?: string;
}) {
  const [draft, setDraft] = useState<RuleInput>(() => (rule ? fromRule(rule) : blankInput()));
  const triggers = catalog.filter((d) => d.kind === "trigger");
  const conditions = catalog.filter((d) => d.kind === "condition");
  const actions = catalog.filter((d) => d.kind === "action");
  const trigger = triggers.find((d) => d.type === draft.trigger.kind);

  function setTrigger(patch: Partial<Trigger>) {
    setDraft((d) => ({ ...d, trigger: { ...d.trigger, ...patch } }));
  }
  function setCondition(i: number, patch: Partial<Condition>) {
    setDraft((d) => ({ ...d, conditions: d.conditions.map((c, j) => (j === i ? { ...c, ...patch } : c)) }));
  }
  function setAction(i: number, patch: Partial<Action>) {
    setDraft((d) => ({ ...d, actions: d.actions.map((a, j) => (j === i ? { ...a, ...patch } : a)) }));
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const cleaned: RuleInput = { ...draft, name: draft.name.trim() };
    if (cleaned.trigger.kind !== "scheduled") delete cleaned.trigger.schedule;
    if (cleaned.trigger.kind === "scheduled" && !cleaned.trigger.schedule) cleaned.trigger.schedule = { unit: "hours", every: 1 };
    onSave(cleaned);
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={rule ? t.projectSetup.editor.editTitle : t.projectSetup.editor.newTitle}
      size="lg"
      attrs={{ "data-rule-editor": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" form="rule-form" loading={saving} disabled={!draft.name.trim() || draft.actions.length === 0} data-action="save-rule">
            {rule ? t.projectSetup.editor.save : t.projectSetup.editor.create}
          </Button>
        </>
      }
    >
      <form id="rule-form" onSubmit={onSubmit} className="space-y-5" noValidate>
        <Field
          label={t.projectSetup.editor.name}
          required
          value={draft.name}
          onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          placeholder={t.projectSetup.editor.namePlaceholder}
        />

        <fieldset className="space-y-3">
          <legend className="text-sm font-medium text-ink">{t.projectSetup.editor.when}</legend>
          <Select
            label={t.projectSetup.editor.trigger}
            id="field-trigger"
            value={draft.trigger.kind}
            onChange={(e) => setDraft({ ...draft, trigger: { kind: e.target.value } })}
            hint={trigger?.description}
          >
            {triggers.map((d) => (
              <option key={d.type} value={d.type}>
                {d.label}
              </option>
            ))}
          </Select>
          {draft.trigger.kind === "issue.updated" && (
            <Field
              label={t.projectSetup.editor.fieldToWatch}
              value={draft.trigger.field ?? ""}
              onChange={(e) => setTrigger({ field: e.target.value })}
              hint={t.projectSetup.editor.fieldToWatchHint}
            />
          )}
          {draft.trigger.kind === "scheduled" && (
            <>
              <Field
                label={t.projectSetup.editor.query}
                required
                value={draft.trigger.query ?? ""}
                onChange={(e) => setTrigger({ query: e.target.value })}
                placeholder="statusCategory != done AND due < now()"
              />
              <div className="grid grid-cols-3 gap-3">
                <Select
                  label={t.projectSetup.editor.rhythm}
                  id="field-rhythm"
                  value={draft.trigger.schedule?.unit ?? "hours"}
                  onChange={(e) =>
                    setTrigger({
                      schedule: {
                        ...(draft.trigger.schedule ?? {}),
                        unit: e.target.value as "hours" | "daily" | "weekly",
                        every: draft.trigger.schedule?.every ?? 1,
                        at: draft.trigger.schedule?.at ?? "09:00",
                      },
                    })
                  }
                >
                  <option value="hours">{t.projectSetup.editor.everyHours}</option>
                  <option value="daily">{t.projectSetup.editor.daily}</option>
                  <option value="weekly">{t.projectSetup.editor.weekly}</option>
                </Select>
                {(draft.trigger.schedule?.unit ?? "hours") === "hours" ? (
                  <Field
                    label={t.projectSetup.editor.hours}
                    type="number"
                    min={1}
                    value={String(draft.trigger.schedule?.every ?? 1)}
                    onChange={(e) => setTrigger({ schedule: { ...(draft.trigger.schedule ?? { unit: "hours" }), every: Number(e.target.value) } })}
                  />
                ) : (
                  <Field
                    label={t.projectSetup.editor.at}
                    type="time"
                    value={draft.trigger.schedule?.at ?? "09:00"}
                    onChange={(e) => setTrigger({ schedule: { ...(draft.trigger.schedule ?? { unit: "daily" }), at: e.target.value } })}
                  />
                )}
                {draft.trigger.schedule?.unit === "weekly" && (
                  <Select
                    label={t.projectSetup.editor.on}
                    id="field-weekday"
                    value={String(draft.trigger.schedule?.weekday ?? 1)}
                    onChange={(e) => setTrigger({ schedule: { ...(draft.trigger.schedule ?? { unit: "weekly" }), weekday: Number(e.target.value) } })}
                  >
                    {t.projectSetup.editor.weekdays.map((day, i) => (
                      <option key={day} value={i}>
                        {day}
                      </option>
                    ))}
                  </Select>
                )}
              </div>
            </>
          )}
          {draft.trigger.kind === "incoming" && rule?.trigger.token && (
            <p className="text-sm text-ink-muted" data-incoming-address>
              {t.projectSetup.editor.postTo}
              <span className="font-mono text-ink">/api/v1/automation/hooks/{rule.trigger.token}</span>
              {t.projectSetup.editor.postToAfter}
            </p>
          )}
        </fieldset>

        <fieldset className="space-y-3">
          <legend className="text-sm font-medium text-ink">{t.projectSetup.editor.if}</legend>
          {draft.conditions.map((c, i) => {
            const d = conditions.find((x) => x.type === c.kind);
            return (
              <div key={i} className="flex items-start gap-2 rounded-control border border-border p-3" data-rule-condition={c.kind}>
                <div className="flex-1 space-y-2">
                  <Select
                    label={t.projectSetup.editor.condition}
                    id={`field-condition-${i}`}
                    value={c.kind}
                    onChange={(e) => setCondition(i, { kind: e.target.value, query: undefined, field: undefined, value: undefined, roles: undefined })}
                    hint={d?.description}
                  >
                    {conditions.map((x) => (
                      <option key={x.type} value={x.type}>
                        {x.label}
                      </option>
                    ))}
                  </Select>
                  {c.kind === "nql" && (
                    <Field
                      label={t.projectSetup.editor.query}
                      id={`field-condition-query-${i}`}
                      required
                      value={c.query ?? ""}
                      onChange={(e) => setCondition(i, { query: e.target.value })}
                    />
                  )}
                  {c.kind === "field_equals" && (
                    <div className="grid grid-cols-2 gap-3">
                      <Field
                        label={t.projectSetup.editor.field}
                        id={`field-condition-field-${i}`}
                        required
                        value={c.field ?? ""}
                        onChange={(e) => setCondition(i, { field: e.target.value })}
                        placeholder="priority"
                      />
                      <Field
                        label={t.projectSetup.editor.value}
                        id={`field-condition-value-${i}`}
                        required
                        value={c.value ?? ""}
                        onChange={(e) => setCondition(i, { value: e.target.value })}
                        placeholder="high"
                      />
                    </div>
                  )}
                  {c.kind === "actor_role" && (
                    <div className="flex flex-wrap gap-3">
                      {(d?.options[0]?.choices ?? []).map((role) => (
                        <Checkbox
                          key={role}
                          label={role}
                          checked={(c.roles ?? []).includes(role)}
                          onChange={(e) =>
                            setCondition(i, { roles: e.target.checked ? [...(c.roles ?? []), role] : (c.roles ?? []).filter((r) => r !== role) })
                          }
                        />
                      ))}
                    </div>
                  )}
                </div>
                <IconButton
                  icon={<Icon.X />}
                  label={t.projectSetup.editor.removeCondition}
                  size="sm"
                  onClick={() => setDraft({ ...draft, conditions: draft.conditions.filter((_, j) => j !== i) })}
                />
              </div>
            );
          })}
          <Button
            variant="secondary"
            size="sm"
            icon={<Icon.Plus />}
            onClick={() => setDraft({ ...draft, conditions: [...draft.conditions, { kind: "nql" }] })}
            data-action="add-condition"
          >
            {t.projectSetup.editor.addCondition}
          </Button>
        </fieldset>

        <fieldset className="space-y-3">
          <legend className="text-sm font-medium text-ink">{t.projectSetup.editor.then}</legend>
          {draft.actions.map((a, i) => {
            const d = actions.find((x) => x.type === a.kind);
            return (
              <div key={i} className="flex items-start gap-2 rounded-control border border-border p-3" data-rule-action={a.kind}>
                <div className="flex-1 space-y-2">
                  <Select
                    label={t.projectSetup.editor.action}
                    id={`field-action-${i}`}
                    value={a.kind}
                    onChange={(e) =>
                      setAction(i, {
                        kind: e.target.value,
                        field: undefined,
                        value: undefined,
                        text: undefined,
                        subject: undefined,
                        to: undefined,
                        endpointId: undefined,
                      })
                    }
                    hint={d?.description}
                  >
                    {actions.map((x) => (
                      <option key={x.type} value={x.type}>
                        {x.label}
                      </option>
                    ))}
                  </Select>
                  {a.kind === "set_field" && (
                    <div className="grid grid-cols-2 gap-3">
                      <Field
                        label={t.projectSetup.editor.field}
                        id={`field-action-field-${i}`}
                        required
                        value={a.field ?? ""}
                        onChange={(e) => setAction(i, { field: e.target.value })}
                        placeholder="priority"
                      />
                      <Field
                        label={t.projectSetup.editor.value}
                        id={`field-action-value-${i}`}
                        required
                        value={a.value ?? ""}
                        onChange={(e) => setAction(i, { value: e.target.value })}
                        placeholder="high"
                      />
                    </div>
                  )}
                  {(a.kind === "transition" || a.kind === "assign") && (
                    <Field
                      label={a.kind === "transition" ? t.projectSetup.editor.transition : t.projectSetup.editor.who}
                      id={`field-action-value-${i}`}
                      required
                      value={a.value ?? ""}
                      onChange={(e) => setAction(i, { value: e.target.value })}
                    />
                  )}
                  {a.kind === "add_label" && (
                    <Labelled id={`field-action-value-${i}`} label={t.projectSetup.editor.label}>
                      <LabelInput
                        id={`field-action-value-${i}`}
                        mode="one"
                        value={a.value ? [a.value] : []}
                        onChange={(names) => setAction(i, { value: names[0] })}
                      />
                    </Labelled>
                  )}
                  {(a.kind === "add_comment" || a.kind === "create_subissue" || a.kind === "create_issue") && (
                    <Field
                      label={a.kind === "add_comment" ? t.projectSetup.editor.comment : t.projectSetup.editor.summary}
                      id={`field-action-text-${i}`}
                      required
                      rows={2}
                      value={a.text ?? ""}
                      onChange={(e) => setAction(i, { text: e.target.value })}
                    />
                  )}
                  {a.kind === "send_webhook" && (
                    <Select
                      label={t.projectSetup.editor.endpoint}
                      id={`field-action-endpoint-${i}`}
                      value={a.endpointId ?? ""}
                      onChange={(e) => setAction(i, { endpointId: e.target.value || undefined })}
                    >
                      <option value="">{t.projectSetup.editor.chooseWebhook}</option>
                      {webhooks.map((w) => (
                        <option key={w.id} value={w.id}>
                          {w.name}
                        </option>
                      ))}
                    </Select>
                  )}
                  {a.kind === "send_mail" && (
                    <>
                      <div className="grid grid-cols-2 gap-3">
                        <Field
                          label={t.projectSetup.editor.to}
                          id={`field-action-to-${i}`}
                          required
                          value={a.to ?? ""}
                          onChange={(e) => setAction(i, { to: e.target.value })}
                          placeholder={t.projectSetup.editor.toPlaceholder}
                        />
                        <Field
                          label={t.projectSetup.editor.subject}
                          id={`field-action-subject-${i}`}
                          required
                          value={a.subject ?? ""}
                          onChange={(e) => setAction(i, { subject: e.target.value })}
                        />
                      </div>
                      <Field
                        label={t.projectSetup.editor.body}
                        id={`field-action-text-${i}`}
                        rows={2}
                        value={a.text ?? ""}
                        onChange={(e) => setAction(i, { text: e.target.value })}
                      />
                    </>
                  )}
                </div>
                <IconButton
                  icon={<Icon.X />}
                  label={t.projectSetup.editor.removeAction}
                  size="sm"
                  disabled={draft.actions.length === 1}
                  onClick={() => setDraft({ ...draft, actions: draft.actions.filter((_, j) => j !== i) })}
                />
              </div>
            );
          })}
          <Button
            variant="secondary"
            size="sm"
            icon={<Icon.Plus />}
            onClick={() => setDraft({ ...draft, actions: [...draft.actions, { kind: "add_label" }] })}
            data-action="add-action"
          >
            {t.projectSetup.editor.addAction}
          </Button>
        </fieldset>

        <div className="flex flex-wrap items-end gap-5">
          <Field
            label={t.projectSetup.editor.cap}
            type="number"
            min={1}
            value={String(draft.hourlyCap ?? 100)}
            onChange={(e) => setDraft({ ...draft, hourlyCap: Number(e.target.value) })}
            className="w-24"
          />
          <Checkbox
            label={t.projectSetup.editor.ownChanges}
            checked={draft.allowOwnEvents}
            onChange={(e) => setDraft({ ...draft, allowOwnEvents: e.target.checked })}
            className="text-ink-muted"
          />
        </div>
        {/* Last, just above the button that caused it, and scrolled to. */}
        {error && <ErrorBanner reveal>{error}</ErrorBanner>}
      </form>
    </Dialog>
  );
}
