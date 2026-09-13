import { Card, SelectInput, Table, Td, Th } from "@/components/ui";
import { useIssueTypes, useStatuses } from "@/api/issues";
import type { ImportWord } from "@/api/filters";
import { TARGET_LABELS } from "./targets";

/** The priorities this tracker has; a file's own are mapped onto them. */
const PRIORITIES = ["lowest", "low", "medium", "high", "highest"];

/** What the file's words mean here: Jira's Requested is somebody's To Do. */
export function WordsCard({
  words,
  values,
  onChange,
}: {
  words: ImportWord[];
  values: Record<string, Record<string, string>>;
  onChange: (values: Record<string, Record<string, string>>) => void;
}) {
  const types = useIssueTypes();
  const statuses = useStatuses();
  if (words.length === 0) return null;

  function optionsFor(target: string): string[] {
    if (target === "type") return (types.data?.issueTypes ?? []).map((t) => t.name);
    if (target === "status") return (statuses.data?.statuses ?? []).map((s) => s.name);
    return PRIORITIES;
  }

  function pick(target: string, word: string, means: string) {
    onChange({ ...values, [target]: { ...(values[target] ?? {}), [word]: means } });
  }

  return (
    <Card className="mb-4 p-5" data-import-words>
      <h2 className="mb-1 text-sm font-medium text-ink">What its words mean here</h2>
      <p className="mb-3 text-sm text-ink-muted">A word this tracker does not know refuses its rows until you say what it is.</p>
      <Table dense>
        <thead>
          <tr>
            <Th>Column</Th>
            <Th>In the file</Th>
            <Th>On how many rows</Th>
            <Th>Means</Th>
          </tr>
        </thead>
        <tbody>
          {words.map((word) => (
            <tr key={`${word.target}:${word.value}`} data-import-word={word.value}>
              <Td className="text-sm text-ink-muted">{TARGET_LABELS[word.target] ?? word.target}</Td>
              <Td className="text-sm text-ink">{word.value}</Td>
              <Td className="text-sm text-ink-muted">{word.count}</Td>
              <Td>
                <label htmlFor={`word-${word.target}-${word.value}`} className="sr-only">
                  What {word.value} means here
                </label>
                <SelectInput
                  id={`word-${word.target}-${word.value}`}
                  controlSize="sm"
                  value={values[word.target]?.[word.value] ?? word.means}
                  onChange={(e) => pick(word.target, word.value, e.target.value)}
                  data-import-word-means={word.value}
                >
                  <option value="">Leave it out</option>
                  {optionsFor(word.target).map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </SelectInput>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </Card>
  );
}
