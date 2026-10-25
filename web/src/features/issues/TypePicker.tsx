import { type Issue, type IssueType, type Status, useIssueTypes, useUpdateIssue } from "@/api/issues";
import { Icon } from "@/components/icons";
import { Button, Menu, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { TypeBadge } from "./badges";
import { typeChoices, useTypeChange } from "./typeChange";
import { t } from "@/i18n";

/** The issue's type in its header, which anybody who may edit can change. */
export function TypeField({ issue, editable }: { issue: Issue; editable: boolean }) {
  const { data } = useIssueTypes();
  const choices = editable ? typeChoices(data?.issueTypes ?? [], issue) : [];
  if (choices.length === 0) return <TypeBadge icon={issue.type.icon} name={issue.type.name} />;
  return <TypePicker issue={issue} choices={choices} />;
}

// A change that moves the status is asked about first, so nobody loses where
// the issue was without being told; one that keeps it simply happens.
function TypePicker({ issue, choices }: { issue: Issue; choices: IssueType[] }) {
  const update = useUpdateIssue();
  const landing = useTypeChange(issue);
  const confirm = useConfirm();
  const toast = useToast();

  async function change(type: IssueType) {
    let lands: Status | null;
    try {
      lands = await landing(type.id);
    } catch (error) {
      toast.error((error as Error).message);
      return;
    }
    if (lands) {
      const body = t.issueList.typePicker.moves(issue.status.name, lands.name);
      if (!(await confirm({ verb: t.issueList.typePicker.change, noun: t.issueList.typePicker.changeNoun(issue.key, type.name), body }))) return;
    }
    update.mutate({ key: issue.key, typeId: type.id }, { onSuccess: () => toast.success(t.issueList.typePicker.changed(type.name)) });
  }

  return (
    <Menu
      label={t.issueList.typePicker.menu}
      trigger={(props) => (
        <Button
          variant="ghost"
          size="sm"
          onClick={props.toggle}
          aria-haspopup={props["aria-haspopup"]}
          aria-expanded={props["aria-expanded"]}
          aria-label={t.issueList.typePicker.current(issue.type.name)}
          title={t.issueList.typePicker.changeTitle}
          loading={update.isPending}
          iconRight={<Icon.ChevronDown className="size-3 text-ink-subtle" />}
          className="gap-0.5 px-1!"
          data-action="change-type"
        >
          <TypeBadge icon={issue.type.icon} name={issue.type.name} />
        </Button>
      )}
      items={choices.map((type) => ({
        label: type.name,
        icon: (
          <span aria-hidden="true">
            <TypeBadge icon={type.icon} name={type.name} />
          </span>
        ),
        onSelect: () => void change(type),
        attrs: { "data-type-option": type.name },
      }))}
    />
  );
}
