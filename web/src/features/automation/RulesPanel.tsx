import { useState } from "react";
import { useAutomationCatalog, useCreateRule, useDeleteRule, useRuleRuns, useRules, useRunRule, useUpdateRule, useWebhooks, type Rule, type RuleInput } from "@/api/automation";
import { Button, Drawer, EmptyState, IconButton, Menu, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useFormat } from "@/lib/format";
import { describeRule } from "./describe";
import { RuleEditor } from "./RuleEditor";

// The rules of a project, or of the organization with no key: a table of
// sentences, an editor, and a log per rule that says why each run did what
// it did.
export function RulesPanel({ projectKey, canEdit }: { projectKey?: string; canEdit: boolean }) {
  const { data, isLoading } = useRules(projectKey);
  const { data: catalogData } = useAutomationCatalog();
  const { data: hooks } = useWebhooks();
  const create = useCreateRule(projectKey);
  const update = useUpdateRule();
  const remove = useDeleteRule();
  const run = useRunRule();
  const confirm = useConfirm();
  const toast = useToast();
  const format = useFormat();
  const [editing, setEditing] = useState<Rule | null | "new">(null);
  const [logOf, setLogOf] = useState<Rule | null>(null);

  const catalog = catalogData?.catalog ?? [];
  const rules = data?.rules ?? [];
  const saving = create.isPending || update.isPending;
  const saveError = ((editing === "new" ? create.error : update.error) as Error | null)?.message;

  function save(input: RuleInput) {
    if (editing === "new") {
      create.mutate(input, { onSuccess: () => { setEditing(null); toast.success("Rule created"); } });
    } else if (editing) {
      update.mutate({ id: editing.id, ...input }, { onSuccess: () => { setEditing(null); toast.success("Rule saved"); } });
    }
  }

  function toggle(rule: Rule) {
    update.mutate({ id: rule.id, name: rule.name, enabled: !rule.enabled, trigger: rule.trigger, conditions: rule.conditions, actions: rule.actions, allowOwnEvents: rule.allowOwnEvents, hourlyCap: rule.hourlyCap });
  }

  return (
    <div className="space-y-4">
      {canEdit && (
        <div className="flex justify-end">
          <Button icon={<Icon.Plus />} onClick={() => setEditing("new")} data-action="new-rule" data-guide="new-rule">
            New rule
          </Button>
        </div>
      )}
      {isLoading ? null : rules.length === 0 ? (
        <EmptyState
          icon={<Icon.Bolt />}
          title="No rules yet"
          description="A rule watches for something, checks it, and does things for you: label moved work, assign what comes in, tell a chat room."
        />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Rule</Th>
              <Th>Does</Th>
              <Th>Last run</Th>
              <Th>State</Th>
              {canEdit && <Th className="w-12" />}
            </tr>
          </thead>
          <tbody>
            {rules.map((rule) => (
              <tr key={rule.id} data-rule={rule.name} data-rule-enabled={rule.enabled ? "true" : "false"}>
                <Td className="font-medium text-ink">{rule.name}</Td>
                <Td className="max-w-md text-sm text-ink-muted">{describeRule(rule, catalog)}</Td>
                <Td className="text-sm text-ink-muted">{rule.lastRunAt ? format.relative(rule.lastRunAt) : "never"}</Td>
                <Td>
                  <Tag className={rule.enabled ? "" : "text-ink-subtle"}>{rule.enabled ? "On" : "Off"}</Tag>
                </Td>
                {canEdit && (
                  <Td>
                    <Menu
                      label={`Actions for ${rule.name}`}
                      align="end"
                      trigger={(props) => <IconButton icon={<Icon.More />} label={`Actions for ${rule.name}`} size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} data-rule-menu={rule.name} />}
                      items={[
                        { label: "Run log", icon: <Icon.Clock />, onSelect: () => setLogOf(rule), attrs: { "data-action": "rule-log" } },
                        { label: "Run now", icon: <Icon.Bolt />, onSelect: () => run.mutate({ id: rule.id }, { onSuccess: (r) => toast.info(`Run ${r.run.outcome}${r.run.reason ? `: ${r.run.reason}` : ""}`) }), attrs: { "data-action": "rule-run" } },
                        { label: "Edit", icon: <Icon.Edit />, onSelect: () => setEditing(rule), attrs: { "data-action": "rule-edit" } },
                        { label: rule.enabled ? "Turn off" : "Turn on", icon: rule.enabled ? <Icon.EyeOff /> : <Icon.Eye />, onSelect: () => toggle(rule), attrs: { "data-action": "rule-toggle" } },
                        {
                          label: "Delete",
                          icon: <Icon.Trash />,
                          danger: true,
                          onSelect: async () => (await confirm({ noun: "rule", verb: "Delete", body: `${rule.name} and its log go; nothing it did is undone.` })) && remove.mutate(rule.id),
                          attrs: { "data-action": "rule-delete" },
                        },
                      ]}
                    />
                  </Td>
                )}
              </tr>
            ))}
          </tbody>
        </Table>
      )}

      {editing !== null && (
        <RuleEditor
          key={editing === "new" ? "new" : editing.id}
          open
          onClose={() => setEditing(null)}
          rule={editing === "new" ? null : editing}
          catalog={catalog}
          webhooks={hooks?.webhooks ?? []}
          onSave={save}
          saving={saving}
          error={saveError}
        />
      )}
      {logOf && <RunLog rule={logOf} onClose={() => setLogOf(null)} />}
    </div>
  );
}

function RunLog({ rule, onClose }: { rule: Rule; onClose: () => void }) {
  const { data } = useRuleRuns(rule.id);
  const format = useFormat();
  const runs = data?.runs ?? [];
  return (
    <Drawer open onClose={onClose} title={`Runs of ${rule.name}`} attrs={{ "data-rule-log": rule.name }}>
      {runs.length === 0 ? (
        <p className="text-sm text-ink-muted">This rule has not run yet.</p>
      ) : (
        <ul className="divide-y divide-border">
          {runs.map((r) => (
            <li key={r.id} className="space-y-1 py-3 text-sm" data-rule-run={r.outcome}>
              <div className="flex items-center gap-2">
                <Tag>{r.outcome}</Tag>
                {r.issueKey && <span className="font-mono text-ink">{r.issueKey}</span>}
                <span className="ml-auto text-xs text-ink-subtle">{format.relative(r.startedAt)}</span>
              </div>
              {r.reason && <p className="text-ink-muted">{r.reason}</p>}
              {r.actions.map((a, i) => (
                <p key={i} className={a.ok ? "text-ink-muted" : "text-danger"}>
                  {a.ok ? "" : "Failed: "}
                  {a.note}
                </p>
              ))}
            </li>
          ))}
        </ul>
      )}
    </Drawer>
  );
}
