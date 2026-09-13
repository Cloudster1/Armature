import type { ReactNode } from "react";
import type { Widget } from "@/api/reports";
import { Card, cx } from "@/components/ui";

/**
 * One widget on a grid: its title, its body, and the note when the filter does
 * not reach it. The dashboard and a shared link draw the same card.
 */
export function WidgetCard({
  widget,
  controls,
  notNarrowed = false,
  children,
}: {
  widget: Widget;
  /** The arrange controls beside the title. Given, even as nothing, the title keeps its row; a shared link passes none and the title stands alone. */
  controls?: ReactNode;
  /** Whether to say that the dashboard's filter counts nothing here. */
  notNarrowed?: boolean;
  children: ReactNode;
}) {
  return (
    <Card className={cx("p-4", widget.width === 2 && "md:col-span-2")} data-widget={widget.kind}>
      {controls === undefined ? (
        <h2 className="mb-3 text-sm font-medium text-ink">{widget.title}</h2>
      ) : (
        <div className="mb-3 flex items-start justify-between gap-2">
          <h2 className="text-sm font-medium text-ink">{widget.title}</h2>
          {controls}
        </div>
      )}
      {children}
      {notNarrowed && (
        <p className="mt-3 text-xs text-ink-subtle" data-not-narrowed="">
          Not narrowed by the filter: this widget counts sprints, not issues.
        </p>
      )}
    </Card>
  );
}
