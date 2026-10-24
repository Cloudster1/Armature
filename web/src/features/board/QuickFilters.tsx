import { useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { BOARD_SEARCH_DELAY_MS } from "@/config";
import { Avatar, Button, Chip, Input, Popover, Toolbar } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TypeBadge } from "@/features/issues/badges";
import { EMPTY_QUICK_FILTER, UNASSIGNED, isFiltered, type QuickFilter, type QuickFilterOptions } from "./quickFilter";

/** The chips above a board's lanes, which narrow the cards it already has. */
export function QuickFilters({
  filter,
  onChange,
  options,
  hidden,
  leftOut,
}: {
  filter: QuickFilter;
  onChange: (filter: QuickFilter) => void;
  options: QuickFilterOptions;
  /** How many of the lanes' cards the filter keeps off the board. */
  hidden: number;
  /** The finished work a kanban board leaves out, with the search that finds it. */
  leftOut?: { count: number; query: string; days: number };
}) {
  const [text, setText] = useState(filter.q ?? "");
  const timer = useRef<number | undefined>(undefined);
  // The timer reads the filter as it is when it fires, so a chip pressed while typing stays pressed.
  const latest = useRef({ filter, onChange });
  useEffect(() => {
    latest.current = { filter, onChange };
  });
  // A change from elsewhere, such as Clear or going back, shows in the box; while typing, the box leads.
  useEffect(() => {
    if (timer.current === undefined) setText(filter.q ?? "");
  }, [filter.q]);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  function type(value: string) {
    setText(value);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      timer.current = undefined;
      latest.current.onChange({ ...latest.current.filter, q: value });
    }, BOARD_SEARCH_DELAY_MS);
  }

  function clear() {
    window.clearTimeout(timer.current);
    timer.current = undefined;
    setText("");
    onChange(EMPTY_QUICK_FILTER);
  }

  return (
    <Toolbar
      label="Board filters"
      start={
        <>
          <Chip pressed={Boolean(filter.mine)} onClick={() => onChange({ ...filter, mine: !filter.mine || undefined })} data-quick-filter="mine">
            Only mine
          </Chip>
          <Pick
            name="Assignee"
            chosen={filter.assignees}
            onChange={(assignees) => onChange({ ...filter, assignees })}
            values={options.assignees.map((person) => ({
              value: person.id,
              label: person.name,
              icon: person.id === UNASSIGNED ? undefined : <Avatar name={person.name} src={person.avatarUrl} size="sm" />,
            }))}
          />
          <Pick
            name="Label"
            chosen={filter.labels}
            onChange={(labels) => onChange({ ...filter, labels })}
            values={options.labels.map((label) => ({ value: label.id, label: label.name }))}
          />
          <Pick
            name="Type"
            chosen={filter.types}
            onChange={(types) => onChange({ ...filter, types })}
            values={options.types.map((each) => ({ value: each.name, label: each.name, icon: <TypeBadge icon={each.icon} name={each.name} /> }))}
          />
          <Pick
            name="Epic"
            chosen={filter.epics}
            onChange={(epics) => onChange({ ...filter, epics })}
            values={options.epics.map((epic) => ({ value: epic.key, label: epic.summary }))}
          />
          <Input
            type="search"
            controlSize="sm"
            value={text}
            onChange={(event) => type(event.target.value)}
            placeholder="Key or summary"
            aria-label="Filter cards by key or summary"
            className="max-w-56"
          />
        </>
      }
      end={
        (leftOut || isFiltered(filter)) && (
          <>
            {leftOut && (
              <span className="text-xs text-ink-muted" data-done-left-out={leftOut.count}>
                <Link
                  to="/search"
                  search={{ q: leftOut.query }}
                  title={`Finished more than ${leftOut.days} day${leftOut.days === 1 ? "" : "s"} ago. Open them in search.`}
                  className="hover:text-accent hover:underline"
                >
                  {leftOut.count} finished issue{leftOut.count === 1 ? "" : "s"} left out
                </Link>
              </span>
            )}
            {isFiltered(filter) && (
              <>
                {hidden > 0 && (
                  <span role="status" className="text-xs text-ink-muted" data-hidden-cards={hidden}>
                    {hidden} card{hidden === 1 ? "" : "s"} hidden by the filters
                  </span>
                )}
                <Button size="sm" variant="ghost" onClick={clear}>
                  Clear filters
                </Button>
              </>
            )}
          </>
        )
      }
    />
  );
}

/** One kind of chip, whose values open beside it; a kind the board has none of is not offered. */
function Pick({
  name,
  values,
  chosen,
  onChange,
}: {
  name: string;
  values: Array<{ value: string; label: string; icon?: ReactNode }>;
  chosen: string[];
  onChange: (chosen: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  if (values.length === 0 && chosen.length === 0) return null;
  const toggle = (value: string) => onChange(chosen.includes(value) ? chosen.filter((each) => each !== value) : [...chosen, value]);
  return (
    <Popover
      open={open}
      onClose={() => setOpen(false)}
      label={`Filter by ${name.toLowerCase()}`}
      trigger={
        <Chip pressed={chosen.length > 0} aria-expanded={open} onClick={() => setOpen((was) => !was)} data-quick-filter={name.toLowerCase()}>
          {name}
          {chosen.length > 0 && <span className="tabular-nums">{chosen.length}</span>}
          <Icon.ChevronDown />
        </Chip>
      }
      className="w-80"
    >
      <div className="flex flex-wrap gap-1" role="group" aria-label={`Filter by ${name.toLowerCase()}`}>
        {values.map((each) => (
          <Chip key={each.value} pressed={chosen.includes(each.value)} onClick={() => toggle(each.value)} data-quick-filter-value={each.value}>
            {each.icon}
            {each.label}
          </Chip>
        ))}
      </div>
    </Popover>
  );
}
