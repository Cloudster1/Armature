import { useState } from "react";
import type { Arrangement, ArrangementInput, Area, Placement } from "@/api/arrange";
import type { Field } from "@/api/fields";
import { Button, Card, ErrorBanner, SelectInput, Tag } from "@/components/ui";
import { areaLabels, originWords, slotLabel } from "./labels";
import { t } from "@/i18n";

/** What a place is called, however it is named. */
function nameOf(place: Placement, fields: Field[]): string {
  if (place.fieldId) return fields.find((f) => f.id === place.fieldId)?.name ?? t.looks.arrange.aField;
  return place.slot ? slotLabel(place.slot) : "";
}

function keyOf(place: Placement): string {
  return place.fieldId ?? place.slot ?? "";
}

export function ArrangementEditor({
  arrangements,
  fields,
  saving,
  error,
  canFollow,
  readOnly = false,
  onSave,
}: {
  arrangements: Arrangement[];
  fields: Field[];
  saving: boolean;
  error: Error | null;
  /** A project can hand a type back; the organization has nobody to hand it to. */
  canFollow: boolean;
  /** Shown to somebody who may see the arrangement but not change it. */
  readOnly?: boolean;
  onSave: (input: ArrangementInput) => void;
}) {
  const [typeId, setTypeId] = useState(arrangements[0]?.issueTypeId ?? "");
  const found = arrangements.find((each) => each.issueTypeId === typeId) ?? arrangements[0];
  if (!found) return null;
  const current = found;
  const places = current.places;

  function write(next: Placement[]) {
    onSave({ issueTypeId: current.issueTypeId, places: next });
  }

  function moveTo(place: Placement, area: Area) {
    write([...places.filter((each) => keyOf(each) !== keyOf(place)), { ...place, area }]);
  }

  function shift(place: Placement, by: -1 | 1) {
    const inArea = places.filter((each) => each.area === place.area);
    const at = inArea.findIndex((each) => keyOf(each) === keyOf(place));
    const to = at + by;
    if (to < 0 || to >= inArea.length) return;
    const reordered = [...inArea];
    const here = reordered[at];
    const there = reordered[to];
    if (!here || !there) return;
    reordered[at] = there;
    reordered[to] = here;
    write([...places.filter((each) => each.area !== place.area), ...reordered]);
  }

  return (
    <div data-arrangement>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <label htmlFor="arrange-type" className="text-sm text-ink-muted">
          {t.looks.arrange.issueType}
        </label>
        <SelectInput id="arrange-type" value={current.issueTypeId} onChange={(e) => setTypeId(e.target.value)} data-arrange-type>
          {arrangements.map((each) => (
            <option key={each.issueTypeId} value={each.issueTypeId}>
              {each.issueTypeName}
            </option>
          ))}
        </SelectInput>
        <Tag data-arrange-origin={current.origin.scope}>{originWords(current.origin.scope)}</Tag>
        {canFollow && !readOnly && current.origin.scope === "project" && (
          <Button
            variant="secondary"
            size="sm"
            loading={saving}
            onClick={() => onSave({ issueTypeId: current.issueTypeId, places: null })}
            data-action="follow-the-organization"
          >
            {t.looks.arrange.follow}
          </Button>
        )}
      </div>

      {error && <ErrorBanner>{error.message}</ErrorBanner>}

      <fieldset disabled={readOnly} className="grid min-w-0 gap-3 md:grid-cols-2">
        {areaLabels().map(({ area, title, note }) => (
          <AreaColumn
            key={area}
            area={area}
            title={title}
            note={note}
            places={places.filter((place) => place.area === area)}
            fields={fields}
            saving={saving}
            onDrop={(place) => moveTo(place, area)}
            onMove={moveTo}
            onShift={shift}
            find={(key) => places.find((place) => keyOf(place) === key)}
            readOnly={readOnly}
          />
        ))}
      </fieldset>
    </div>
  );
}

function AreaColumn({
  area,
  title,
  note,
  places,
  fields,
  saving,
  onDrop,
  onMove,
  onShift,
  find,
  readOnly,
}: {
  area: Area;
  title: string;
  note: string;
  places: Placement[];
  fields: Field[];
  saving: boolean;
  onDrop: (place: Placement) => void;
  onMove: (place: Placement, area: Area) => void;
  onShift: (place: Placement, by: -1 | 1) => void;
  find: (key: string) => Placement | undefined;
  readOnly: boolean;
}) {
  return (
    <Card
      className="p-3"
      data-arrange-area={area}
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault();
        const place = find(e.dataTransfer.getData("text/plain"));
        if (place && !readOnly) onDrop(place);
      }}
    >
      <h3 className="text-2xs font-medium tracking-wide text-ink-subtle uppercase">{title}</h3>
      {note && <p className="mt-1 text-xs text-ink-subtle">{note}</p>}
      <ul className="mt-2 space-y-1">
        {places.length === 0 && <li className="text-sm text-ink-subtle">{t.looks.arrange.nothing}</li>}
        {places.map((place, at) => (
          <li
            key={keyOf(place)}
            draggable={!readOnly}
            onDragStart={(e) => e.dataTransfer.setData("text/plain", keyOf(place))}
            className="flex items-center gap-1 rounded-control border border-border bg-canvas px-2 py-1"
            data-arrange-place={keyOf(place)}
          >
            <span className="min-w-0 flex-1 truncate text-sm text-ink">{nameOf(place, fields)}</span>
            <Button variant="ghost" size="sm" disabled={saving || at === 0} onClick={() => onShift(place, -1)} data-action="move-earlier">
              {t.looks.arrange.up}
            </Button>
            <Button variant="ghost" size="sm" disabled={saving || at === places.length - 1} onClick={() => onShift(place, 1)} data-action="move-later">
              {t.looks.arrange.down}
            </Button>
            <label htmlFor={`arrange-where-${keyOf(place)}`} className="sr-only">
              {t.looks.arrange.whereGoes(nameOf(place, fields))}
            </label>
            <SelectInput
              id={`arrange-where-${keyOf(place)}`}
              controlSize="sm"
              value={area}
              onChange={(e) => onMove(place, e.target.value as Area)}
              data-arrange-where={keyOf(place)}
            >
              {areaLabels().map((each) => (
                <option key={each.area} value={each.area}>
                  {each.title}
                </option>
              ))}
            </SelectInput>
          </li>
        ))}
      </ul>
    </Card>
  );
}
