import { useEffect, useState } from "react";
import { useIssueFields, useSetFieldValue, type FieldValue } from "@/api/fields";
import { Checkbox, ErrorBanner, Input, SelectInput, cx } from "@/components/ui";
import { badInputMessage, inputFromValue, inputTypeFor, valueFromInput } from "./values";

/**
 * A project's custom fields on one issue, each drawn as the control its kind
 * asks for. An answer is saved when the control loses focus or changes, and
 * the API's refusal, if any, is shown under it.
 */
export function IssueFields({ issueKey, editable, except = [] }: { issueKey: string; editable: boolean; except?: string[] }) {
  const { data } = useIssueFields(issueKey);
  const values = (data?.values ?? []).filter((v) => !except.includes(v.field.id));
  if (values.length === 0) return null;
  return (
    <>
      {values.map((v) => (
        <FieldRow key={v.field.id} issueKey={issueKey} value={v} editable={editable} />
      ))}
    </>
  );
}

/** One of the project's fields, wherever the arrangement put it. */
export function IssueField({ issueKey, fieldId, editable }: { issueKey: string; fieldId: string; editable: boolean }) {
  const { data } = useIssueFields(issueKey);
  const value = (data?.values ?? []).find((v) => v.field.id === fieldId);
  if (!value) return null;
  return <FieldRow issueKey={issueKey} value={value} editable={editable} />;
}

function FieldRow({ issueKey, value, editable }: { issueKey: string; value: FieldValue; editable: boolean }) {
  const set = useSetFieldValue();
  const { field } = value;
  const [text, setText] = useState(inputFromValue(field.kind, value.value));
  // What the browser could not parse, said in words rather than lost.
  const [refused, setRefused] = useState<string | null>(null);

  // The server's answer wins over whatever was half typed when it arrived.
  useEffect(() => setText(inputFromValue(field.kind, value.value)), [field.kind, value.value]);

  function save(raw: string | boolean) {
    setRefused(null);
    const next = valueFromInput(field.kind, raw);
    const current = value.value ?? null;
    if (next === current) return;
    set.mutate({ issueKey, fieldId: field.id, value: next });
  }

  const wide = field.kind === "url" || field.kind === "text";
  const shown = value.display ?? "";

  return (
    <div className={wide ? "py-2" : "grid grid-cols-[5.5rem_minmax(0,1fr)] items-center gap-x-3 py-2"} data-custom-field={field.name}>
      <dt className={cx("text-sm text-ink-muted", wide && "mb-1")}>{field.name}</dt>
      <dd className="flex min-w-0 flex-col items-end gap-1 text-sm">
        {!editable ? (
          <ReadOnly value={value} />
        ) : field.kind === "select" ? (
          <SelectInput
            aria-label={field.name}
            controlSize="sm"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              save(e.target.value);
            }}
            className="w-full min-w-0"
          >
            <option value="">None</option>
            {field.options.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
            {text !== "" && !field.options.includes(text) && (
              // A choice the project no longer offers is still what somebody
              // said; it stays visible and cannot be chosen again.
              <option value={text} disabled>
                {text} (no longer offered)
              </option>
            )}
          </SelectInput>
        ) : field.kind === "checkbox" ? (
          <Checkbox
            label={<span className="sr-only">{field.name}</span>}
            aria-label={field.name}
            checked={text === "true"}
            onChange={(e) => {
              setText(e.target.checked ? "true" : "");
              save(e.target.checked);
            }}
          />
        ) : (
          <Input
            type={inputTypeFor(field.kind)}
            aria-label={field.name}
            controlSize="sm"
            value={text}
            step={field.kind === "number" ? "any" : undefined}
            placeholder={field.kind === "url" ? "https://" : undefined}
            onChange={(e) => setText(e.target.value)}
            onBlur={(e) => {
              // A number or date control hands back an empty value for text
              // it could not parse; saving that would clear the answer.
              if (e.target.validity.badInput) {
                setRefused(badInputMessage(field.kind, field.name));
                return;
              }
              save(text);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") (e.target as HTMLInputElement).blur();
            }}
            className={cx("text-right", wide && "text-left")}
          />
        )}
        {field.kind === "url" && shown && (
          <a href={shown} target="_blank" rel="noreferrer" className="max-w-full truncate text-accent hover:underline">
            {shown}
          </a>
        )}
        {(refused ?? (set.error as Error | null)?.message) && (
          <div className="w-full text-left">
            <ErrorBanner>{refused ?? (set.error as Error).message}</ErrorBanner>
          </div>
        )}
      </dd>
    </div>
  );
}

function ReadOnly({ value }: { value: FieldValue }) {
  const shown = value.display ?? "";
  if (!shown) return <span className="text-ink-subtle">None</span>;
  if (value.field.kind === "url") {
    return (
      <a href={shown} target="_blank" rel="noreferrer" className="max-w-full truncate text-accent hover:underline">
        {shown}
      </a>
    );
  }
  return <span className="text-ink">{shown}</span>;
}
