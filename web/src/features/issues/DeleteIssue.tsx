import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type Issue, isAboutIssue, useDeleteIssue } from "@/api/issues";
import { Icon } from "@/components/icons";
import { IconButton, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

// Deleting takes the issue's children and everything on it, so the question
// names the issue by its key; afterwards there is nothing left here to show.
export function DeleteIssueButton({ issue }: { issue: Issue }) {
  const remove = useDeleteIssue();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const toast = useToast();
  const queryClient = useQueryClient();

  async function ask() {
    const sure = await confirm({ noun: t.kit.confirm.things.issue(issue.key), body: t.issueView.header.deleteBody(issue.key) });
    if (!sure) return;
    remove.mutate(issue.key, {
      onSuccess: async () => {
        toast.success(t.issueView.header.deleted(issue.key));
        await navigate({ to: "/projects/$projectKey", params: { projectKey: issue.projectKey } });
        queryClient.removeQueries({ predicate: (query) => isAboutIssue(query.queryKey, issue.key) });
      },
    });
  }

  return (
    <IconButton
      icon={<Icon.Trash />}
      label={t.issueView.header.delete}
      size="sm"
      aria-busy={remove.isPending}
      onClick={() => void ask()}
      data-action="delete-issue"
    />
  );
}
