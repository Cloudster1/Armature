import { Fragment } from "react";
import type { Placement } from "@/api/arrange";
import type { Issue } from "@/api/issues";
import { Card } from "@/components/ui";
import { Section } from "./IssueRail";
import { Description } from "./Description";
import { Activity } from "./IssueActivity";
import { History } from "./IssueHistory";
import { ChildrenPanel } from "./hierarchy";
import { LinksPanel } from "./LinksPanel";
import { WatchersPanel } from "./WatchersPanel";
import { DevelopmentPanel } from "@/features/git/DevelopmentPanel";
import { WorklogPanel } from "@/features/time/TimeTracking";
import { AttachmentPanel } from "@/features/attachments/AttachmentPanel";
import { keyOf, namedFields, placesIn } from "./view/areas";
import { renderPlace } from "./view/slots";

/** What the issue is and what happened on it, in the order the rail offers. */
export function IssueMain({ issue, editable, me, isDesk, places }: { issue: Issue; editable: boolean; me?: string; isDesk: boolean; places: Placement[] }) {
  const main = placesIn(places, "main");
  const named = namedFields(places);
  // The description is a section of its own; anything else placed here reads
  // under it, where a long field has the width it wants.
  const described = main.some((place) => place.slot === "description");
  const alongside = main.filter((place) => place.slot !== "description");

  return (
    <div className="min-w-0 space-y-6">
      {described && (
        <Section id="description" title="Description" hidden>
          <Description issueKey={issue.key} doc={issue.description ?? null} editable={editable} />
        </Section>
      )}
      {alongside.length > 0 && (
        <Card className="p-4">
          <dl className="divide-y divide-border text-sm">
            {alongside.map((place) => (
              <Fragment key={keyOf(place)}>{renderPlace(place, { issue, editable, named })}</Fragment>
            ))}
          </dl>
        </Card>
      )}
      <Section id="activity" title="Activity">
        <Activity issueKey={issue.key} />
      </Section>
      <Section id="attachments" hidden>
        <AttachmentPanel issueKey={issue.key} editable={editable} note={isDesk ? "The customer sees these files on their request." : undefined} />
      </Section>
      <Section id="work" hidden>
        <WorklogPanel issue={issue} editable={editable} me={me} />
      </Section>
      <Section id="development" hidden>
        <DevelopmentPanel issueKey={issue.key} projectKey={issue.projectKey} />
      </Section>
      <Section id="children" hidden>
        <ChildrenPanel issueKey={issue.key} />
      </Section>
      <Section id="links" hidden>
        <LinksPanel issueKey={issue.key} editable={editable} />
      </Section>
      <Section id="watchers" hidden>
        <WatchersPanel issueKey={issue.key} editable={editable} me={me} />
      </Section>
      <Section id="history" title="History">
        <History issueKey={issue.key} />
      </Section>
    </div>
  );
}
