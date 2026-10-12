import { cx } from "./cx";
import { WORDMARK_EMBLEM_SIZE } from "@/config";

/** The product's name with its emblem; one component so every page that names Armature names it the same way. */
export function Wordmark({ size = "md", className }: { size?: keyof typeof WORDMARK_EMBLEM_SIZE; className?: string }) {
  const px = WORDMARK_EMBLEM_SIZE[size];
  return (
    <span className={cx("inline-flex items-center gap-2", className)} data-wordmark="">
      <img src="/emblem.webp" alt="" width={px} height={px} className="rounded-md" />
      <span className="font-mono text-sm font-medium tracking-wide text-ink-muted">armature</span>
    </span>
  );
}
