import type { ButtonHTMLAttributes } from "react";
import { cx } from "./cx";

/** A filter that is on or off. It says so with aria-pressed, which the tests read. */
export function Chip({ pressed, className, children, ...rest }: ButtonHTMLAttributes<HTMLButtonElement> & { pressed: boolean }) {
  return (
    <button
      {...rest}
      type="button"
      aria-pressed={pressed}
      className={cx(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-sm whitespace-nowrap transition-colors",
        pressed ? "border-accent bg-accent-subtle font-medium text-accent" : "border-border text-ink-muted hover:border-border-strong hover:text-ink",
        className,
      )}
    >
      {children}
    </button>
  );
}
