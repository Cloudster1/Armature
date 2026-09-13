import type { LabelColor, LabelRef } from "@/api/issues";
import { IconButton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";

/** The palette, as classes; the server names the colour, the client draws it. */
export const LABEL_CLASSES: Record<LabelColor, string> = {
  gray: "bg-stone-200 text-stone-800 dark:bg-stone-700 dark:text-stone-100",
  red: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-100",
  orange: "bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-100",
  amber: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-100",
  green: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100",
  teal: "bg-teal-100 text-teal-800 dark:bg-teal-900 dark:text-teal-100",
  blue: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-100",
  purple: "bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-100",
  pink: "bg-pink-100 text-pink-800 dark:bg-pink-900 dark:text-pink-100",
};

/** One label, as a coloured word. */
export function LabelChip({
  label,
  onRemove,
  className,
}: {
  label: Pick<LabelRef, "name" | "color">;
  onRemove?: () => void;
  className?: string;
}) {
  return (
    <span
      data-label={label.name}
      className={cx(
        "inline-flex items-center gap-1 rounded px-1.5 py-px text-2xs font-medium",
        LABEL_CLASSES[label.color] ?? LABEL_CLASSES.gray,
        className,
      )}
    >
      {label.name}
      {onRemove && (
        <IconButton icon={<Icon.X className="size-3" />} label={`Remove label ${label.name}`} size="xs" onClick={onRemove} className="-mr-1 size-4 text-current opacity-70 hover:bg-transparent hover:text-current hover:opacity-100" />
      )}
    </span>
  );
}

/** A row of labels, or nothing. */
export function LabelChips({ labels, className }: { labels: LabelRef[]; className?: string }) {
  if (labels.length === 0) return null;
  return (
    <span className={cx("inline-flex flex-wrap gap-1", className)}>
      {labels.map((label) => (
        <LabelChip key={label.id} label={label} />
      ))}
    </span>
  );
}
