import { useRef } from "react";
import { useIssue } from "@/api/issues";
import { useArrangements } from "@/api/arrange";
import { useMe } from "@/api/auth";
import { useProject } from "@/api/projects";
import { canWriteIssues, useAccess } from "@/api/access";
import { ErrorBanner, Skeleton, cx } from "@/components/ui";
import { IssueHeader } from "./IssueHeader";
import { Rail } from "./IssueRail";
import { IssueMain } from "./IssueMain";
import { IssueAside } from "./IssueAside";

// The issue in two columns, with a rail to jump between its sections. The
// same component is the page and the drawer.
export function IssuePage({ issueKey, inDrawer = false }: { issueKey: string; inDrawer?: boolean }) {
  const { data, isLoading, error } = useIssue(issueKey);
  const { data: access } = useAccess();
  const { data: session } = useMe();
  const { data: projectData } = useProject(data?.issue?.projectKey ?? "");
  // The project is in the key, so the arrangement is asked for in the same
  // breath as the issue rather than after it has arrived.
  const { data: arranged } = useArrangements(data?.issue?.projectKey ?? projectOf(issueKey));
  const rootRef = useRef<HTMLDivElement>(null);

  if (isLoading) return <Skeleton lines={6} />;
  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  const issue = data?.issue;
  if (!issue) return null;
  const editable = canWriteIssues(access, issue.projectKey);
  const me = session?.principal?.user.id;
  // On a desk the files are part of what the customer is told.
  const isDesk = projectData?.project?.kind === "service";
  // The arrangement is the server's answer, never a guess made here, so the
  // columns wait for it; the issue itself is on the page either way.
  const arrangement = arranged?.arrangements.find((each) => each.issueTypeId === issue.type.id);

  return (
    <div data-issue-page={issue.key} ref={rootRef}>
      <IssueHeader issue={issue} editable={editable} inDrawer={inDrawer} />
      <Rail root={rootRef} />
      {arrangement ? (
        <div className={cx("grid gap-8", inDrawer ? "grid-cols-1" : "lg:grid-cols-[1fr_20rem]")}>
          <IssueMain issue={issue} editable={editable} me={me} isDesk={isDesk} places={arrangement.places} />
          <IssueAside issue={issue} editable={editable} places={arrangement.places} />
        </div>
      ) : (
        <Skeleton lines={6} />
      )}
    </div>
  );
}

/** The project an issue belongs to, read off its key. */
function projectOf(issueKey: string): string {
  const at = issueKey.lastIndexOf("-");
  return at > 0 ? issueKey.slice(0, at) : "";
}
