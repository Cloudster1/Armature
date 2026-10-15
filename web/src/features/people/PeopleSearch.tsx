import { useEffect, useRef, useState } from "react";
import { Input } from "@/components/ui";
import { PERSON_SEARCH_DELAY_MS } from "@/config";

/** The search box over a list of people; it writes the address once typing pauses, not per letter. */
export function PeopleSearch({ value, onChange, label = "Find people" }: { value: string; onChange: (text: string) => void; label?: string }) {
  const [text, setText] = useState(value);
  const timer = useRef<number | undefined>(undefined);
  const latest = useRef(onChange);
  useEffect(() => {
    latest.current = onChange;
  });
  // A change from elsewhere, such as going back, shows in the box; while typing, the box leads.
  useEffect(() => {
    if (timer.current === undefined) setText(value);
  }, [value]);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  function type(next: string) {
    setText(next);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      timer.current = undefined;
      latest.current(next.trim());
    }, PERSON_SEARCH_DELAY_MS);
  }

  return (
    <Input
      type="search"
      controlSize="sm"
      aria-label={label}
      placeholder="Find by name or address"
      value={text}
      onChange={(event) => type(event.target.value)}
      className="w-64"
      data-people-search=""
    />
  );
}
