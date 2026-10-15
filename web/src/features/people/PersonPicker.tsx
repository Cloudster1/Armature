import { useId, useState, type ReactNode } from "react";
import { useMemberSearch, type Member } from "@/api/people";
import { Input, Labelled, SelectInput, cx, fieldId, type ControlSize } from "@/components/ui";
import { PERSON_SEARCH_DELAY_MS } from "@/config";
import { useDebounced } from "@/lib/debounce";

/** An option already chosen: what it stands for and how it reads. */
export interface Chosen {
  id: string;
  name: string;
}

interface PersonPickerProps {
  /** The visible label; without one the picker is bare and named by aria-label alone. */
  label?: string;
  "aria-label"?: string;
  /** The chosen option's value, or empty for none. */
  value: string;
  onChange: (value: string, member?: Member) => void;
  /** What the empty choice says, such as "Unassigned"; without it there is no empty choice. */
  none?: string;
  /** The chosen one as the record names them, so it shows without looking anybody up. */
  selected?: Chosen | null;
  /** Ids not to offer, such as the people already watching. */
  exclude?: string[];
  /** Leave the service desk's customers out. */
  staff?: boolean;
  /** What a person's option stands for; their id unless said. */
  optionValue?: (member: Member) => string;
  /** How a person's option reads; their name unless said. */
  optionLabel?: (member: Member) => string;
  /** Puts the people under a heading, beside choices such as groups. */
  group?: string;
  /** Choices offered before the people, such as Nobody. */
  children?: ReactNode;
  id?: string;
  hint?: string;
  disabled?: boolean;
  controlSize?: ControlSize;
  className?: string;
  selectClassName?: string;
  [data: `data-${string}`]: string | undefined;
}

const byId = (member: Member) => member.id;
const byName = (member: Member) => member.name;

/** Chooses one person by asking the server as somebody types, so it never needs everybody. */
export function PersonPicker({
  label,
  "aria-label": ariaLabel,
  value,
  onChange,
  none,
  selected,
  exclude = [],
  staff = false,
  optionValue = byId,
  optionLabel = byName,
  group,
  children,
  id,
  hint,
  disabled,
  controlSize = "md",
  className,
  selectClassName,
  ...data
}: PersonPickerProps) {
  const [words, setWords] = useState("");
  // Who was picked here, so they stay chosen after the search moves past them.
  const [picked, setPicked] = useState<Chosen | null>(null);
  const settled = useDebounced(words.trim(), PERSON_SEARCH_DELAY_MS);
  const { data: found } = useMemberSearch(settled, { staff });
  const name = ariaLabel ?? label ?? "";
  // A bare picker gets an id of its own, so two on one page never share one.
  const ownId = useId();
  const selectId = id ?? (label ? fieldId(label) : ownId);

  const people = (found?.members ?? []).filter((member) => !exclude.includes(member.id));
  const options = people.map((member) => ({ id: optionValue(member), name: optionLabel(member), member }));
  const chosen = [picked, selected].find((each) => each && value && each.id === value);
  const kept = chosen && !options.some((option) => option.id === value) ? [{ ...chosen, member: undefined }] : [];
  const nothing = Boolean(settled) && found !== undefined && people.length === 0;
  const shownHint = nothing ? "Nobody here matches that. Try part of their name or their address." : hint;

  function choose(next: string) {
    const option = options.find((each) => each.id === next);
    if (option) setPicked({ id: option.id, name: option.name });
    onChange(next, option?.member);
  }

  const list = [...kept, ...options].map((option) => (
    <option key={option.id} value={option.id}>
      {option.name}
    </option>
  ));
  const picker = (
    <div className={cx("space-y-1.5", !label && className)} data-person-picker={selectId}>
      <Input
        type="search"
        controlSize={controlSize}
        aria-label={`Find a person for ${name}`}
        placeholder="Find by name or address"
        value={words}
        disabled={disabled}
        onChange={(event) => setWords(event.target.value)}
        // Enter in the search narrows the list; it must not submit the form around it.
        onKeyDown={(event) => event.key === "Enter" && event.preventDefault()}
        data-person-search=""
      />
      <SelectInput
        {...data}
        id={selectId}
        aria-label={ariaLabel}
        aria-describedby={label && shownHint ? `${selectId}-hint` : undefined}
        controlSize={controlSize}
        value={value}
        disabled={disabled}
        onChange={(event) => choose(event.target.value)}
        className={cx("w-full", selectClassName)}
      >
        {none !== undefined && <option value="">{none}</option>}
        {children}
        {group ? <optgroup label={group}>{list}</optgroup> : list}
      </SelectInput>
      {!label && nothing && <p className="text-xs text-ink-subtle">{shownHint}</p>}
    </div>
  );

  if (!label) return picker;
  return (
    <Labelled id={selectId} label={label} hint={shownHint} className={className}>
      {picker}
    </Labelled>
  );
}
