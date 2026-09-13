import { DockedPanel, Drawer, IconButton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ISSUE_PANEL_DOCK_MIN_PX, ISSUE_PANEL_WIDTH } from "@/config";
import { useMinWidth } from "@/features/shell/state";
import { indexOf, useIssueDrawer } from "./IssueDrawer";
import { IssuePage } from "./IssuePage";

/**
 * The issue beside the list: a docked column on a wide screen, the drawer on
 * a narrow one. Either way the same page, with previous and next in its head.
 */
export function IssuePanel() {
  const { current, close, keys, step } = useIssueDrawer();
  const docked = useMinWidth(ISSUE_PANEL_DOCK_MIN_PX);
  if (current === null) return null;
  const at = indexOf(keys, current);
  const actions = keys.length > 0 && (
    <>
      <IconButton icon={<Icon.ChevronUp />} label="Previous issue" size="sm" onClick={() => step(-1)} disabled={at <= 0} data-action="panel-prev" />
      <IconButton icon={<Icon.ChevronDown />} label="Next issue" size="sm" onClick={() => step(1)} disabled={at >= keys.length - 1} data-action="panel-next" />
    </>
  );
  const body = <IssuePage issueKey={current} inDrawer />;
  if (docked) {
    return (
      <DockedPanel open onClose={close} title={current} width={ISSUE_PANEL_WIDTH} actions={actions} attrs={{ "data-issue-panel": current }}>
        {body}
      </DockedPanel>
    );
  }
  return (
    <Drawer open onClose={close} title={current} actions={actions} attrs={{ "data-issue-drawer": current, "data-issue-panel": current }}>
      {body}
    </Drawer>
  );
}
