import { Card, SelectInput, Table, Td, Th } from "@/components/ui";
import type { ImportColumn } from "@/api/filters";
import { MANY_VALUED, TARGET_LABELS } from "./targets";

/** Which column of the file fills which target, by position. */
export function MappingCard({
  columns,
  targets,
  mapping,
  onChange,
}: {
  columns: ImportColumn[];
  targets: string[];
  mapping: Record<string, number[]>;
  onChange: (mapping: Record<string, number[]>) => void;
}) {
  function holderOf(at: number): string {
    return Object.entries(mapping).find(([, positions]) => positions.includes(at))?.[0] ?? "";
  }

  function pick(at: number, target: string) {
    const next: Record<string, number[]> = {};
    for (const [t, positions] of Object.entries(mapping)) {
      const kept = positions.filter((p) => p !== at);
      if (kept.length) next[t] = kept;
    }
    if (target) next[target] = MANY_VALUED.has(target) ? [...(next[target] ?? []), at].sort((a, b) => a - b) : [at];
    onChange(next);
  }

  return (
    <Card className="mb-4 p-5" data-import-mapping>
      <h2 className="mb-3 text-sm font-medium text-ink">Which column is which</h2>
      <Table dense>
        <thead>
          <tr>
            <Th>Column in the file</Th>
            <Th>Becomes</Th>
            <Th>First values</Th>
          </tr>
        </thead>
        <tbody>
          {columns.map((column) => (
            <tr key={column.at} data-import-column={column.name}>
              <Td className="font-mono text-sm text-ink">
                {column.name}
                <span className="ml-1 text-ink-subtle">#{column.at + 1}</span>
              </Td>
              <Td>
                <label htmlFor={`field-map-${column.at}`} className="sr-only">
                  What {column.name} becomes
                </label>
                <SelectInput
                  id={`field-map-${column.at}`}
                  controlSize="sm"
                  value={holderOf(column.at)}
                  onChange={(e) => pick(column.at, e.target.value)}
                  data-import-target={column.name}
                >
                  <option value="">Skip this column</option>
                  {targets.map((t) => (
                    <option key={t} value={t}>
                      {TARGET_LABELS[t] ?? t}
                    </option>
                  ))}
                </SelectInput>
              </Td>
              <Td className="max-w-xs truncate text-sm text-ink-muted">{column.samples.join(" | ")}</Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </Card>
  );
}
