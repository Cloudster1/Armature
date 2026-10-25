import { useState } from "react";
import {
  useAutomationCatalog,
  useCreateRule,
  useDeleteRule,
  useRuleRuns,
  useRules,
  useRunRule,
  useUpdateRule,
  useWebhooks,
  type Rule,
  type RuleInput,
} from "@/api/automation";
import { Button, Drawer, EmptyState, IconButton, Menu, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useFormat } from "@/lib/format";
import { describeRule } from "./describe";
import { RuleEditor } from "./RuleEditor";
import { t } from "@/i18n";

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
      create.mutate(input, {
        onSuccess: () => {
          setEditing(null);
          toast.success(t.projectSetup.rules.created);
        },
      });
    } else if (editing) {
      update.mutate(
        { id: editing.id, ...input },
        {
          onSuccess: () => {
            setEditing(null);
            toast.success(t.projectSetup.rules.saved);
          },
        },
      );
    }
  }

  function toggle(rule: Rule) {
    update.mutate({
      id: rule.id,
      name: rule.name,
      enabled: !rule.enabled,
      trigger: rule.trigger,
      conditions: rule.conditions,
      actions: rule.actions,
      allowOwnEvents: rule.allowOwnEvents,
      hourlyCap: rule.hourlyCap,
    });
  }

  return (
    <div className="space-y-4">
      {canEdit && (
        <div className="flex justify-end">
          <Button icon={<Icon.Plus />} onClick={() => setEditing("new")} data-action="new-rule" data-guide="new-rule">
            {t.projectSetup.rules.newRule}
          </Button>
        </div>
      )}
      {isLoading ? null : rules.length === 0 ? (
        <EmptyState icon={<Icon.Bolt />} title={t.projectSetup.rules.empty} description={t.projectSetup.rules.emptyDetail} />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>{t.projectSetup.rules.rule}</Th>
              <Th>{t.projectSetup.rules.does}</Th>
              <Th>{t.projectSetup.rules.lastRun}</Th>
              <Th>{t.projectSetup.rules.state}</Th>
              {canEdit && <Th className="w-12" />}
            </tr>
          </thead>
          <tbody>
            {rules.map((rule) => (
              <tr key={rule.id} data-rule={rule.name} data-rule-enabled={rule.enabled ? "true" : "false"}>
                <Td className="font-medium text-ink">{rule.name}</Td>
                <Td className="max-w-md text-sm text-ink-muted">{describeRule(rule, catalog)}</Td>
                <Td className="text-sm text-ink-muted">{rule.lastRunAt ? format.relative(rule.lastRunAt) : t.projectSetup.rules.never}</Td>
                <Td>
                  <Tag className={rule.enabled ? "" : "text-ink-subtle"}>{rule.enabled ? t.projectSetup.rules.on : t.projectSetup.rules.off}</Tag>
                </Td>
                {canEdit && (
                  <Td>
                    <Menu
                      label={t.projectSetup.rules.actionsFor(rule.name)}
                      align="end"
                      trigger={(props) => (
                        <IconButton
                          icon={<Icon.More />}
                          label={t.projectSetup.rules.actionsFor(rule.name)}
                          size="sm"
                          onClick={props.toggle}
                          aria-haspopup={props["aria-haspopup"]}
                          aria-expanded={props["aria-expanded"]}
                          data-rule-menu={rule.name}
                        />
                      )}
                      items={[
                        { label: t.projectSetup.rules.runLog, icon: <Icon.Clock />, onSelect: () => setLogOf(rule), attrs: { "data-action": "rule-log" } },
                        {
                          label: t.projectSetup.rules.runNow,
                          icon: <Icon.Bolt />,
                          onSelect: () =>
                            run.mutate({ id: rule.id }, { onSuccess: (r) => toast.info(t.projectSetup.rules.ran(r.run.outcome, r.run.reason ?? "")) }),
                          attrs: { "data-action": "rule-run" },
                        },
                        { label: t.projectSetup.rules.edit, icon: <Icon.Edit />, onSelect: () => setEditing(rule), attrs: { "data-action": "rule-edit" } },
                        {
                          label: rule.enabled ? t.projectSetup.rules.turnOff : t.projectSetup.rules.turnOn,
                          icon: rule.enabled ? <Icon.EyeOff /> : <Icon.Eye />,
                          onSelect: () => toggle(rule),
                          attrs: { "data-action": "rule-toggle" },
                        },
                        {
                          label: t.projectSetup.rules.delete,
                          icon: <Icon.Trash />,
                          danger: true,
                          onSelect: async () =>
                            (await confirm({
                              noun: t.kit.confirm.things.rule,
                              verb: t.kit.confirm.verbs.delete,
                              body: t.projectSetup.rules.deleteBody(rule.name),
                            })) && remove.mutate(rule.id),
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
    <Drawer open onClose={onClose} title={t.projectSetup.rules.runsOf(rule.name)} attrs={{ "data-rule-log": rule.name }}>
      {runs.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.projectSetup.rules.notRun}</p>
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
                  {a.ok ? "" : t.projectSetup.rules.failed}
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
