import type { ReactNode } from "react";
import { cx } from "./cx";

export type PageWidth = "narrow" | "content" | "wide";

// Three widths for the whole product: forms, reading, and canvases. A page
// picks one by what it is, not by how much it happens to hold today.
const widths: Record<PageWidth, string> = {
  narrow: "mx-auto max-w-[44rem]",
  content: "mx-auto max-w-[72rem]",
  wide: "w-full",
};

export function Page({ width = "content", children, className }: { width?: PageWidth; children: ReactNode; className?: string }) {
  return <div className={cx(widths[width], className)}>{children}</div>;
}
